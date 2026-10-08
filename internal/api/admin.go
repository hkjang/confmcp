package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hkjang/confmcp/internal/apikey"
	"github.com/hkjang/confmcp/internal/httpx"
	"github.com/hkjang/confmcp/internal/tools"
	"github.com/hkjang/confmcp/internal/version"
)

// mountAdmin registers the service administrator routes.
func (s *Server) mountAdmin(r chi.Router) {
	r.Get("/dashboard", s.adminDashboard)
	r.Get("/system", s.adminSystem)
	r.Get("/stats", s.adminStats)

	r.Get("/settings", s.getAllSettings)
	r.Get("/settings/{group}", s.getSettings)
	r.Put("/settings/{group}", s.putSettings)
	r.Post("/test/{target}", s.testTarget)
	r.Get("/mcp-oauth/trace", s.mcpOAuthTrace)
	r.Get("/roles", func(w http.ResponseWriter, r *http.Request) { httpx.JSON(w, http.StatusOK, tools.Roles()) })

	r.Get("/users", s.listUsers)
	r.Post("/users", s.createUser)
	r.Patch("/users/{id}", s.updateUser)
	r.Post("/users/{id}/password", s.setUserPassword)
	r.Delete("/users/{id}", s.deleteUser)

	r.Get("/identity/mappings", s.listMappings)
	r.Post("/identity/mappings", s.createMapping)
	r.Post("/identity/mappings/{sub}/verify", s.verifyMapping)
	r.Post("/identity/mappings/{sub}/active", s.setMappingActive)
	r.Delete("/identity/mappings/{sub}", s.deleteMapping)
	r.Get("/identity/history", s.mappingHistory)
	r.Get("/identity/errors", s.listMappingErrors)
	r.Delete("/identity/errors", s.clearMappingErrors)

	r.Get("/tools", s.listTools)
	r.Patch("/tools/{name}", s.patchTool)
	r.Post("/tools/sync", s.syncTools)

	r.Get("/policy/rules", s.listRules)
	r.Post("/policy/rules", s.createRule)
	r.Put("/policy/rules/{id}", s.updateRule)
	r.Delete("/policy/rules/{id}", s.deleteRule)
	r.Post("/policy/evaluate", s.evaluatePolicy)

	r.Get("/approvals", s.listApprovals)
	r.Get("/approvals/{id}", s.approvalDetail)
	r.Post("/approvals/{id}/decide", s.decideApprovalHTTP)
	r.Get("/operations", s.listOperations)
	r.Post("/operations/{id}/resolve", s.resolveOperation)

	r.Get("/keys", s.listAllKeys)
	r.Post("/keys/{id}/revoke", s.revokeAnyKey)
	r.Post("/keys/{id}/rotate", s.rotateAnyKey)
	r.Get("/key-roles", s.keyRoles)
	r.Put("/key-roles/{name}", s.saveKeyRole)
	r.Delete("/key-roles/{name}", s.deleteKeyRole)
	r.Get("/key-scopes", func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, http.StatusOK, apikey.AllScopes())
	})

	r.Get("/oauth/clients", s.listOAuthClients)
	r.Delete("/oauth/clients/{id}", s.disableOAuthClient)
	r.Get("/oauth/grants", s.listOAuthGrants)
	r.Delete("/oauth/grants/{id}", s.revokeMyConnection)

	r.Get("/audit", s.listAudit)
	r.Post("/audit/purge", s.purgeAudit)
	r.Get("/sessions", s.listMCPSessions)
}

// ---------- dashboard & system ----------

