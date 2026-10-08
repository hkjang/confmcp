package api

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hkjang/confmcp/internal/audit"
	"github.com/hkjang/confmcp/internal/httpx"
	"github.com/hkjang/confmcp/internal/oauthserver"
	"github.com/hkjang/confmcp/internal/settings"
	"github.com/hkjang/confmcp/internal/tools"
	"github.com/hkjang/confmcp/internal/version"
)

// MCP OAuth.
//
// An MCP client that gets a 401 from /mcp follows RFC 9728 to the protected
// resource metadata, which names this gateway as the authorization server.
// The client registers here (RFC 7591), sends the user to /oauth/authorize
// with PKCE, and the user signs in to the console (Keycloak SSO, silently when
// a Keycloak session exists) and approves the client once. confmcp then
// issues its own tokens, bound to the MCP resource.

// versionHeader names the build that answered.
const versionHeader = "X-Confmcp-Version"

// mountOAuth registers the discovery, authorization and token endpoints.
func (s *Server) mountOAuth(r interface {
	Get(pattern string, h http.HandlerFunc)
	Post(pattern string, h http.HandlerFunc)
}) {
	r.Get("/.well-known/oauth-protected-resource", discovery(s.protectedResource))
	r.Get("/.well-known/oauth-protected-resource/mcp", discovery(s.protectedResource))
	r.Get("/.well-known/oauth-authorization-server", discovery(s.authorizationServerMetadata))
	r.Get("/.well-known/oauth-authorization-server/mcp", discovery(s.authorizationServerMetadata))
	r.Get("/.well-known/openid-configuration", discovery(s.authorizationServerMetadata))
	r.Get("/.well-known/openid-configuration/mcp", discovery(s.authorizationServerMetadata))
	r.Get("/.well-known/*", discovery(s.unknownWellKnown))

	r.Post("/oauth/register", discovery(s.registerClient))
	r.Get("/oauth/authorize", discovery(s.authorize))
	r.Post("/oauth/token", discovery(s.token))
	r.Post("/oauth/revoke", discovery(s.revoke))
}

// discovery marks an answer that must never be cached and lets browser-based
// MCP clients read it cross-origin.
func discovery(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set(versionHeader, version.Version)
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, MCP-Protocol-Version")
		h(w, r)
	}
}

func (s *Server) unknownWellKnown(w http.ResponseWriter, r *http.Request) {
	s.traceRequest(r, traceUnknown, http.StatusNotFound, "")
	httpx.FailCode(w, http.StatusNotFound, "NOT_FOUND", "알 수 없는 well-known 경로입니다")
}

func (s *Server) traceRequest(r *http.Request, kind string, status int, detail string) {
	if s.trace == nil {
		return
	}
	sec, _ := s.Store.Security(r.Context())
	s.trace.record(traceEvent{
		Kind: kind, Method: r.Method, Path: r.URL.Path, Status: status,
		IP: httpx.ClientIP(r, sec.TrustProxyHeaders), Agent: r.UserAgent(), Detail: detail,
	})
}

// issuer is this gateway's public origin, which is also the OAuth issuer.
func (s *Server) issuer(r *http.Request) string {
	cfg, _ := s.Store.MCP(r.Context())
	if normalized := httpx.NormalizeBaseURL(strings.TrimSuffix(strings.TrimRight(cfg.ResourceURL, "/"), "/mcp")); normalized != "" {
		return normalized
	}
	return s.baseURL(r)
}

func (s *Server) resourceMetadataURL(r *http.Request) string {
	return s.issuer(r) + "/.well-known/oauth-protected-resource/mcp"
}

func (s *Server) mcpResource(r *http.Request) string { return s.issuer(r) + "/mcp" }

func oauthScopes() []string {
	return []string{tools.ScopeRead, tools.ScopeAttachmentRead, tools.ScopeWrite, tools.ScopeAttachmentWrite,
		tools.ScopeExecute, tools.ScopeAIInvoke}
}

func (s *Server) protectedResource(w http.ResponseWriter, r *http.Request) {
	kc, _ := s.Store.Keycloak(r.Context())
	resource := s.issuer(r)
	if strings.HasSuffix(r.URL.Path, "/mcp") {
		resource += "/mcp"
	}
	servers := []string{}
	if kc.MCPOAuthEnabled {
		servers = append(servers, s.issuer(r))
	}
	s.traceRequest(r, traceResourceMeta, http.StatusOK, "authorization_servers="+strings.Join(servers, ","))
	httpx.JSON(w, http.StatusOK, map[string]any{
		"resource":                 resource,
		"authorization_servers":    servers,
		"bearer_methods_supported": []string{"header"},
		"scopes_supported":         oauthScopes(),
		"resource_name":            "confmcp",
		"resource_documentation":   "https://hkjang.github.io/confmcp/",
	})
}

