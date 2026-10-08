// Package oauthserver makes confmcp a real OAuth 2.1 authorization server for
// MCP clients.
//
// MCP clients register here (RFC 7591), send the user through
// /oauth/authorize with PKCE, and receive tokens issued by confmcp itself. The
// user signs in through the console's Keycloak OIDC login (or a local
// account), so Keycloak needs nothing beyond the one web client the console
// already uses: no public MCP client, no loopback redirect registration, no
// audience mapper. Because confmcp issues the tokens, the metadata names
// confmcp as issuer truthfully; nothing is mirrored or rewritten.
//
// Access tokens are opaque, stored only as hashes, bound to the MCP resource
// and short-lived. Refresh tokens rotate on every use, and reuse of a rotated
// refresh token revokes the whole grant.
package oauthserver

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Token prefixes make confmcp tokens recognisable in an Authorization header.
const (
	AccessPrefix  = "cmcp_at_"
	RefreshPrefix = "cmcp_rt_"
	codePrefix    = "cmcp_ac_"
)

// Lifetimes.
const (
	CodeTTL    = 2 * time.Minute
	AccessTTL  = time.Hour
	RefreshTTL = 30 * 24 * time.Hour
)

// Errors with OAuth error codes.
var (
	ErrInvalidClient  = errors.New("invalid_client")
	ErrInvalidGrant   = errors.New("invalid_grant")
	ErrInvalidRequest = errors.New("invalid_request")
	ErrInvalidToken   = errors.New("invalid_token")
)

// Client is a registered MCP client.
type Client struct {
	ID           string     `json:"clientId"`
	Name         string     `json:"clientName"`
	RedirectURIs []string   `json:"redirectUris"`
	SoftwareID   string     `json:"softwareId,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	LastUsedAt   *time.Time `json:"lastUsedAt,omitempty"`
	Grants       int        `json:"activeGrants"`
}

// Grant is a user's authorisation of a client.
type Grant struct {
	ID         uuid.UUID  `json:"id"`
	ClientID   string     `json:"clientId"`
	ClientName string     `json:"clientName"`
	UserID     int64      `json:"userId"`
	Username   string     `json:"username"`
	Scopes     []string   `json:"scopes"`
	Resource   string     `json:"resource"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	RevokedAt  *time.Time `json:"revokedAt,omitempty"`
}

// Server stores clients, codes and tokens.
type Server struct{ pool *pgxpool.Pool }

// New builds the server store.
func New(pool *pgxpool.Pool) *Server { return &Server{pool: pool} }

func random(prefix string, n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return prefix + base64.RawURLEncoding.EncodeToString(b)
}

func hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

var privateScheme = regexp.MustCompile(`^[a-z][a-z0-9+.-]*\.[a-z0-9+.-]+$`)

// ValidRedirect accepts loopback HTTP redirects (any port, RFC 8252 §7.3),
// HTTPS redirects, and reverse-domain private-use schemes (RFC 8252 §7.1).
func ValidRedirect(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return fmt.Errorf("redirect_uri 형식이 올바르지 않습니다: %s", raw)
	}
	if u.Fragment != "" {
		return fmt.Errorf("redirect_uri 에 fragment 를 쓸 수 없습니다")
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		host := u.Hostname()
		if host != "127.0.0.1" && host != "localhost" && host != "::1" {
			return fmt.Errorf("http redirect_uri 는 루프백 주소만 허용합니다: %s", raw)
		}
		return nil
	case "https":
		if u.Host == "" {
			return fmt.Errorf("redirect_uri 에 호스트가 없습니다")
		}
		return nil
	default:
		if privateScheme.MatchString(strings.ToLower(u.Scheme)) || strings.EqualFold(u.Scheme, "cursor") ||
			strings.EqualFold(u.Scheme, "vscode") || strings.EqualFold(u.Scheme, "vscode-insiders") {
			return nil
		}
		return fmt.Errorf("허용되지 않는 redirect_uri 스킴입니다: %s", u.Scheme)
	}
}

// matchRedirect compares a requested redirect with a registered one. Loopback
// redirects match regardless of port, since native clients pick a free port.
func matchRedirect(registered, requested string) bool {
	if registered == requested {
		return true
	}
	a, err1 := url.Parse(registered)
	b, err2 := url.Parse(requested)
	if err1 != nil || err2 != nil {
		return false
	}
	loop := func(u *url.URL) bool {
		h := u.Hostname()
		return strings.EqualFold(u.Scheme, "http") && (h == "127.0.0.1" || h == "localhost" || h == "::1")
	}
	if loop(a) && loop(b) {
		return a.Hostname() == b.Hostname() && a.Path == b.Path && a.RawQuery == b.RawQuery
	}
	return false
}