func (s *Server) adminDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := map[string]any{"version": version.Current()}

	counts := map[string]int{}
	for key, query := range map[string]string{
		"users":            `SELECT COUNT(*) FROM users WHERE active`,
		"mappings":         `SELECT COUNT(*) FROM confluence_identity_mapping WHERE active`,
		"mappingErrors":    `SELECT COUNT(*) FROM identity_mapping_errors`,
		"activeKeys":       `SELECT COUNT(*) FROM api_keys WHERE revoked_at IS NULL AND (expires_at IS NULL OR expires_at > NOW())`,
		"rotationDue":      `SELECT COUNT(*) FROM api_keys WHERE revoked_at IS NULL AND rotation_due_at < NOW()`,
		"pendingApprovals": `SELECT COUNT(*) FROM approval_requests WHERE status='pending' AND expires_at > NOW()`,
		"enabledTools":     `SELECT COUNT(*) FROM mcp_tools WHERE enabled`,
		"policyRules":      `SELECT COUNT(*) FROM policy_rules`,
		"allowedSpaces":    `SELECT COUNT(*) FROM policy_rules WHERE kind='space' AND effect='allow'`,
		"mcpSessions":      `SELECT COUNT(*) FROM mcp_sessions WHERE closed_at IS NULL AND last_seen_at > NOW() - INTERVAL '1 hour'`,
		"oauthGrants":      `SELECT COUNT(*) FROM oauth_grants WHERE revoked_at IS NULL`,
		"outcomeUnknown":   `SELECT COUNT(*) FROM operation_records WHERE status='outcome_unknown'`,
	} {
		var n int
		if err := s.Pool.QueryRow(ctx, query).Scan(&n); err == nil {
			counts[key] = n
		}
	}
	out["counts"] = counts

	if st, err := s.Audit.Summary(ctx, 24*time.Hour, "hour"); err == nil {
		out["stats24h"] = st
	}
	if rows, err := s.Pool.Query(ctx, `
		SELECT tool_name, COUNT(*), COUNT(*) FILTER (WHERE NOT success) FROM audit_log
		WHERE tool_name IS NOT NULL AND occurred_at > NOW() - INTERVAL '7 days'
		GROUP BY tool_name ORDER BY COUNT(*) DESC LIMIT 8`); err == nil {
		defer rows.Close()
		top := []map[string]any{}
		for rows.Next() {
			var name string
			var n, f int
			if rows.Scan(&name, &n, &f) == nil {
				top = append(top, map[string]any{"tool": name, "calls": n, "failures": f})
			}
		}
		out["topTools"] = top
	}
	if recent, _, err := s.Audit.List(ctx, auditQuery(12)); err == nil {
		out["recentAudit"] = recent
	}
	health := s.healthSnapshot(ctx)
	out["health"] = health
	out["warnings"] = s.setupWarnings(ctx, counts, health)
	httpx.JSON(w, http.StatusOK, out)
}

// setupWarnings lists configuration problems in the order to fix them.
func (s *Server) setupWarnings(ctx context.Context, counts map[string]int, health map[string]any) []map[string]string {
	out := []map[string]string{}
	add := func(level, msg, link string) {
		out = append(out, map[string]string{"level": level, "message": msg, "link": link})
	}
	conf, _ := s.Store.Confluence(ctx)
	kc, _ := s.Store.Keycloak(ctx)
	if conf.BaseURL == "" {
		add("error", "Confluence 연결이 설정되지 않았습니다", "/admin/confluence")
	}
	if c, ok := health["permission"].(map[string]any); ok && c["ok"] == false {
		add("error", "권한 판정을 사용할 수 없어 모든 도구가 PERMISSION_UNKNOWN 으로 차단됩니다", "/admin/permission")
	}
	if counts["allowedSpaces"] == 0 {
		add("warning", "MCP 접근이 허용된 공간이 없습니다. 정책에서 공간 허용 규칙을 추가하십시오", "/admin/policy")
	}
	if !kc.Enabled {
		add("info", "Keycloak SSO 가 꺼져 있습니다. 로컬 계정으로만 로그인할 수 있습니다", "/admin/auth")
	}
	if counts["outcomeUnknown"] > 0 {
		add("warning", "결과가 불명확한 쓰기 작업이 있습니다. Confluence 에서 확인한 뒤 정리하십시오", "/admin/operations")
	}
	if counts["mappingErrors"] > 0 {
		add("info", "사용자 매핑 실패가 있습니다", "/admin/identity")
	}
	return out
}

