package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hkjang/confmcp/internal/audit"
	"github.com/hkjang/confmcp/internal/httpx"
	"github.com/hkjang/confmcp/internal/permission"
	"github.com/hkjang/confmcp/internal/settings"
	"github.com/hkjang/confmcp/internal/tools"
)

var settingGroups = []string{settings.KeyKeycloak, settings.KeyConfluence, settings.KeyPermission, settings.KeyLimits,
	settings.KeyAI, settings.KeySecurity, settings.KeyUI, settings.KeyKeyPolicy, settings.KeyMCP}

// maskedSettings returns a group with secrets replaced by presence flags.
func (s *Server) maskedSettings(ctx context.Context, group string) (any, error) {
	switch group {
	case settings.KeyKeycloak:
		cfg, err := s.Store.Keycloak(ctx)
		if err != nil {
			return nil, err
		}
		has := cfg.ClientSecretEnc != ""
		cfg.ClientSecretEnc = ""
		return map[string]any{"value": cfg, "secrets": map[string]bool{"clientSecret": has}}, nil
	case settings.KeyConfluence:
		cfg, err := s.Store.Confluence(ctx)
		if err != nil {
			return nil, err
		}
		has := cfg.ServicePasswordEnc != ""
		cfg.ServicePasswordEnc = ""
		return map[string]any{"value": cfg, "secrets": map[string]bool{"servicePassword": has}}, nil
	case settings.KeyPermission:
		cfg, err := s.Store.Permission(ctx)
		if err != nil {
			return nil, err
		}
		has := cfg.PluginSecretEnc != ""
		cfg.PluginSecretEnc = ""
		return map[string]any{"value": cfg, "secrets": map[string]bool{"pluginSecret": has}}, nil
	case settings.KeyLimits:
		cfg, err := s.Store.Limits(ctx)
		return map[string]any{"value": cfg, "defaults": settings.DefaultLimits()}, err
	case settings.KeyAI:
		cfg, err := s.Store.AI(ctx)
		if err != nil {
			return nil, err
		}
		has := cfg.APIKeyEnc != ""
		cfg.APIKeyEnc = ""
		return map[string]any{"value": cfg, "secrets": map[string]bool{"apiKey": has},
			"limits": map[string]any{"maxTokenCeiling": settings.MaxTokenCeiling}}, nil
	case settings.KeySecurity:
		cfg, err := s.Store.Security(ctx)
		return map[string]any{"value": cfg}, err
	case settings.KeyUI:
		cfg, err := s.Store.UI(ctx)
		return map[string]any{"value": cfg}, err
	case settings.KeyKeyPolicy:
		cfg, err := s.Store.KeyPolicy(ctx)
		return map[string]any{"value": cfg}, err
	case settings.KeyMCP:
		cfg, err := s.Store.MCP(ctx)
		return map[string]any{"value": cfg}, err
	default:
		return nil, errNotFound("알 수 없는 설정 그룹입니다: " + group)
	}
}

