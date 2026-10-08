package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/hkjang/confmcp/internal/apikey"
	"github.com/hkjang/confmcp/internal/audit"
	"github.com/hkjang/confmcp/internal/crypto"
	"github.com/hkjang/confmcp/internal/identity"
	"github.com/hkjang/confmcp/internal/oauthserver"
	"github.com/hkjang/confmcp/internal/settings"
	"github.com/hkjang/confmcp/internal/tools"
)

// ErrUnauthenticated is returned when no usable credential is present.
var ErrUnauthenticated = errors.New("인증 정보가 없습니다")

// Service resolves credentials into principals and serves the login endpoints.
type Service struct {
	Users    *Users
	Sessions *Sessions
	OIDC     *OIDC
	Mapper   *identity.Mapper
	Keys     *apikey.Service
	OAuth    *oauthserver.Server
	Store    *settings.Store
	Audit    *audit.Logger
	Sealer   *crypto.Sealer
	// ResourceFor returns the MCP resource URL a request addresses, so that
	// a confmcp access token is only accepted by the resource it names.
	ResourceFor func(*http.Request) string
}

// Identity is the authenticated caller as seen by the web API.
type Identity struct {
	User        *User             `json:"user"`
	Mapping     *identity.Mapping `json:"confluence,omitempty"`
	MappingErr  string            `json:"mappingError,omitempty"`
	Roles       []string          `json:"roles"`
	Scopes      []string          `json:"scopes"`
	AuthMode    string            `json:"authMode"`
	KeycloakSub string            `json:"keycloakSub"`
	APIKeyID    string            `json:"apiKeyId,omitempty"`
	OAuthClient string            `json:"oauthClient,omitempty"`
}

// subjectFor returns the durable subject for a user: the Keycloak subject when
// the account came from SSO, and a stable local identifier otherwise.
func subjectFor(u *User) string {
	if u.KeycloakSub != "" {
		return u.KeycloakSub
	}
	return "local:" + strings.ToLower(u.Username)
}

// Resolve authenticates a request, trying session cookie, bearer token and
// API key in that order.
func (s *Service) Resolve(ctx context.Context, r *http.Request) (*Identity, error) {
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if authz == "" {
		if c, err := r.Cookie(SessionCookie); err == nil && c.Value != "" {
			sess, err := s.Sessions.Resolve(ctx, c.Value)
			if err == nil {
				u, err := s.Users.ByID(ctx, sess.UserID)
				if err != nil {
					return nil, err
				}
				if !u.Active {
					return nil, errors.New("비활성 계정입니다")
				}
				return s.identityFor(ctx, u, "session", ""), nil
			}
		}
	}
	if authz != "" {
		scheme, token, _ := strings.Cut(authz, " ")
		token = strings.TrimSpace(token)
		if strings.EqualFold(scheme, "bearer") {
			switch {
			case strings.HasPrefix(token, apikey.Prefix+"_"):
				return s.resolveAPIKey(ctx, token)
			case strings.HasPrefix(token, oauthserver.AccessPrefix):
				return s.resolveOAuth(ctx, r, token)
			default:
				return s.resolveOIDCBearer(ctx, token)
			}
		}
	}
	if raw := strings.TrimSpace(r.Header.Get("X-API-Key")); raw != "" {
		return s.resolveAPIKey(ctx, raw)
	}
	return nil, ErrUnauthenticated
}

// intersect keeps the scopes present in both lists. A credential never
// carries more than the owner's current roles allow (AUTH-08): roles are
// re-read on every request, so a demotion takes effect immediately.
func intersect(granted, allowed []string) []string {
	ok := map[string]bool{}
	for _, a := range allowed {
		ok[strings.ToLower(a)] = true
	}
	out := []string{}
	for _, g := range granted {
		if ok[strings.ToLower(g)] {
			out = append(out, g)
		}
	}
	return out
}

func (s *Service) resolveAPIKey(ctx context.Context, raw string) (*Identity, error) {
	v, err := s.Keys.Verify(ctx, raw)
	if err != nil {
		return nil, err
	}
	u, err := s.Users.ByID(ctx, v.Key.UserID)
	if err != nil {
		return nil, err
	}
	if !u.Active {
		return nil, errors.New("비활성 계정의 키입니다")
	}
	id := s.identityFor(ctx, u, "apikey", v.Key.ID.String())
	id.Scopes = intersect(v.Scopes, id.Scopes)
	return id, nil
}