func component(ok bool, detail string) map[string]any { return map[string]any{"ok": ok, "detail": detail} }

func detailOf(err error, okDetail string) string {
	if err != nil {
		return err.Error()
	}
	return okDetail
}

// healthSnapshot checks every dependency. Upstream problems are shown to the
// administrator here; they do not fail /readyz.
func (s *Server) healthSnapshot(ctx context.Context) map[string]any {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	out := map[string]any{}
	out["database"] = component(s.Pool.Ping(ctx) == nil, "PostgreSQL")

	kc, err := s.Store.Keycloak(ctx)
	switch {
	case err != nil:
		out["keycloak"] = component(false, err.Error())
	case !kc.Enabled:
		out["keycloak"] = map[string]any{"ok": true, "detail": "비활성(로컬 로그인만)", "skipped": true}
	default:
		_, _, kerr := s.Auth.OIDC.OAuth2Config(ctx, kc.RedirectURL)
		out["keycloak"] = component(kerr == nil, detailOf(kerr, "디스커버리 정상"))
	}

	conf, _ := s.Store.Confluence(ctx)
	if conf.BaseURL == "" {
		out["confluence"] = component(false, "Confluence 미설정")
	} else if adapter, _, aerr := s.Provider.Adapter(ctx); aerr != nil {
		out["confluence"] = component(false, aerr.Error())
	} else if cred, cerr := s.Provider.ServiceCredential(ctx); cerr != nil {
		out["confluence"] = map[string]any{"ok": conf.ExecutionMode == "delegated", "detail": cerr.Error(), "skipped": conf.ExecutionMode == "delegated"}
	} else if u, perr := adapter.CurrentUser(ctx, cred); perr != nil {
		out["confluence"] = component(false, perr.Error())
	} else {
		detail := conf.BaseURL + " · 서비스 계정 " + u.Username
		if conf.DetectedVersion != "" {
			detail += " · v" + conf.DetectedVersion
		}
		out["confluence"] = component(true, detail)
	}

	if h, err := s.Resolver.Health(ctx); err != nil {
		out["permission"] = component(false, err.Error())
	} else {
		detail := "모드: " + toString(h["mode"])
		if ph, ok := h["plugin"]; ok && ph != nil {
			detail += " · 플러그인 정상"
		}
		out["permission"] = component(true, detail)
	}

	ai, _ := s.Store.AI(ctx)
	if ai.Enabled {
		out["ai"] = map[string]any{"ok": true, "detail": ai.Provider + " / " + ai.Model}
	} else {
		out["ai"] = map[string]any{"ok": true, "detail": "비활성", "skipped": true}
	}
	return out
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func (s *Server) adminSystem(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := map[string]any{"version": version.Current(), "health": s.healthSnapshot(ctx), "startedAt": s.booted}

	var dbVersion, dbName, dbSize string
	_ = s.Pool.QueryRow(ctx, `SELECT version()`).Scan(&dbVersion)
	_ = s.Pool.QueryRow(ctx, `SELECT current_database()`).Scan(&dbName)
	_ = s.Pool.QueryRow(ctx, `SELECT pg_size_pretty(pg_database_size(current_database()))`).Scan(&dbSize)
	out["database"] = map[string]any{"version": dbVersion, "name": dbName, "size": dbSize}

	if rows, err := s.Pool.Query(ctx, `SELECT version, applied_at FROM schema_migrations ORDER BY version`); err == nil {
		defer rows.Close()
		migrations := []map[string]any{}
		for rows.Next() {
			var v string
			var at time.Time
			if rows.Scan(&v, &at) == nil {
				migrations = append(migrations, map[string]any{"version": v, "appliedAt": at})
			}
		}
		out["migrations"] = migrations
	}
	stat := s.Pool.Stat()
	out["pool"] = map[string]any{"total": stat.TotalConns(), "idle": stat.IdleConns(), "acquired": stat.AcquiredConns()}
	out["environment"] = []string{"DATABASE_URL", "BOOTSTRAP_ADMIN", "BOOTSTRAP_ADMIN_PASSWORD", "ENCRYPTION_KEY"}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Server) adminStats(w http.ResponseWriter, r *http.Request) {
	window := 24 * time.Hour
	bucket := "hour"
	if r.URL.Query().Get("range") == "7d" {
		window, bucket = 7*24*time.Hour, "day"
	}
	if r.URL.Query().Get("range") == "30d" {
		window, bucket = 30*24*time.Hour, "day"
	}
	st, err := s.Audit.Summary(r.Context(), window, bucket)
	if err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, st)
}