func (s *Server) getAllSettings(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{}
	for _, g := range settingGroups {
		v, err := s.maskedSettings(r.Context(), g)
		if err != nil {
			httpx.Fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		out[g] = v
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	v, err := s.maskedSettings(r.Context(), chi.URLParam(r, "group"))
	if err != nil {
		writeErr(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, v)
}

func badURL(w http.ResponseWriter, err error) {
	httpx.FailCode(w, http.StatusBadRequest, "INVALID_URL", err.Error())
}

// seal replaces a sealed field only when a new plaintext value was typed, so
// saving a form never wipes a stored credential the operator did not retype.
func (s *Server) seal(current string, typed string) (string, error) {
	if v := strings.TrimSpace(typed); v != "" {
		return s.Store.Seal(v)
	}
	return current, nil
}

// putSettings replaces a settings group.
func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	group := chi.URLParam(r, "group")
	actor := identityOf(r).User.Username

	var body struct {
		Value   map[string]any    `json:"value"`
		Secrets map[string]string `json:"secrets"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Fail(w, http.StatusBadRequest, "요청 형식이 올바르지 않습니다")
		return
	}
	var err error
	switch group {
	case settings.KeyKeycloak:
		cur, _ := s.Store.Keycloak(ctx)
		next := cur
		if e := remarshal(body.Value, &next); e != nil {
			httpx.Fail(w, http.StatusBadRequest, e.Error())
			return
		}
		if next.ClientSecretEnc, err = s.seal(cur.ClientSecretEnc, body.Secrets["clientSecret"]); err != nil {
			httpx.Fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		if next.UsernameClaim == "" {
			next.UsernameClaim = "preferred_username"
		}
		if next.RoleClaimPath == "" {
			next.RoleClaimPath = "realm_access.roles"
		}
		if len(next.Scopes) == 0 {
			next.Scopes = []string{"openid", "profile", "email"}
		}
		next.ClientID = strings.TrimSpace(next.ClientID)
		var verr error
		if next.Issuer, verr = httpx.CleanBaseURL("Issuer URL", next.Issuer, false); verr != nil {
			badURL(w, verr)
			return
		}
		if next.RedirectURL, verr = httpx.CleanRedirectURI("Redirect URI", next.RedirectURL, false); verr != nil {
			badURL(w, verr)
			return
		}
		if next.PostLogoutURL, verr = httpx.CleanRedirectURI("로그아웃 후 이동 URL", next.PostLogoutURL, false); verr != nil {
			badURL(w, verr)
			return
		}
		if next.Enabled && (next.Issuer == "" || next.ClientID == "") {
			httpx.FailCode(w, http.StatusBadRequest, "INVALID_URL", "Keycloak SSO 를 켜려면 Issuer URL 과 Client ID 가 필요합니다")
			return
		}
		if next.RedirectURL != "" && !strings.HasSuffix(next.RedirectURL, CallbackPath) {
			httpx.FailCode(w, http.StatusBadRequest, "INVALID_URL", "Redirect URI 는 "+CallbackPath+" 로 끝나야 합니다")
			return
		}
		if next.DefaultRole != "" && !strings.HasPrefix(next.DefaultRole, "confluence-mcp-") {
			httpx.Fail(w, http.StatusBadRequest, "기본 역할은 confluence-mcp-* 역할이어야 합니다")
			return
		}
		if next.AcceptKeycloakTokens && len(next.MCPAudiences) == 0 {
			httpx.Fail(w, http.StatusBadRequest, "Keycloak 토큰을 허용하려면 허용 audience 를 하나 이상 입력하십시오")
			return
		}
		err = s.Store.Put(ctx, group, next, actor)
		s.Auth.OIDC.Reset()

	case settings.KeyConfluence:
		cur, _ := s.Store.Confluence(ctx)
		next := cur
		if e := remarshal(body.Value, &next); e != nil {
			httpx.Fail(w, http.StatusBadRequest, e.Error())
			return
		}
		if next.ServicePasswordEnc, err = s.seal(cur.ServicePasswordEnc, body.Secrets["servicePassword"]); err != nil {
			httpx.Fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		var verr error
		if next.BaseURL, verr = httpx.CleanBaseURL("Confluence 기본 URL", next.BaseURL, false); verr != nil {
			badURL(w, verr)
			return
		}
		if next.ExecutionMode != "delegated" {
			next.ExecutionMode = "service"
		}
		if strings.TrimSpace(next.InstanceID) == "" {
			next.InstanceID = "default"
		}
		if next.TimeoutSec <= 0 || next.TimeoutSec > 120 {
			next.TimeoutSec = 15
		}
		if next.ToolTimeoutSec <= 0 || next.ToolTimeoutSec > 600 {
			next.ToolTimeoutSec = 30
		}
		// Detection results are written by the connection test only.
		next.DetectedVersion, next.DetectedBuild, next.DetectedAt = cur.DetectedVersion, cur.DetectedBuild, cur.DetectedAt
		if next.BaseURL != cur.BaseURL {
			next.DetectedVersion, next.DetectedBuild, next.DetectedAt = "", "", ""
		}
		err = s.Store.Put(ctx, group, next, actor)
		s.Provider.Reset()
		s.Resolver.Reset()

	case settings.KeyPermission:
		cur, _ := s.Store.Permission(ctx)
		next := cur
		if e := remarshal(body.Value, &next); e != nil {
			httpx.Fail(w, http.StatusBadRequest, e.Error())
			return
		}
		secret := body.Secrets["pluginSecret"]
		if secret == "generate" {
			b := make([]byte, 32)
			_, _ = rand.Read(b)
			secret = hex.EncodeToString(b)
		}
		if next.PluginSecretEnc, err = s.seal(cur.PluginSecretEnc, secret); err != nil {
			httpx.Fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		if next.Mode != "delegated" {
			next.Mode = "plugin"
		}
		var verr error
		if next.PluginBaseURL, verr = httpx.CleanBaseURL("권한 플러그인 URL", next.PluginBaseURL, false); verr != nil {
			badURL(w, verr)
			return
		}
		if next.CacheTTLSec < 0 || next.CacheTTLSec > 300 {
			next.CacheTTLSec = 0
		}
		if next.TimeoutSec <= 0 || next.TimeoutSec > 60 {
			next.TimeoutSec = 10
		}
		err = s.Store.Put(ctx, group, next, actor)
		s.Resolver.Reset()
		if err == nil && body.Secrets["pluginSecret"] == "generate" {
			// The generated secret is shown once so it can be entered in the plugin.
			s.Audit.Write(ctx, audit.Entry{Category: audit.CatAdmin, Action: "settings.update", KeycloakUsername: actor,
				Success: true, Detail: map[string]any{"group": group, "generated": true}})
			v, _ := s.maskedSettings(ctx, group)
			httpx.JSON(w, http.StatusOK, map[string]any{"settings": v, "generatedSecret": secret})
			return
		}

	case settings.KeyLimits:
		cur, _ := s.Store.Limits(ctx)
		next := cur
		if e := remarshal(body.Value, &next); e != nil {
			httpx.Fail(w, http.StatusBadRequest, e.Error())
			return
		}
		err = s.Store.Put(ctx, group, next.Normalize(), actor)

	case settings.KeyAI:
		cur, _ := s.Store.AI(ctx)
		next := cur
		if e := remarshal(body.Value, &next); e != nil {
			httpx.Fail(w, http.StatusBadRequest, e.Error())
			return
		}
		if next.APIKeyEnc, err = s.seal(cur.APIKeyEnc, body.Secrets["apiKey"]); err != nil {
			httpx.Fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		var verr error
		if next.BaseURL, verr = httpx.CleanBaseURL("AI 기본 URL", next.BaseURL, false); verr != nil {
			badURL(w, verr)
			return
		}
		if next.Provider != "openai-compatible" {
			next.Provider = "anthropic"
		}
		if next.MaxTokens > settings.MaxTokenCeiling {
			next.MaxTokens = settings.MaxTokenCeiling
		}
		if next.MaxTokens <= 0 {
			next.MaxTokens = 16384
		}
		if next.ContextLimit <= 0 || next.ContextLimit > settings.MaxTokenCeiling {
			next.ContextLimit = settings.MaxTokenCeiling
		}
		if next.TimeoutSec <= 0 {
			next.TimeoutSec = 600
		}
		models := []string{}
		for _, m := range next.Models {
			if m = strings.TrimSpace(m); m != "" {
				models = append(models, m)
			}
		}
		next.Models = models
		err = s.Store.Put(ctx, group, next, actor)

	case settings.KeySecurity:
		cur, _ := s.Store.Security(ctx)
		next := cur
		if e := remarshal(body.Value, &next); e != nil {
			httpx.Fail(w, http.StatusBadRequest, e.Error())
			return
		}
		if next.SessionTTLMinutes <= 0 {
			next.SessionTTLMinutes = 480
		}
		if next.ApprovalTTLMin <= 0 {
			next.ApprovalTTLMin = 30
		}
		if next.RateLimitPerMin <= 0 {
			next.RateLimitPerMin = 120
		}
		if next.RateLimitBurst <= 0 {
			next.RateLimitBurst = 40
		}
		err = s.Store.Put(ctx, group, next, actor)

	case settings.KeyUI:
		cur, _ := s.Store.UI(ctx)
		next := cur
		if e := remarshal(body.Value, &next); e != nil {
			httpx.Fail(w, http.StatusBadRequest, e.Error())
			return
		}
		if next.FontScale < 0.85 || next.FontScale > 1.4 {
			next.FontScale = 1.0
		}
		if strings.TrimSpace(next.ServiceName) == "" {
			next.ServiceName = "confmcp"
		}
		switch next.DefaultTheme {
		case "light", "dark", "system":
		default:
			next.DefaultTheme = "light"
		}
		err = s.Store.Put(ctx, group, next, actor)

	case settings.KeyKeyPolicy:
		cur, _ := s.Store.KeyPolicy(ctx)
		next := cur
		if e := remarshal(body.Value, &next); e != nil {
			httpx.Fail(w, http.StatusBadRequest, e.Error())
			return
		}
		if next.MaxKeysPerUser <= 0 {
			next.MaxKeysPerUser = 5
		}
		err = s.Store.Put(ctx, group, next, actor)

	case settings.KeyMCP:
		cur, _ := s.Store.MCP(ctx)
		next := cur
		if e := remarshal(body.Value, &next); e != nil {
			httpx.Fail(w, http.StatusBadRequest, e.Error())
			return
		}
		var verr error
		if next.ResourceURL, verr = httpx.CleanBaseURL("공개 URL", next.ResourceURL, false); verr != nil {
			badURL(w, verr)
			return
		}
		if next.MaxResponseKB <= 0 {
			next.MaxResponseKB = 512
		}
		err = s.Store.Put(ctx, group, next, actor)

	default:
		httpx.Fail(w, http.StatusNotFound, "알 수 없는 설정 그룹입니다")
		return
	}
	if err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Audit.Write(ctx, audit.Entry{Category: audit.CatAdmin, Action: "settings.update",
		KeycloakUsername: actor, Success: true, Detail: map[string]any{"group": group}})
	v, err := s.maskedSettings(ctx, group)
	if err != nil {
		writeErr(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, v)
}

// testTarget runs a connectivity check against a configured dependency.
func (s *Server) testTarget(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	ok := func(v map[string]any) { v["ok"] = true; httpx.JSON(w, http.StatusOK, v) }
	failed := func(err error, extra ...map[string]any) {
		out := map[string]any{"ok": false, "error": err.Error()}
		for _, e := range extra {
			for k, v := range e {
				out[k] = v
			}
		}
		httpx.JSON(w, http.StatusOK, out)
	}
	switch chi.URLParam(r, "target") {
	case "confluence":
		adapter, conf, err := s.Provider.Adapter(ctx)
		if err != nil {
			failed(err)
			return
		}
		cred, err := s.Provider.ServiceCredential(ctx)
		if err != nil {
			failed(err)
			return
		}
		info, err := adapter.Probe(ctx, cred)
		if err != nil {
			failed(err)
			return
		}
		out := map[string]any{"info": info, "note": "서비스 계정으로 확인한 결과입니다. 요청자 본인의 권한이 아닙니다."}
		warnings := []string{}
		if info.Version != "" {
			conf.DetectedVersion, conf.DetectedBuild = info.Version, info.BuildNumber
			conf.DetectedAt = time.Now().Format(time.RFC3339)
			_ = s.Store.Put(ctx, settings.KeyConfluence, conf, identityOf(r).User.Username)
			if !strings.HasPrefix(info.Version, "7.2") {
				warnings = append(warnings, "이 게이트웨이는 Confluence 7.2.0 기준으로 검증되었습니다. 감지된 버전: "+info.Version)
			}
		} else {
			warnings = append(warnings, "버전을 확인하지 못했습니다 (/rest/applinks/1.0/manifest)")
		}
		for name, okFeature := range info.Features {
			if !okFeature {
				warnings = append(warnings, "기능 확인 실패: "+name+" — 확인되지 않은 기능은 사용하지 마십시오")
			}
		}
		if spaces, _, err := adapter.Spaces(ctx, cred, 0, 5); err == nil {
			keys := []string{}
			for _, sp := range spaces {
				keys = append(keys, sp.Key)
			}
			out["sampleSpaces"] = keys
		}
		out["warnings"] = warnings
		ok(out)

	case "permission":
		h, err := s.Resolver.Health(ctx)
		if err != nil {
			failed(err, map[string]any{"health": h})
			return
		}
		out := map[string]any{"health": h}
		// Optional live decision for a user and target.
		username := strings.TrimSpace(r.URL.Query().Get("username"))
		if username != "" {
			m, err := s.findMappingByUsername(ctx, username)
			if err != nil {
				failed(err, out)
				return
			}
			var target permission.Target
			if id := r.URL.Query().Get("contentId"); id != "" {
				target = permission.ContentTarget(id)
			} else {
				target = permission.SpaceTarget(r.URL.Query().Get("spaceKey"))
			}
			var userID int64
			_ = s.Pool.QueryRow(ctx, `SELECT id FROM users WHERE username=$1`, m.KeycloakUsername).Scan(&userID)
			d, err := s.Resolver.Check(ctx, permission.Subject{UserID: userID, UserKey: m.ConfluenceUserKey, Username: m.ConfluenceUsername},
				target, permission.AllOps...)
			if err != nil {
				failed(err, out)
				return
			}
			out["decision"] = d
			s.Audit.Write(ctx, audit.Entry{Category: audit.CatAdmin, Action: "permission.test",
				KeycloakUsername: identityOf(r).User.Username, ConfluenceUsername: m.ConfluenceUsername, Success: true,
				Detail: map[string]any{"target": target}})
		}
		ok(out)

	case "keycloak":
		s.testKeycloak(w, r)

	case "mcp-oauth":
		kc, _ := s.Store.Keycloak(ctx)
		mcpCfg, _ := s.Store.MCP(ctx)
		iss := s.issuer(r)
		checks := []map[string]any{
			{"name": "MCP OAuth 사용", "ok": kc.MCPOAuthEnabled, "detail": "confmcp 가 MCP 클라이언트용 인가 서버로 동작합니다"},
			{"name": "사용자 로그인 수단", "ok": true, "detail": map[bool]string{true: "Keycloak SSO (사일런트 로그인 지원)", false: "로컬 계정 (Keycloak 미설정)"}[kc.Enabled]},
			{"name": "공개 URL", "ok": true, "detail": iss + map[bool]string{true: " (설정값)", false: " (요청 주소에서 유도 — 리버스 프록시 뒤라면 MCP 설정의 공개 URL 을 입력하십시오)"}[mcpCfg.ResourceURL != ""]},
		}
		clients, _ := s.OAuth.Clients(ctx)
		ok(map[string]any{
			"issuer": iss, "mcpUrl": iss + "/mcp",
			"protectedResourceMetadata": iss + "/.well-known/oauth-protected-resource/mcp",
			"authorizationServerMetadata": iss + "/.well-known/oauth-authorization-server",
			"checks": checks, "registeredClients": len(clients),
			"claudeCode": "claude mcp add --transport http confmcp " + iss + "/mcp",
		})

	case "ai":
		reply, err := s.AI.Test(ctx)
		if err != nil {
			failed(err)
			return
		}
		ok(map[string]any{"reply": reply})

	default:
		httpx.Fail(w, http.StatusNotFound, "알 수 없는 점검 대상입니다")
	}
}

// testKeycloak checks discovery and that Keycloak accepts the redirect URI,
// and reports the exact values to register.
func (s *Server) testKeycloak(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	kc, _, err := s.Auth.OIDC.Config(ctx)
	if err != nil {
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	s.Auth.OIDC.Reset()
	redirect := s.redirectURI(r, kc.RedirectURL)
	out := map[string]any{
		"redirectUri":   redirect,
		"postLogoutUri": kc.PostLogoutURL,
		"register": map[string]any{
			"clientType":                  "OpenID Connect · Client authentication ON (confidential) · Standard flow",
			"validRedirectUris":           []string{redirect},
			"webOrigins":                  []string{s.baseURL(r)},
			"validPostLogoutRedirectUris": postLogoutRegistrations(kc.PostLogoutURL),
			"roles":                       []string{tools.RoleReader, tools.RoleWriter, tools.RoleApprover, tools.RoleAdmin},
		},
	}
	warnings := []string{}
	if kc.PostLogoutURL == "" {
		warnings = append(warnings, "로그아웃 후 이동 URL 이 비어 있어 로그아웃 시 Keycloak 화면에 머무릅니다.")
	}
	cfg, _, err := s.Auth.OIDC.OAuth2Config(ctx, redirect)
	if err != nil {
		out["ok"], out["error"], out["warnings"] = false, err.Error(), warnings
		httpx.JSON(w, http.StatusOK, out)
		return
	}
	out["ok"] = true
	out["authUrl"], out["tokenUrl"], out["scopes"] = cfg.Endpoint.AuthURL, cfg.Endpoint.TokenURL, cfg.Scopes
	check := checkRedirect(ctx, kc, cfg.Endpoint.AuthURL, kc.ClientID, redirect)
	out["redirectCheck"] = check
	switch {
	case check.Error != "":
		warnings = append(warnings, "Redirect URI 를 Keycloak 에 확인하지 못했습니다: "+check.Error)
	case check.ClientMissing:
		out["ok"] = false
		out["error"] = fmt.Sprintf("Keycloak 에 Client ID %s 가 없습니다 (%s)", kc.ClientID, check.Detail)
	case !check.Accepted:
		out["ok"] = false
		out["error"] = fmt.Sprintf("Keycloak 이 Redirect URI %s 를 거부합니다 (%s). Valid redirect URIs 에 그대로 등록하십시오", redirect, check.Detail)
	}
	if len(warnings) > 0 {
		out["warnings"] = warnings
	}
	httpx.JSON(w, http.StatusOK, out)
}

func postLogoutRegistrations(configured string) []string {
	if strings.TrimSpace(configured) == "" {
		return []string{}
	}
	return []string{configured}
}