// Register stores a new client. Registration is open, as MCP clients expect,
// but a client is only a name and redirect URIs: it gains nothing until a
// signed-in user approves it on the consent screen.
func (s *Server) Register(ctx context.Context, name, softwareID string, redirects []string) (*Client, error) {
	if len(redirects) == 0 {
		return nil, fmt.Errorf("%w: redirect_uris 가 필요합니다", ErrInvalidRequest)
	}
	if len(redirects) > 10 {
		return nil, fmt.Errorf("%w: redirect_uris 는 10개 이하", ErrInvalidRequest)
	}
	for _, r := range redirects {
		if err := ValidRedirect(r); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
		}
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "MCP 클라이언트"
	}
	if len([]rune(name)) > 100 {
		name = string([]rune(name)[:100])
	}
	c := &Client{ID: "mcp_" + strings.TrimPrefix(random("", 18), "-"), Name: name, RedirectURIs: redirects,
		SoftwareID: softwareID, CreatedAt: time.Now()}
	_, err := s.pool.Exec(ctx, `INSERT INTO oauth_clients(client_id, client_name, redirect_uris, software_id)
		VALUES ($1,$2,$3,NULLIF($4,''))`, c.ID, c.Name, redirects, softwareID)
	return c, err
}

// Client loads a registered client.
func (s *Server) Client(ctx context.Context, id string) (*Client, error) {
	c := &Client{}
	err := s.pool.QueryRow(ctx, `SELECT client_id, client_name, redirect_uris, COALESCE(software_id,''), created_at, last_used_at
		FROM oauth_clients WHERE client_id=$1 AND disabled_at IS NULL`, id).
		Scan(&c.ID, &c.Name, &c.RedirectURIs, &c.SoftwareID, &c.CreatedAt, &c.LastUsedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvalidClient
	}
	return c, err
}

// CheckRedirect verifies a redirect against the client's registration.
func (c *Client) CheckRedirect(requested string) bool {
	for _, r := range c.RedirectURIs {
		if matchRedirect(r, requested) {
			return true
		}
	}
	return false
}

// AuthRequest is a validated /oauth/authorize request awaiting consent.
type AuthRequest struct {
	ClientID      string   `json:"c"`
	RedirectURI   string   `json:"r"`
	State         string   `json:"s"`
	CodeChallenge string   `json:"cc"`
	Scopes        []string `json:"sc"`
	Resource      string   `json:"rs"`
	Exp           int64    `json:"e"`
}