func (s *Server) authorizationServerMetadata(w http.ResponseWriter, r *http.Request) {
	kc, _ := s.Store.Keycloak(r.Context())
	if !kc.MCPOAuthEnabled {
		s.traceRequest(r, traceServerMeta, http.StatusNotFound, "MCP OAuth 꺼짐")
		httpx.FailCode(w, http.StatusNotFound, "OAUTH_DISABLED", "MCP OAuth 가 비활성화되어 있습니다")
		return
	}
	iss := s.issuer(r)
	s.traceRequest(r, traceServerMeta, http.StatusOK, "issuer="+iss)
	httpx.JSON(w, http.StatusOK, map[string]any{
		"issuer":                                iss,
		"authorization_endpoint":                iss + "/oauth/authorize",
		"token_endpoint":                        iss + "/oauth/token",
		"registration_endpoint":                 iss + "/oauth/register",
		"revocation_endpoint":                   iss + "/oauth/revoke",
		"response_types_supported":              []string{"code"},
		"response_modes_supported":              []string{"query"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"revocation_endpoint_auth_methods_supported": []string{"none"},
		"scopes_supported":                               oauthScopes(),
		"authorization_response_iss_parameter_supported": true,
		"service_documentation":                          "https://hkjang.github.io/confmcp/guide-user.html#mcp",
	})
}

func writeOAuthError(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, status, map[string]string{"error": code, "error_description": description})
}

func oauthCode(err error) string {
	for _, e := range []error{oauthserver.ErrInvalidClient, oauthserver.ErrInvalidGrant, oauthserver.ErrInvalidRequest} {
		if errors.Is(err, e) {
			return e.Error()
		}
	}
	return "server_error"
}

// registerClient implements RFC 7591 for public MCP clients.
func (s *Server) registerClient(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	kc, _ := s.Store.Keycloak(ctx)
	if !kc.MCPOAuthEnabled {
		writeOAuthError(w, http.StatusNotFound, "invalid_request", "MCP OAuth 가 비활성화되어 있습니다")
		return
	}
	sec, _ := s.Store.Security(ctx)
	ip := httpx.ClientIP(r, sec.TrustProxyHeaders)
	if !s.limiter.Allow("register:"+ip, 20, 5) {
		writeOAuthError(w, http.StatusTooManyRequests, "invalid_request", "등록 요청이 너무 많습니다")
		return
	}
	var body struct {
		RedirectURIs            []string `json:"redirect_uris"`
		ClientName              string   `json:"client_name"`
		SoftwareID              string   `json:"software_id"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
		GrantTypes              []string `json:"grant_types"`
		ResponseTypes           []string `json:"response_types"`
		Scope                   string   `json:"scope"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "JSON 본문을 읽을 수 없습니다")
		return
	}
	if m := body.TokenEndpointAuthMethod; m != "" && m != "none" {
		// Public clients only; asking for a secret-based method is answered
		// with what this server supports rather than refused.
		body.TokenEndpointAuthMethod = "none"
	}
	c, err := s.OAuth.Register(ctx, body.ClientName, body.SoftwareID, body.RedirectURIs)
	if err != nil {
		s.traceRequest(r, traceRegister, http.StatusBadRequest, err.Error())
		code := "invalid_client_metadata"
		if strings.Contains(err.Error(), "redirect_uri") {
			code = "invalid_redirect_uri"
		}
		writeOAuthError(w, http.StatusBadRequest, code, strings.TrimPrefix(err.Error(), "invalid_request: "))
		return
	}
	s.traceRequest(r, traceRegister, http.StatusCreated, "client_id="+c.ID+" name="+c.Name)
	s.Audit.Write(ctx, audit.Entry{Category: audit.CatAuth, Action: "oauth.register", Success: true, IP: ip,
		Detail: map[string]any{"clientId": c.ID, "clientName": c.Name, "redirectUris": c.RedirectURIs}})
	httpx.JSON(w, http.StatusCreated, map[string]any{
		"client_id":                  c.ID,
		"client_id_issued_at":        c.CreatedAt.Unix(),
		"client_name":                c.Name,
		"redirect_uris":              c.RedirectURIs,
		"token_endpoint_auth_method": "none",
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"scope":                      strings.Join(oauthScopes(), " "),
	})
}