func (s *Service) resolveOAuth(ctx context.Context, r *http.Request, token string) (*Identity, error) {
	kc, _ := s.Store.Keycloak(ctx)
	if !kc.MCPOAuthEnabled {
		return nil, errors.New("MCP OAuth 가 비활성화되어 있습니다")
	}
	resource := ""
	if s.ResourceFor != nil {
		resource = s.ResourceFor(r)
	}
	p, err := s.OAuth.Verify(ctx, token, resource)
	if err != nil {
		return nil, err
	}
	u, err := s.Users.ByID(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	if !u.Active {
		return nil, errors.New("비활성 계정입니다")
	}
	id := s.identityFor(ctx, u, "oauth", "")
	id.OAuthClient = p.ClientID
	if len(p.Scopes) > 0 && !(len(p.Scopes) == 1 && p.Scopes[0] == "*") {
		id.Scopes = intersect(p.Scopes, id.Scopes)
	}
	return id, nil
}

func (s *Service) resolveOIDCBearer(ctx context.Context, token string) (*Identity, error) {
	claims, err := s.OIDC.VerifyAccessToken(ctx, token)
	if err != nil {
		return nil, err
	}
	kc, err := s.Store.Keycloak(ctx)
	if err != nil {
		return nil, err
	}
	if kc.RequireRole != "" && !claims.HasRole(kc.RequireRole) {
		return nil, fmt.Errorf("%s 역할이 필요합니다", kc.RequireRole)
	}
	u, err := s.Users.UpsertFromKeycloak(ctx, claims.Issuer, claims.Subject, claims.Username,
		claims.Email, claims.DisplayName, claims.Roles, claims.HasRole(kc.AdminRole))
	if err != nil {
		return nil, err
	}
	if !u.Active {
		return nil, errors.New("비활성 계정입니다")
	}
	return s.identityFor(ctx, u, "oauth", ""), nil
}

// RolesOf returns the effective roles of a user. Without any confmcp role the
// configured default role applies. A service administrator also holds the
// admin role, which manages settings but grants no Confluence data access of
// its own.
func RolesOf(u *User, defaultRole string) []string {
	roles := append([]string{}, u.Roles...)
	has := false
	for _, r := range roles {
		if strings.HasPrefix(r, "confluence-mcp-") {
			has = true
		}
	}
	if !has && defaultRole != "" {
		roles = append(roles, defaultRole)
	}
	if u.IsServiceAdmin {
		roles = appendUnique(roles, tools.RoleAdmin)
	}
	return roles
}

// identityFor assembles the identity, resolving the Confluence mapping.
func (s *Service) identityFor(ctx context.Context, u *User, mode, keyID string) *Identity {
	kc, _ := s.Store.Keycloak(ctx)
	roles := RolesOf(u, kc.DefaultRole)
	id := &Identity{
		User:        u,
		AuthMode:    mode,
		KeycloakSub: subjectFor(u),
		APIKeyID:    keyID,
		Roles:       roles,
		Scopes:      tools.ScopesForRoles(roles),
	}
	if m, err := s.Mapper.Resolve(ctx, u.KeycloakIssuer, id.KeycloakSub, u.Username); err == nil {
		id.Mapping = m
	} else {
		id.MappingErr = strings.TrimPrefix(err.Error(), identity.ErrUnmapped.Error()+": ")
	}
	return id
}

// Principal converts an identity into a tool principal.
func (id *Identity) Principal(client, ip, requestID string) tools.Principal {
	p := tools.Principal{
		UserID:         id.User.ID,
		KeycloakIssuer: id.User.KeycloakIssuer,
		KeycloakSub:    id.KeycloakSub,
		Username:       id.User.Username,
		DisplayName:    id.User.DisplayName,
		Roles:          id.Roles,
		Scopes:         id.Scopes,
		IsServiceAdmin: id.User.IsServiceAdmin,
		AuthMode:       id.AuthMode,
		Client:         client,
		IP:             ip,
		RequestID:      requestID,
	}
	if id.OAuthClient != "" && client == "" {
		p.Client = id.OAuthClient
	}
	if id.Mapping != nil && id.Mapping.Active {
		p.ConfluenceUserKey = id.Mapping.ConfluenceUserKey
		p.ConfluenceUsername = id.Mapping.ConfluenceUsername
	}
	return p
}

func appendUnique(list []string, v string) []string {
	for _, item := range list {
		if strings.EqualFold(item, v) {
			return list
		}
	}
	return append(list, v)
}

// LoginLocal verifies a username/password pair and creates a session.
func (s *Service) LoginLocal(ctx context.Context, username, password, ip, ua string) (string, time.Duration, *Identity, error) {
	sec, err := s.Store.Security(ctx)
	if err != nil {
		return "", 0, nil, err
	}
	ttl := time.Duration(sec.SessionTTLMinutes) * time.Minute
	if ttl <= 0 {
		ttl = 8 * time.Hour
	}

	u, err := s.Users.ByUsername(ctx, username)
	if err != nil || !u.Active {
		return "", 0, nil, errors.New("아이디 또는 비밀번호가 올바르지 않습니다")
	}
	hash, err := s.Users.PasswordHash(ctx, u.ID)
	if err != nil || hash == "" || !crypto.VerifyPassword(hash, password) {
		s.Audit.Write(ctx, audit.Entry{
			Category: audit.CatAuth, Action: "login.local", KeycloakUsername: username,
			Success: false, ErrorCode: "BAD_CREDENTIALS", IP: ip,
		})
		return "", 0, nil, errors.New("아이디 또는 비밀번호가 올바르지 않습니다")
	}

	token, _, err := s.Sessions.Create(ctx, u.ID, ttl, ip, ua)
	if err != nil {
		return "", 0, nil, err
	}
	s.Users.TouchLogin(ctx, u.ID)
	s.Audit.Write(ctx, audit.Entry{
		Category: audit.CatAuth, Action: "login.local", KeycloakUsername: u.Username,
		Success: true, IP: ip, AuthMode: "session",
	})
	return token, ttl, s.identityFor(ctx, u, "session", ""), nil
}

// oidcState is the short-lived state carried in a cookie across the redirect.
type oidcState struct {
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	Silent   bool   `json:"q"`
	ReturnTo string `json:"r"`
	Redirect string `json:"d"`
	Issued   int64  `json:"t"`
}

// StateCookie is the cookie holding the in-flight OIDC state.
const StateCookie = "confmcp_oidc"

// SealState encrypts the OIDC state for the cookie.
func (s *Service) SealState(st oidcState) (string, error) {
	body, err := json.Marshal(st)
	if err != nil {
		return "", err
	}
	return s.Sealer.Seal(base64.RawURLEncoding.EncodeToString(body))
}

// OpenState decrypts and validates the OIDC state cookie.
func (s *Service) OpenState(raw string) (*oidcState, error) {
	plain, err := s.Sealer.Open(raw)
	if err != nil {
		return nil, err
	}
	body, err := base64.RawURLEncoding.DecodeString(plain)
	if err != nil {
		return nil, err
	}
	var st oidcState
	if err := json.Unmarshal(body, &st); err != nil {
		return nil, err
	}
	if time.Since(time.Unix(st.Issued, 0)) > 10*time.Minute {
		return nil, errors.New("로그인 상태가 만료되었습니다")
	}
	return &st, nil
}

// NewState builds a fresh OIDC state.
func NewState(silent bool, returnTo, redirect string) (oidcState, error) {
	state, err := crypto.RandomToken(16)
	if err != nil {
		return oidcState{}, err
	}
	nonce, err := crypto.RandomToken(16)
	if err != nil {
		return oidcState{}, err
	}
	verifier, err := crypto.RandomToken(32)
	if err != nil {
		return oidcState{}, err
	}
	return oidcState{
		State: state, Nonce: nonce, Verifier: verifier, Silent: silent,
		ReturnTo: returnTo, Redirect: redirect, Issued: time.Now().Unix(),
	}, nil
}

// CompleteOIDC turns verified claims into a session.
func (s *Service) CompleteOIDC(ctx context.Context, claims *Claims, ip, ua string) (string, time.Duration, *Identity, error) {
	kc, err := s.Store.Keycloak(ctx)
	if err != nil {
		return "", 0, nil, err
	}
	if kc.RequireRole != "" && !claims.HasRole(kc.RequireRole) {
		return "", 0, nil, fmt.Errorf("%s 역할이 없어 로그인할 수 없습니다", kc.RequireRole)
	}

	var u *User
	if kc.AutoProvision {
		u, err = s.Users.UpsertFromKeycloak(ctx, claims.Issuer, claims.Subject, claims.Username,
			claims.Email, claims.DisplayName, claims.Roles, claims.HasRole(kc.AdminRole))
	} else {
		u, err = s.Users.ByUsername(ctx, claims.Username)
		if err == nil {
			u, err = s.Users.UpsertFromKeycloak(ctx, claims.Issuer, claims.Subject, claims.Username,
				claims.Email, claims.DisplayName, claims.Roles, claims.HasRole(kc.AdminRole))
		}
	}
	if err != nil {
		return "", 0, nil, err
	}
	if !u.Active {
		return "", 0, nil, errors.New("비활성 계정입니다")
	}

	sec, err := s.Store.Security(ctx)
	if err != nil {
		return "", 0, nil, err
	}
	ttl := time.Duration(sec.SessionTTLMinutes) * time.Minute
	if ttl <= 0 {
		ttl = 8 * time.Hour
	}
	token, _, err := s.Sessions.Create(ctx, u.ID, ttl, ip, ua)
	if err != nil {
		return "", 0, nil, err
	}
	s.Users.TouchLogin(ctx, u.ID)
	s.Audit.Write(ctx, audit.Entry{
		Category: audit.CatAuth, Action: "login.oidc", KeycloakSub: claims.Subject,
		KeycloakUsername: claims.Username, Success: true, IP: ip, AuthMode: "oauth",
		Detail: map[string]any{"roles": claims.Roles},
	})
	return token, ttl, s.identityFor(ctx, u, "session", ""), nil
}