// ready reports whether this process can serve: the database answers and the
// settings can be read. Confluence or IdP outages are shown to administrators
// instead, so an upstream problem does not take the console down with it.
func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	dbErr := s.Pool.Ping(ctx)
	_, setErr := s.Store.Security(ctx)
	conf, _ := s.Store.Confluence(ctx)
	ready := dbErr == nil && setErr == nil
	status := http.StatusOK
	if !ready {
		status = http.StatusServiceUnavailable
	}
	httpx.JSON(w, status, map[string]any{
		"ready":    ready,
		"database": component(dbErr == nil, detailOf(dbErr, "ok")),
		"settings": component(setErr == nil, detailOf(setErr, "ok")),
		"confluenceConfigured": conf.BaseURL != "",
		"version":  version.Version,
	})
}

// metrics exposes Prometheus text-format counters built from the audit log.
func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var sb strings.Builder
	emit := func(name, help, typ string, value int, labels string) {
		sb.WriteString("# HELP " + name + " " + help + "\n")
		sb.WriteString("# TYPE " + name + " " + typ + "\n")
		sb.WriteString(name + labels + " " + strconv.Itoa(value) + "\n")
	}
	count := func(query string) int {
		var n int
		_ = s.Pool.QueryRow(ctx, query).Scan(&n)
		return n
	}
	emit("confmcp_build_info", "빌드 정보", "gauge", 1, `{version="`+version.Version+`",commit="`+version.Commit+`"}`)
	emit("confmcp_tool_requests_total", "도구 호출 수", "counter", count(`SELECT COUNT(*) FROM audit_log WHERE tool_name IS NOT NULL`), "")
	emit("confmcp_tool_errors_total", "도구 실패 수", "counter", count(`SELECT COUNT(*) FROM audit_log WHERE success=FALSE AND tool_name IS NOT NULL`), "")
	emit("confmcp_permission_denied_total", "권한·정책 거부 수", "counter",
		count(`SELECT COUNT(*) FROM audit_log WHERE error_code IN ('PERMISSION_DENIED','POLICY_DENIED','PERMISSION_UNKNOWN')`), "")
	emit("confmcp_identity_mapping_errors", "식별 매핑 실패 수", "gauge", count(`SELECT COUNT(*) FROM identity_mapping_errors`), "")
	emit("confmcp_approval_pending", "대기 중 승인 수", "gauge", count(`SELECT COUNT(*) FROM approval_requests WHERE status='pending' AND expires_at > NOW()`), "")
	emit("confmcp_operation_outcome_unknown", "결과 불명확 쓰기 수", "gauge", count(`SELECT COUNT(*) FROM operation_records WHERE status='outcome_unknown'`), "")
	emit("confmcp_api_keys_active", "활성 API 키 수", "gauge", count(`SELECT COUNT(*) FROM api_keys WHERE revoked_at IS NULL`), "")
	emit("confmcp_oauth_grants_active", "활성 MCP OAuth 연결 수", "gauge", count(`SELECT COUNT(*) FROM oauth_grants WHERE revoked_at IS NULL`), "")
	emit("confmcp_mcp_sessions_active", "활성 MCP 세션 수", "gauge",
		count(`SELECT COUNT(*) FROM mcp_sessions WHERE closed_at IS NULL AND last_seen_at > NOW() - INTERVAL '1 hour'`), "")
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(sb.String()))
}