// IssueCode creates a one-time authorization code for an approved request.
func (s *Server) IssueCode(ctx context.Context, req AuthRequest, userID int64) (string, error) {
	code := random(codePrefix, 32)
	_, err := s.pool.Exec(ctx, `INSERT INTO oauth_codes(code_hash, client_id, user_id, redirect_uri, code_challenge,
		scopes, resource, expires_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		hash(code), req.ClientID, userID, req.RedirectURI, req.CodeChallenge, req.Scopes, req.Resource,
		time.Now().Add(CodeTTL))
	return code, err
}

// Tokens is a token endpoint answer.
type Tokens struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// ExchangeCode redeems a code with its PKCE verifier. A code works once.
func (s *Server) ExchangeCode(ctx context.Context, code, clientID, redirectURI, verifier, resource string) (*Tokens, error) {
	var userID int64
	var cid, redirect, challenge, res string
	var scopes []string
	var exp time.Time
	err := s.pool.QueryRow(ctx, `UPDATE oauth_codes SET used_at=NOW()
		WHERE code_hash=$1 AND used_at IS NULL
		RETURNING client_id, user_id, redirect_uri, code_challenge, scopes, resource, expires_at`, hash(code)).
		Scan(&cid, &userID, &redirect, &challenge, &scopes, &res, &exp)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w: 인가 코드가 없거나 이미 사용되었습니다", ErrInvalidGrant)
	}
	if err != nil {
		return nil, err
	}
	switch {
	case time.Now().After(exp):
		return nil, fmt.Errorf("%w: 인가 코드가 만료되었습니다", ErrInvalidGrant)
	case cid != clientID:
		return nil, fmt.Errorf("%w: 다른 클라이언트의 코드입니다", ErrInvalidGrant)
	case redirect != redirectURI:
		return nil, fmt.Errorf("%w: redirect_uri 가 인가 요청과 다릅니다", ErrInvalidGrant)
	case resource != "" && res != "" && strings.TrimRight(resource, "/") != strings.TrimRight(res, "/"):
		return nil, fmt.Errorf("%w: resource 가 인가 요청과 다릅니다", ErrInvalidGrant)
	}
	sum := sha256.Sum256([]byte(verifier))
	if verifier == "" || subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(challenge)) != 1 {
		return nil, fmt.Errorf("%w: PKCE code_verifier 가 맞지 않습니다", ErrInvalidGrant)
	}
	grant := uuid.New()
	if _, err := s.pool.Exec(ctx, `INSERT INTO oauth_grants(id, client_id, user_id, scopes, resource) VALUES ($1,$2,$3,$4,$5)`,
		grant, cid, userID, scopes, res); err != nil {
		return nil, err
	}
	_, _ = s.pool.Exec(ctx, `UPDATE oauth_clients SET last_used_at=NOW() WHERE client_id=$1`, cid)
	return s.mint(ctx, grant, scopes)
}

func (s *Server) mint(ctx context.Context, grant uuid.UUID, scopes []string) (*Tokens, error) {
	access := random(AccessPrefix, 32)
	refresh := random(RefreshPrefix, 32)
	_, err := s.pool.Exec(ctx, `INSERT INTO oauth_tokens(token_hash, grant_id, kind, expires_at) VALUES
		($1,$3,'access',$4), ($2,$3,'refresh',$5)`,
		hash(access), hash(refresh), grant, time.Now().Add(AccessTTL), time.Now().Add(RefreshTTL))
	if err != nil {
		return nil, err
	}
	return &Tokens{AccessToken: access, TokenType: "Bearer", ExpiresIn: int(AccessTTL.Seconds()),
		RefreshToken: refresh, Scope: strings.Join(scopes, " ")}, nil
}

// Refresh rotates a refresh token. Presenting an already-rotated refresh
// token means it leaked, so the whole grant is revoked.
func (s *Server) Refresh(ctx context.Context, refresh, clientID string) (*Tokens, error) {
	var grant uuid.UUID
	var used *time.Time
	var exp time.Time
	var cid string
	var revoked *time.Time
	var scopes []string
	err := s.pool.QueryRow(ctx, `SELECT t.grant_id, t.used_at, t.expires_at, g.client_id, g.revoked_at, g.scopes
		FROM oauth_tokens t JOIN oauth_grants g ON g.id=t.grant_id WHERE t.token_hash=$1 AND t.kind='refresh'`, hash(refresh)).
		Scan(&grant, &used, &exp, &cid, &revoked, &scopes)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w: 알 수 없는 refresh_token 입니다", ErrInvalidGrant)
	}
	if err != nil {
		return nil, err
	}
	if cid != clientID {
		return nil, fmt.Errorf("%w: 다른 클라이언트의 토큰입니다", ErrInvalidGrant)
	}
	if revoked != nil || time.Now().After(exp) {
		return nil, fmt.Errorf("%w: 만료되었거나 철회된 승인입니다. 다시 로그인하십시오", ErrInvalidGrant)
	}
	if used != nil {
		_ = s.RevokeGrant(ctx, grant)
		return nil, fmt.Errorf("%w: 이미 사용된 refresh_token 입니다. 보안을 위해 이 연결을 철회했습니다", ErrInvalidGrant)
	}
	tag, err := s.pool.Exec(ctx, `UPDATE oauth_tokens SET used_at=NOW() WHERE token_hash=$1 AND used_at IS NULL`, hash(refresh))
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		_ = s.RevokeGrant(ctx, grant)
		return nil, fmt.Errorf("%w: 이미 사용된 refresh_token 입니다", ErrInvalidGrant)
	}
	_, _ = s.pool.Exec(ctx, `UPDATE oauth_grants SET last_used_at=NOW() WHERE id=$1`, grant)
	return s.mint(ctx, grant, scopes)
}

// Principal is what an access token proves.
type Principal struct {
	UserID   int64
	ClientID string
	GrantID  uuid.UUID
	Scopes   []string
	Resource string
}

// Verify checks an access token and the resource it was issued for.
func (s *Server) Verify(ctx context.Context, access, resource string) (*Principal, error) {
	if !strings.HasPrefix(access, AccessPrefix) {
		return nil, ErrInvalidToken
	}
	var p Principal
	var exp time.Time
	var revoked *time.Time
	err := s.pool.QueryRow(ctx, `SELECT g.user_id, g.client_id, g.id, g.scopes, g.resource, t.expires_at, g.revoked_at
		FROM oauth_tokens t JOIN oauth_grants g ON g.id=t.grant_id WHERE t.token_hash=$1 AND t.kind='access'`, hash(access)).
		Scan(&p.UserID, &p.ClientID, &p.GrantID, &p.Scopes, &p.Resource, &exp, &revoked)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w: 알 수 없는 토큰입니다", ErrInvalidToken)
	}
	if err != nil {
		return nil, err
	}
	if revoked != nil {
		return nil, fmt.Errorf("%w: 철회된 연결입니다", ErrInvalidToken)
	}
	if time.Now().After(exp) {
		return nil, fmt.Errorf("%w: 만료된 토큰입니다", ErrInvalidToken)
	}
	if resource != "" && p.Resource != "" && !sameResource(p.Resource, resource) {
		return nil, fmt.Errorf("%w: 이 리소스를 위해 발급된 토큰이 아닙니다", ErrInvalidToken)
	}
	return &p, nil
}

func sameResource(a, b string) bool {
	norm := func(s string) string { return strings.TrimSuffix(strings.TrimRight(s, "/"), "/mcp") }
	return strings.EqualFold(norm(a), norm(b))
}

// Revoke revokes the grant behind a token (RFC 7009); unknown tokens are not
// an error.
func (s *Server) Revoke(ctx context.Context, token string) error {
	var grant uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT grant_id FROM oauth_tokens WHERE token_hash=$1`, hash(token)).Scan(&grant)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.RevokeGrant(ctx, grant)
}