// authorize validates the request and sends the browser to sign-in and
// consent. Errors about the client or redirect are shown, never redirected:
// redirecting to an unverified URI would make this an open redirector.
func (s *Server) authorize(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	kc, _ := s.Store.Keycloak(ctx)
	if !kc.MCPOAuthEnabled {
		s.oauthPage(w, r, "MCP OAuth 가 비활성화되어 있습니다. 관리자에게 문의하십시오.")
		return
	}
	client, err := s.OAuth.Client(ctx, q.Get("client_id"))
	if err != nil {
		s.oauthPage(w, r, "등록되지 않은 MCP 클라이언트입니다. 클라이언트에서 연결을 다시 시도하십시오.")
		return
	}
	redirect := q.Get("redirect_uri")
	if redirect == "" && len(client.RedirectURIs) == 1 {
		redirect = client.RedirectURIs[0]
	}
	if !client.CheckRedirect(redirect) {
		s.oauthPage(w, r, "redirect_uri 가 등록된 값과 다릅니다: "+redirect)
		return
	}
	fail := func(code, desc string) {
		u, _ := url.Parse(redirect)
		v := u.Query()
		v.Set("error", code)
		v.Set("error_description", desc)
		if st := q.Get("state"); st != "" {
			v.Set("state", st)
		}
		v.Set("iss", s.issuer(r))
		u.RawQuery = v.Encode()
		http.Redirect(w, r, u.String(), http.StatusFound)
	}
	if q.Get("response_type") != "code" {
		fail("unsupported_response_type", "response_type=code 만 지원합니다")
		return
	}
	if q.Get("code_challenge") == "" || q.Get("code_challenge_method") != "S256" {
		fail("invalid_request", "PKCE(S256) code_challenge 가 필요합니다")
		return
	}
	scopes := []string{}
	for _, sc := range strings.Fields(q.Get("scope")) {
		for _, known := range oauthScopes() {
			if sc == known {
				scopes = append(scopes, sc)
			}
		}
	}
	if len(scopes) == 0 {
		// No confmcp scope asked for: the grant follows the user's roles.
		scopes = []string{"*"}
	}
	resource := q.Get("resource")
	if resource != "" && !strings.HasPrefix(strings.TrimRight(resource, "/"), s.issuer(r)) {
		fail("invalid_target", "이 서버의 리소스가 아닙니다: "+resource)
		return
	}
	if resource == "" {
		resource = s.mcpResource(r)
	}
	req := oauthserver.AuthRequest{
		ClientID: client.ID, RedirectURI: redirect, State: q.Get("state"),
		CodeChallenge: q.Get("code_challenge"), Scopes: scopes, Resource: resource,
		Exp: time.Now().Add(15 * time.Minute).Unix(),
	}
	sealed, err := s.Signer.Seal(req)
	if err != nil {
		fail("server_error", err.Error())
		return
	}
	consent := "/oauth/consent?req=" + url.QueryEscape(sealed)

	id, err := s.Auth.Resolve(ctx, r)
	if err != nil || id.AuthMode != "session" {
		// Not signed in: the login page signs in (silently via Keycloak when an
		// SSO session exists) and comes back to the consent page.
		http.Redirect(w, r, "/login?next="+url.QueryEscape(consent), http.StatusFound)
		return
	}
	if kc.MCPAutoConsent {
		target, err := s.approveAuthorization(r, id.User.ID, id.User.Username, req)
		if err != nil {
			fail("server_error", err.Error())
			return
		}
		http.Redirect(w, r, target, http.StatusFound)
		return
	}
	http.Redirect(w, r, consent, http.StatusFound)
}

// approveAuthorization issues a code and returns the client redirect.
func (s *Server) approveAuthorization(r *http.Request, userID int64, username string, req oauthserver.AuthRequest) (string, error) {
	code, err := s.OAuth.IssueCode(r.Context(), req, userID)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(req.RedirectURI)
	if err != nil {
		return "", err
	}
	v := u.Query()
	v.Set("code", code)
	if req.State != "" {
		v.Set("state", req.State)
	}
	v.Set("iss", s.issuer(r))
	u.RawQuery = v.Encode()
	sec, _ := s.Store.Security(r.Context())
	s.Audit.Write(r.Context(), audit.Entry{Category: audit.CatAuth, Action: "oauth.authorize", KeycloakUsername: username,
		Success: true, IP: httpx.ClientIP(r, sec.TrustProxyHeaders), Detail: map[string]any{"clientId": req.ClientID, "scopes": req.Scopes}})
	return u.String(), nil
}

func (s *Server) openAuthRequest(raw string) (*oauthserver.AuthRequest, error) {
	var req oauthserver.AuthRequest
	if err := s.Signer.Open(raw, &req); err != nil {
		return nil, err
	}
	if time.Now().Unix() > req.Exp {
		return nil, errors.New("인가 요청이 만료되었습니다. 클라이언트에서 다시 연결하십시오")
	}
	return &req, nil
}

