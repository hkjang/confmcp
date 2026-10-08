package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/hkjang/confmcp/internal/apikey"
	"github.com/hkjang/confmcp/internal/audit"
	"github.com/hkjang/confmcp/internal/httpx"
	"github.com/hkjang/confmcp/internal/version"
)


// ---------- users ----------

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.Users.List(r.Context(), r.URL.Query().Get("q"), queryInt(r, "limit", 200))
	if err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, users)
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username    string   `json:"username"`
		Password    string   `json:"password"`
		DisplayName string   `json:"displayName"`
		Email       string   `json:"email"`
		IsAdmin     bool     `json:"isServiceAdmin"`
		Roles       []string `json:"roles"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Fail(w, http.StatusBadRequest, "요청 형식이 올바르지 않습니다")
		return
	}
	if strings.TrimSpace(body.Username) == "" || len(body.Password) < 10 {
		httpx.Fail(w, http.StatusBadRequest, "사용자명과 10자 이상의 비밀번호가 필요합니다")
		return
	}
	u, err := s.Users.CreateLocal(r.Context(), strings.TrimSpace(body.Username), body.Password,
		body.DisplayName, body.Email, body.IsAdmin, body.Roles)
	if err != nil {
		httpx.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	s.Audit.Write(r.Context(), audit.Entry{
		Category: audit.CatAdmin, Action: "user.create",
		KeycloakUsername: identityOf(r).User.Username, Success: true,
		Detail: map[string]any{"target": u.Username},
	})
	httpx.JSON(w, http.StatusCreated, u)
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Fail(w, http.StatusBadRequest, "ID 형식이 올바르지 않습니다")
		return
	}
	target, err := s.Users.ByID(ctx, id)
	if err != nil {
		httpx.Fail(w, http.StatusNotFound, "사용자를 찾을 수 없습니다")
		return
	}
	var body struct {
		DisplayName string   `json:"displayName"`
		Email       string   `json:"email"`
		IsAdmin     bool     `json:"isServiceAdmin"`
		Active      bool     `json:"active"`
		Roles       []string `json:"roles"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Fail(w, http.StatusBadRequest, "요청 형식이 올바르지 않습니다")
		return
	}
	// Never let the last administrator remove their own access.
	if target.IsServiceAdmin && (!body.IsAdmin || !body.Active) {
		if n, err := s.Users.CountAdmins(ctx); err == nil && n <= 1 {
			httpx.Fail(w, http.StatusConflict, "마지막 서비스 관리자는 변경할 수 없습니다")
			return
		}
	}
	if err := s.Users.Update(ctx, id, body.DisplayName, body.Email,
		body.IsAdmin, body.Active, body.Roles); err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !body.Active {
		_ = s.Sessions.RevokeUser(ctx, id)
		_ = s.OAuth.RevokeUser(ctx, id)
	}
	s.Audit.Write(ctx, audit.Entry{Category: audit.CatAdmin, Action: "user.update",
		KeycloakUsername: identityOf(r).User.Username, Success: true,
		Detail: map[string]any{"target": target.Username, "roles": body.Roles, "active": body.Active, "admin": body.IsAdmin}})
	updated, _ := s.Users.ByID(ctx, id)
	httpx.JSON(w, http.StatusOK, updated)
}

func (s *Server) setUserPassword(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Fail(w, http.StatusBadRequest, "ID 형식이 올바르지 않습니다")
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := httpx.Decode(r, &body); err != nil || len(body.Password) < 10 {
		httpx.Fail(w, http.StatusBadRequest, "10자 이상의 비밀번호가 필요합니다")
		return
	}
	if err := s.Users.SetPassword(r.Context(), id, body.Password); err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.Sessions.RevokeUser(r.Context(), id)
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Fail(w, http.StatusBadRequest, "ID 형식이 올바르지 않습니다")
		return
	}
	if id == identityOf(r).User.ID {
		httpx.Fail(w, http.StatusConflict, "자기 계정은 삭제할 수 없습니다")
		return
	}
	target, err := s.Users.ByID(ctx, id)
	if err == nil && target.IsServiceAdmin {
		if n, err := s.Users.CountAdmins(ctx); err == nil && n <= 1 {
			httpx.Fail(w, http.StatusConflict, "마지막 서비스 관리자는 삭제할 수 없습니다")
			return
		}
	}
	if err := s.Users.Delete(ctx, id); err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------- keys ----------

func (s *Server) listAllKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := s.Keys.List(r.Context(), int64(queryInt(r, "userId", 0)))
	if err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, keys)
}