// RevokeGrant ends a client's authorisation.
func (s *Server) RevokeGrant(ctx context.Context, grant uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE oauth_grants SET revoked_at=NOW() WHERE id=$1 AND revoked_at IS NULL`, grant)
	return err
}

// RevokeUser ends every grant of a user (deactivation, admin action).
func (s *Server) RevokeUser(ctx context.Context, userID int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE oauth_grants SET revoked_at=NOW() WHERE user_id=$1 AND revoked_at IS NULL`, userID)
	return err
}

// Grants lists grants, optionally for one user.
func (s *Server) Grants(ctx context.Context, userID int64, activeOnly bool) ([]Grant, error) {
	rows, err := s.pool.Query(ctx, `SELECT g.id, g.client_id, c.client_name, g.user_id, u.username, g.scopes, g.resource,
		g.created_at, g.last_used_at, g.revoked_at
		FROM oauth_grants g JOIN oauth_clients c ON c.client_id=g.client_id JOIN users u ON u.id=g.user_id
		WHERE ($1=0 OR g.user_id=$1) AND (NOT $2 OR g.revoked_at IS NULL)
		ORDER BY g.created_at DESC LIMIT 300`, userID, activeOnly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Grant{}
	for rows.Next() {
		var g Grant
		if err := rows.Scan(&g.ID, &g.ClientID, &g.ClientName, &g.UserID, &g.Username, &g.Scopes, &g.Resource,
			&g.CreatedAt, &g.LastUsedAt, &g.RevokedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// GrantOwner returns the user a grant belongs to.
func (s *Server) GrantOwner(ctx context.Context, id uuid.UUID) (int64, error) {
	var uid int64
	err := s.pool.QueryRow(ctx, `SELECT user_id FROM oauth_grants WHERE id=$1`, id).Scan(&uid)
	return uid, err
}

// Clients lists registered clients with their active grant counts.
func (s *Server) Clients(ctx context.Context) ([]Client, error) {
	rows, err := s.pool.Query(ctx, `SELECT c.client_id, c.client_name, c.redirect_uris, COALESCE(c.software_id,''),
		c.created_at, c.last_used_at,
		(SELECT COUNT(*) FROM oauth_grants g WHERE g.client_id=c.client_id AND g.revoked_at IS NULL)
		FROM oauth_clients c WHERE c.disabled_at IS NULL ORDER BY c.created_at DESC LIMIT 300`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Client{}
	for rows.Next() {
		var c Client
		if err := rows.Scan(&c.ID, &c.Name, &c.RedirectURIs, &c.SoftwareID, &c.CreatedAt, &c.LastUsedAt, &c.Grants); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DisableClient blocks a client and revokes its grants.
func (s *Server) DisableClient(ctx context.Context, id string) error {
	if _, err := s.pool.Exec(ctx, `UPDATE oauth_clients SET disabled_at=NOW() WHERE client_id=$1`, id); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `UPDATE oauth_grants SET revoked_at=NOW() WHERE client_id=$1 AND revoked_at IS NULL`, id)
	return err
}

// Purge removes expired codes and tokens and stale unused clients.
func (s *Server) Purge(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM oauth_codes WHERE expires_at < NOW() - INTERVAL '1 hour'`)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `DELETE FROM oauth_tokens WHERE expires_at < NOW() - INTERVAL '1 day'`)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `DELETE FROM oauth_clients c WHERE c.last_used_at IS NULL
		AND c.created_at < NOW() - INTERVAL '7 days'
		AND NOT EXISTS (SELECT 1 FROM oauth_grants g WHERE g.client_id=c.client_id)`)
	return err
}