// consentInfo describes a pending authorization for the consent page.
func (s *Server) consentInfo(w http.ResponseWriter, r *http.Request) {
	req, err := s.openAuthRequest(r.URL.Query().Get("req"))
	if err != nil {
		httpx.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	client, err := s.OAuth.Client(r.Context(), req.ClientID)
	if err != nil {
		httpx.Fail(w, http.StatusBadRequest, "등록되지 않은 클라이언트입니다")
		return
	}
	host := req.RedirectURI
	if u, err := url.Parse(req.RedirectURI); err == nil {
		host = u.Scheme + "://" + u.Host
	}
	id := identityOf(r)
	httpx.JSON(w, http.StatusOK, map[string]any{
		"clientId": client.ID, "clientName": client.Name, "redirectHost": host,
		"scopes": req.Scopes, "effectiveScopes": id.Scopes, "resource": req.Resource,
		"user": id.User.Username, "mapped": id.Mapping != nil,
	})
}

// consentDecision records the user's answer and returns where to go next.
func (s *Server) consentDecision(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Req     string `json:"req"`
		Approve bool   `json:"approve"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Fail(w, http.StatusBadRequest, "요청 형식이 올바르지 않습니다")
		return
	}
	req, err := s.openAuthRequest(body.Req)
	if err != nil {
		httpx.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	id := identityOf(r)
	if id.AuthMode != "session" {
		httpx.Fail(w, http.StatusForbidden, "브라우저 로그인 세션에서만 승인할 수 있습니다")
		return
	}
	if !body.Approve {
		u, _ := url.Parse(req.RedirectURI)
		v := u.Query()
		v.Set("error", "access_denied")
		v.Set("error_description", "사용자가 연결을 거부했습니다")
		if req.State != "" {
			v.Set("state", req.State)
		}
		v.Set("iss", s.issuer(r))
		u.RawQuery = v.Encode()
		httpx.JSON(w, http.StatusOK, map[string]any{"redirect": u.String()})
		return
	}
	target, err := s.approveAuthorization(r, id.User.ID, id.User.Username, *req)
	if err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"redirect": target})
}

// token implements the authorization_code and refresh_token grants.
func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "폼 본문을 읽을 수 없습니다")
		return
	}
	sec, _ := s.Store.Security(ctx)
	ip := httpx.ClientIP(r, sec.TrustProxyHeaders)
	if !s.limiter.Allow("token:"+ip, 120, 30) {
		writeOAuthError(w, http.StatusTooManyRequests, "invalid_request", "요청이 너무 많습니다")
		return
	}
	clientID := r.PostForm.Get("client_id")
	if clientID == "" {
		if u, _, ok := r.BasicAuth(); ok {
			clientID = u
		}
	}
	if _, err := s.OAuth.Client(ctx, clientID); err != nil {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "등록되지 않은 클라이언트입니다")
		return
	}
	var tokens *oauthserver.Tokens
	var err error
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		tokens, err = s.OAuth.ExchangeCode(ctx, r.PostForm.Get("code"), clientID,
			r.PostForm.Get("redirect_uri"), r.PostForm.Get("code_verifier"), r.PostForm.Get("resource"))
	case "refresh_token":
		tokens, err = s.OAuth.Refresh(ctx, r.PostForm.Get("refresh_token"), clientID)
	default:
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "authorization_code, refresh_token 만 지원합니다")
		return
	}
	if err != nil {
		status := http.StatusBadRequest
		if oauthCode(err) == "server_error" {
			status = http.StatusInternalServerError
		}
		s.Audit.Write(ctx, audit.Entry{Category: audit.CatAuth, Action: "oauth.token", Success: false, IP: ip,
			ErrorCode: oauthCode(err), Message: err.Error(), Detail: map[string]any{"clientId": clientID}})
		writeOAuthError(w, status, oauthCode(err), err.Error())
		return
	}
	if tokens.Scope == "*" {
		tokens.Scope = ""
	}
	w.Header().Set("Pragma", "no-cache")
	httpx.JSON(w, http.StatusOK, tokens)
}

// revoke implements RFC 7009.
func (s *Server) revoke(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err == nil {
		_ = s.OAuth.Revoke(r.Context(), r.PostForm.Get("token"))
	}
	w.WriteHeader(http.StatusOK)
}

// oauthPage shows an authorization error in the browser.
func (s *Server) oauthPage(w http.ResponseWriter, r *http.Request, msg string) {
	httpx.NoCache(w)
	http.Redirect(w, r, "/oauth/consent?error="+url.QueryEscape(msg), http.StatusFound)
}

// keycloakHTTPClient is used for the console's Keycloak checks.
func keycloakHTTPClient(kc settings.Keycloak) *http.Client {
	tr := &http.Transport{Proxy: http.ProxyFromEnvironment, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	if kc.InsecureSkipTLS {
		tr.TLSClientConfig.InsecureSkipVerify = true // #nosec G402 - operator opt-in for internal CAs
	}
	return &http.Client{Timeout: 10 * time.Second, Transport: tr,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