func (s *Server) revokeAnyKey(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.Fail(w, http.StatusBadRequest, "ID 형식이 올바르지 않습니다")
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = httpx.Decode(r, &body)
	if body.Reason == "" {
		body.Reason = "관리자 폐기"
	}
	if err := s.Keys.Revoke(r.Context(), id, body.Reason); err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Audit.Write(r.Context(), audit.Entry{
		Category: audit.CatKey, Action: "key.revoke.admin",
		KeycloakUsername: identityOf(r).User.Username, Success: true,
		Detail: map[string]any{"keyId": id, "reason": body.Reason},
	})
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) rotateAnyKey(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.Fail(w, http.StatusBadRequest, "ID 형식이 올바르지 않습니다")
		return
	}
	issued, err := s.Keys.Rotate(r.Context(), id)
	if err != nil {
		httpx.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, issued)
}

func (s *Server) saveKeyRole(w http.ResponseWriter, r *http.Request) {
	var role apikey.Role
	if err := httpx.Decode(r, &role); err != nil {
		httpx.Fail(w, http.StatusBadRequest, "요청 형식이 올바르지 않습니다")
		return
	}
	role.Name = chi.URLParam(r, "name")
	if err := s.Keys.SaveRole(r.Context(), role); err != nil {
		httpx.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	s.Audit.Write(r.Context(), audit.Entry{
		Category: audit.CatAdmin, Action: "key_role.save",
		KeycloakUsername: identityOf(r).User.Username, Success: true,
		Detail: map[string]any{"role": role.Name, "scopes": role.Scopes},
	})
	out, err := s.Keys.Role(r.Context(), role.Name)
	if err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Server) deleteKeyRole(w http.ResponseWriter, r *http.Request) {
	if err := s.Keys.DeleteRole(r.Context(), chi.URLParam(r, "name")); err != nil {
		httpx.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------- audit & sessions ----------

func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) {
	q := audit.Query{
		Category: r.URL.Query().Get("category"),
		Username: r.URL.Query().Get("username"),
		Tool:     r.URL.Query().Get("tool"),
		Space:    r.URL.Query().Get("space"),
		Content:  r.URL.Query().Get("contentId"),
		Code:     r.URL.Query().Get("code"),
		Success:  queryBoolPtr(r, "success"),
		Limit:    queryInt(r, "limit", 100),
		Offset:   queryInt(r, "offset", 0),
	}
	if since := strings.TrimSpace(r.URL.Query().Get("since")); since != "" {
		if t, err := parseTimeParam(since); err == nil {
			q.Since = &t
		}
	}
	if until := strings.TrimSpace(r.URL.Query().Get("until")); until != "" {
		if t, err := parseTimeParam(until); err == nil {
			q.Until = &t
		}
	}
	entries, total, err := s.Audit.List(r.Context(), q)
	if err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"values": entries, "total": total})
}

func (s *Server) purgeAudit(w http.ResponseWriter, r *http.Request) {
	sec, _ := s.Store.Security(r.Context())
	if err := s.Audit.Purge(r.Context(), sec.AuditRetainDays); err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "retainDays": sec.AuditRetainDays})
}

func (s *Server) listMCPSessions(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Pool.Query(r.Context(), `
		SELECT id, username, client_name, auth_mode, COALESCE(ip,''),
		       created_at, last_seen_at, closed_at
		FROM mcp_sessions ORDER BY last_seen_at DESC LIMIT $1`, queryInt(r, "limit", 100))
	if err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var username, client, mode, ip string
		var created, lastSeen time.Time
		var closed *time.Time
		if err := rows.Scan(&id, &username, &client, &mode, &ip, &created, &lastSeen, &closed); err != nil {
			continue
		}
		out = append(out, map[string]any{
			"id": id, "username": username, "client": client, "authMode": mode,
			"ip": ip, "createdAt": created, "lastSeenAt": lastSeen, "closedAt": closed,
		})
	}
	httpx.JSON(w, http.StatusOK, out)
}

// mcpOAuthTrace returns the OAuth discovery steps this process has seen since
// it started, newest first.
func (s *Server) mcpOAuthTrace(w http.ResponseWriter, r *http.Request) {
	sec, _ := s.Store.Security(r.Context())
	events := []traceEvent{}
	if s.trace != nil {
		events = s.trace.snapshot()
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"since":             s.booted,
		"version":           version.Current(),
		"trustProxyHeaders": sec.TrustProxyHeaders,
		"events":            events,
	})
}

// parseTimeParam accepts RFC 3339 or a plain date.
func parseTimeParam(v string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	return time.ParseInLocation("2006-01-02", v, time.Local)
}
