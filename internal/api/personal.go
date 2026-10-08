package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/hkjang/confmcp/internal/apikey"
	"github.com/hkjang/confmcp/internal/approval"
	"github.com/hkjang/confmcp/internal/audit"
	"github.com/hkjang/confmcp/internal/auth"
	"github.com/hkjang/confmcp/internal/confluence"
	"github.com/hkjang/confmcp/internal/httpx"
	"github.com/hkjang/confmcp/internal/permission"
	"github.com/hkjang/confmcp/internal/tools"
	"github.com/hkjang/confmcp/internal/version"
)

// Prefs are the per-user preferences of the personal pages.
type Prefs struct {
	Theme     string  `json:"theme"`
	FontScale float64 `json:"fontScale"`
	Locale    string  `json:"locale"`
}

func defaultPrefs() Prefs { return Prefs{Theme: "system", FontScale: 1.0, Locale: "ko"} }

func (s *Server) prefs(ctx context.Context, userID int64) Prefs {
	p := defaultPrefs()
	_ = s.Pool.QueryRow(ctx, `SELECT theme, font_scale, locale FROM user_prefs WHERE user_id=$1`, userID).
		Scan(&p.Theme, &p.FontScale, &p.Locale)
	return p
}

// principal builds the tool principal of a console request.
func (s *Server) principal(r *http.Request, id *auth.Identity) tools.Principal {
	sec, _ := s.Store.Security(r.Context())
	return id.Principal("console", httpx.ClientIP(r, sec.TrustProxyHeaders), middleware.GetReqID(r.Context()))
}

// decorate builds the payload the SPA uses to render a signed-in user.
func (s *Server) decorate(r *http.Request, id *auth.Identity) map[string]any {
	ctx := r.Context()
	ui, _ := s.Store.UI(ctx)
	conf, _ := s.Store.Confluence(ctx)
	perm, _ := s.Store.Permission(ctx)
	delegated := conf.ExecutionMode == "delegated" || perm.Mode == "delegated"
	return map[string]any{
		"user":           id.User,
		"confluence":     id.Mapping,
		"mappingError":   id.MappingErr,
		"roles":          id.Roles,
		"scopes":         id.Scopes,
		"authMode":       id.AuthMode,
		"isServiceAdmin": id.User.IsServiceAdmin,
		"canApprove":     tools.CanApprove(id.Roles),
		"prefs":          s.prefs(ctx, id.User.ID),
		"ui":             ui,
		"version":        version.Current(),
		"connection": map[string]any{
			"permissionMode":      perm.Mode,
			"executionMode":       conf.ExecutionMode,
			"allowUserCredential": conf.AllowUserCredential || delegated,
			"needsUserCredential": delegated,
			"configured":          conf.BaseURL != "",
		},
	}
}

// mountPersonal registers the self-service routes available to every user.
func (s *Server) mountPersonal(r chi.Router) {
	r.Route("/me", func(r chi.Router) {
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			httpx.JSON(w, http.StatusOK, s.decorate(r, identityOf(r)))
		})
		r.Put("/prefs", s.savePrefs)
		r.Post("/password", s.changePassword)

		r.Get("/keys", s.myKeys)
		r.Post("/keys", s.createMyKey)
		r.Post("/keys/{id}/rotate", s.rotateMyKey)
		r.Patch("/keys/{id}", s.patchMyKey)
		r.Delete("/keys/{id}", s.revokeMyKey)
		r.Get("/key-roles", s.keyRoles)
		r.Get("/key-scopes", func(w http.ResponseWriter, r *http.Request) {
			httpx.JSON(w, http.StatusOK, apikey.AllScopes())
		})

		r.Get("/confluence", s.myConfluence)
		r.Put("/confluence/credential", s.saveMyCredential)
		r.Post("/confluence/credential/verify", s.verifyMyCredential)
		r.Delete("/confluence/credential", s.deleteMyCredential)
		r.Post("/confluence/mapping/verify", s.verifyMyMapping)

		r.Get("/approvals", s.myApprovals)
		r.Get("/approvals/{id}", s.approvalDetail)
		r.Post("/approvals/{id}/decide", s.decideApprovalHTTP)
		r.Get("/approval-queue", s.approvalQueue)
		r.Get("/operations", s.myOperations)

		r.Get("/uploads", s.myUploads)
		r.Post("/uploads", s.uploadFile)
		r.Delete("/uploads/{id}", s.deleteUpload)

		r.Get("/connections", s.myConnections)
		r.Delete("/connections/{id}", s.revokeMyConnection)

		r.Get("/audit", s.myAudit)
		r.Get("/tools", s.myTools)
		r.Get("/permissions", s.myPermissions)
		r.Get("/mcp-config", s.mcpConfig)
	})
}

func (s *Server) savePrefs(w http.ResponseWriter, r *http.Request) {
	id := identityOf(r)
	var body Prefs
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Fail(w, http.StatusBadRequest, "요청 형식이 올바르지 않습니다")
		return
	}
	if body.FontScale < 0.85 || body.FontScale > 1.4 {
		body.FontScale = 1.0
	}
	switch body.Theme {
	case "light", "dark", "system":
	default:
		body.Theme = "system"
	}
	if body.Locale == "" {
		body.Locale = "ko"
	}
	if _, err := s.Pool.Exec(r.Context(), `
		INSERT INTO user_prefs(user_id, theme, font_scale, locale, updated_at)
		VALUES ($1,$2,$3,$4,NOW())
		ON CONFLICT (user_id) DO UPDATE
		   SET theme=EXCLUDED.theme, font_scale=EXCLUDED.font_scale, locale=EXCLUDED.locale, updated_at=NOW()`,
		id.User.ID, body.Theme, body.FontScale, body.Locale); err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, body)
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	id := identityOf(r)
	var body struct {
		Current string `json:"currentPassword"`
		New     string `json:"newPassword"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Fail(w, http.StatusBadRequest, "요청 형식이 올바르지 않습니다")
		return
	}
	if len(body.New) < 10 {
		httpx.Fail(w, http.StatusBadRequest, "새 비밀번호는 10자 이상이어야 합니다")
		return
	}
	if !id.User.HasPassword {
		httpx.Fail(w, http.StatusBadRequest, "SSO 전용 계정은 비밀번호를 사용하지 않습니다")
		return
	}
	if _, _, _, err := s.Auth.LoginLocal(r.Context(), id.User.Username, body.Current, "", ""); err != nil {
		httpx.Fail(w, http.StatusForbidden, "현재 비밀번호가 올바르지 않습니다")
		return
	}
	if err := s.Users.SetPassword(r.Context(), id.User.ID, body.New); err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Audit.Write(r.Context(), audit.Entry{Category: audit.CatAuth, Action: "password.change",
		KeycloakUsername: id.User.Username, Success: true})
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------- API keys ----------

func (s *Server) myKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := s.Keys.List(r.Context(), identityOf(r).User.ID)
	if err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, keys)
}

func (s *Server) createMyKey(w http.ResponseWriter, r *http.Request) {
	id := identityOf(r)
	pol, _ := s.Store.KeyPolicy(r.Context())
	if !pol.AllowSelfCreate && !id.User.IsServiceAdmin {
		httpx.Fail(w, http.StatusForbidden, "관리자 정책으로 개인 키 발급이 제한되어 있습니다")
		return
	}
	var body struct {
		Name    string   `json:"name"`
		Role    string   `json:"role"`
		Scopes  []string `json:"scopes"`
		TTLDays int      `json:"ttlDays"`
	}
	if err := httpx.Decode(r, &body); err != nil || strings.TrimSpace(body.Name) == "" {
		httpx.Fail(w, http.StatusBadRequest, "키 이름이 필요합니다")
		return
	}
	issued, err := s.Keys.Create(r.Context(), id.User.ID, body.Name, body.Role, body.Scopes, body.TTLDays)
	if err != nil {
		httpx.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	s.Audit.Write(r.Context(), audit.Entry{Category: audit.CatKey, Action: "key.create", KeycloakUsername: id.User.Username,
		Success: true, Detail: map[string]any{"keyId": issued.Key.ID, "role": issued.Key.Role}})
	httpx.JSON(w, http.StatusCreated, issued)
}

// ownedKey loads a key and verifies the caller owns it (or is an admin).
func (s *Server) ownedKey(r *http.Request) (*apikey.Key, error) {
	id := identityOf(r)
	keyID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return nil, errBadRequest("키 ID 형식이 올바르지 않습니다")
	}
	key, err := s.Keys.ByID(r.Context(), keyID)
	if err != nil {
		return nil, errBadRequest("키를 찾을 수 없습니다")
	}
	if key.UserID != id.User.ID && !id.User.IsServiceAdmin {
		return nil, errForbidden("다른 사용자의 키입니다")
	}
	return key, nil
}

func (s *Server) rotateMyKey(w http.ResponseWriter, r *http.Request) {
	key, err := s.ownedKey(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	issued, err := s.Keys.Rotate(r.Context(), key.ID)
	if err != nil {
		httpx.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	s.Audit.Write(r.Context(), audit.Entry{Category: audit.CatKey, Action: "key.rotate",
		KeycloakUsername: identityOf(r).User.Username, Success: true,
		Detail: map[string]any{"from": key.ID, "to": issued.Key.ID}})
	httpx.JSON(w, http.StatusOK, issued)
}

func (s *Server) patchMyKey(w http.ResponseWriter, r *http.Request) {
	key, err := s.ownedKey(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var body struct {
		Role   string   `json:"role"`
		Scopes []string `json:"scopes"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Fail(w, http.StatusBadRequest, "요청 형식이 올바르지 않습니다")
		return
	}
	role := body.Role
	if role == "" {
		role = key.Role
	}
	if err := s.Keys.UpdateScopes(r.Context(), key.ID, role, body.Scopes); err != nil {
		httpx.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	s.Audit.Write(r.Context(), audit.Entry{Category: audit.CatKey, Action: "key.update",
		KeycloakUsername: identityOf(r).User.Username, Success: true,
		Detail: map[string]any{"keyId": key.ID, "role": role, "scopes": body.Scopes}})
	updated, _ := s.Keys.ByID(r.Context(), key.ID)
	httpx.JSON(w, http.StatusOK, updated)
}

func (s *Server) revokeMyKey(w http.ResponseWriter, r *http.Request) {
	key, err := s.ownedKey(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := s.Keys.Revoke(r.Context(), key.ID, "사용자 폐기"); err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Audit.Write(r.Context(), audit.Entry{Category: audit.CatKey, Action: "key.revoke",
		KeycloakUsername: identityOf(r).User.Username, Success: true, Detail: map[string]any{"keyId": key.ID}})
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) keyRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := s.Keys.Roles(r.Context())
	if err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, roles)
}

// ---------- my Confluence connection ----------

func (s *Server) myConfluence(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := identityOf(r)
	conf, _ := s.Store.Confluence(ctx)
	perm, _ := s.Store.Permission(ctx)
	cred, _ := s.Provider.UserCredentialInfo(ctx, id.User.ID)
	out := map[string]any{
		"mapping":             id.Mapping,
		"mappingError":        id.MappingErr,
		"credential":          cred,
		"permissionMode":      perm.Mode,
		"executionMode":       conf.ExecutionMode,
		"baseUrl":             conf.BaseURL,
		"serviceAccount":      conf.ServiceUsername,
		"allowUserCredential": conf.AllowUserCredential || conf.ExecutionMode == "delegated" || perm.Mode == "delegated",
		"trustSameDirectory":  conf.TrustSameDirectory,
		"detectedVersion":     conf.DetectedVersion,
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Server) saveMyCredential(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := identityOf(r)
	conf, _ := s.Store.Confluence(ctx)
	perm, _ := s.Store.Permission(ctx)
	if !conf.AllowUserCredential && conf.ExecutionMode != "delegated" && perm.Mode != "delegated" {
		httpx.Fail(w, http.StatusForbidden, "관리자가 사용자 자격증명 연결을 허용하지 않았습니다")
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := httpx.Decode(r, &body); err != nil || strings.TrimSpace(body.Password) == "" || strings.TrimSpace(body.Username) == "" {
		httpx.Fail(w, http.StatusBadRequest, "Confluence 사용자명과 비밀번호가 필요합니다")
		return
	}
	if err := s.Provider.SaveUserCredential(ctx, id.User.ID, strings.TrimSpace(body.Username), body.Password); err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Audit.Write(ctx, audit.Entry{Category: audit.CatKey, Action: "confluence_credential.save",
		KeycloakUsername: id.User.Username, ConfluenceUsername: body.Username, Success: true})
	s.verifyMyCredential(w, r)
}

// verifyMyCredential signs in to Confluence with the stored credential. A
// successful sign-in proves ownership of that account, so it can also create
// the identity mapping when none exists yet (ID-02).
func (s *Server) verifyMyCredential(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := identityOf(r)
	u, err := s.Provider.VerifyUserCredential(ctx, id.User.ID)
	if err != nil {
		s.Audit.Write(ctx, audit.Entry{Category: audit.CatKey, Action: "confluence_credential.verify",
			KeycloakUsername: id.User.Username, Success: false, Message: err.Error()})
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	m, err := s.Mapper.BindDelegated(ctx, id.User.KeycloakIssuer, id.KeycloakSub, id.User.Username, u)
	if err != nil {
		_ = s.Provider.DeleteUserCredential(ctx, id.User.ID)
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	s.Audit.Write(ctx, audit.Entry{Category: audit.CatKey, Action: "confluence_credential.verify",
		KeycloakUsername: id.User.Username, ConfluenceUserKey: u.UserKey, ConfluenceUsername: u.Username, Success: true})
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "user": u, "mapping": m})
}

func (s *Server) deleteMyCredential(w http.ResponseWriter, r *http.Request) {
	id := identityOf(r)
	if err := s.Provider.DeleteUserCredential(r.Context(), id.User.ID); err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Audit.Write(r.Context(), audit.Entry{Category: audit.CatKey, Action: "confluence_credential.revoke",
		KeycloakUsername: id.User.Username, Success: true})
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) verifyMyMapping(w http.ResponseWriter, r *http.Request) {
	id := identityOf(r)
	m, err := s.Mapper.Verify(r.Context(), id.KeycloakSub, id.User.Username)
	if err != nil {
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "mapping": m})
}

// ---------- approvals ----------

func (s *Server) myApprovals(w http.ResponseWriter, r *http.Request) {
	id := identityOf(r)
	list, err := s.Approvals.List(r.Context(), r.URL.Query().Get("status"), id.KeycloakSub, 200)
	if err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, list)
}

// approvalQueue lists other people's pending requests an approver may decide.
func (s *Server) approvalQueue(w http.ResponseWriter, r *http.Request) {
	id := identityOf(r)
	if !tools.CanApprove(id.Roles) {
		httpx.JSON(w, http.StatusOK, []approval.Request{})
		return
	}
	list, err := s.Approvals.List(r.Context(), approval.StatusPending, "", 200)
	if err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := []approval.Request{}
	for _, a := range list {
		if a.KeycloakSub != id.KeycloakSub && s.Policy.SpaceAllowed(r.Context(), a.SpaceKey) {
			out = append(out, a)
		}
	}
	httpx.JSON(w, http.StatusOK, out)
}

// canSeeTarget reports whether the identity may read the approval's target in
// Confluence. An approver reviews a document only if they could open it.
func (s *Server) canSeeTarget(ctx context.Context, r *http.Request, id *auth.Identity, req *approval.Request) bool {
	if id.Mapping == nil || !id.Mapping.Active {
		return false
	}
	p := s.principal(r, id)
	target := permission.SpaceTarget(req.SpaceKey)
	if req.TargetID != "" {
		target = permission.ContentTarget(req.TargetID)
	}
	if req.SpaceKey == "" && req.TargetID == "" {
		return false
	}
	d, err := s.Resolver.Check(ctx, p.Subject(), target, permission.OpRead)
	return err == nil && d.Allowed(permission.OpRead)
}

func (s *Server) approvalDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := identityOf(r)
	reqID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.Fail(w, http.StatusBadRequest, "요청 ID 형식이 올바르지 않습니다")
		return
	}
	req, err := s.Approvals.ByID(ctx, reqID)
	if err != nil {
		httpx.Fail(w, http.StatusNotFound, err.Error())
		return
	}
	own := req.KeycloakSub == id.KeycloakSub
	if !own && !(tools.CanApprove(id.Roles) && s.canSeeTarget(ctx, r, id, req)) {
		httpx.Fail(w, http.StatusNotFound, "승인 요청을 찾을 수 없거나 볼 권한이 없습니다")
		return
	}
	sec, _ := s.Store.Security(ctx)
	canSelf := own && !req.RequiresApprover && sec.AllowSelfApproval
	httpx.JSON(w, http.StatusOK, map[string]any{
		"request":    req,
		"own":        own,
		"canApprove": req.Status == approval.StatusPending && (canSelf || (!own && tools.CanApprove(id.Roles))),
		"canReject":  req.Status == approval.StatusPending,
	})
}

func (s *Server) decideApprovalHTTP(w http.ResponseWriter, r *http.Request) {
	reqID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.Fail(w, http.StatusBadRequest, "요청 ID 형식이 올바르지 않습니다")
		return
	}
	var body struct {
		Approve bool   `json:"approve"`
		Note    string `json:"note"`
	}
	_ = httpx.Decode(r, &body)
	out, err := s.decide(r, identityOf(r), reqID, body.Approve, body.Note)
	if err != nil {
		writeErr(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// decide applies the approval rules shared by the personal and admin pages:
// a requester may approve their own WRITE change when self-approval is on;
// anything else needs an approver who can see the target in Confluence and
// whose space is open to MCP. The admin role alone does not bypass this.
func (s *Server) decide(r *http.Request, id *auth.Identity, reqID uuid.UUID, approve bool, note string) (*approval.Request, error) {
	ctx := r.Context()
	req, err := s.Approvals.ByID(ctx, reqID)
	if err != nil {
		return nil, errNotFound(err.Error())
	}
	own := req.KeycloakSub == id.KeycloakSub
	if approve {
		sec, _ := s.Store.Security(ctx)
		switch {
		case own && req.RequiresApprover:
			return nil, errForbidden("이 변경은 요청자가 아닌 별도 승인자의 승인이 필요합니다")
		case own && !sec.AllowSelfApproval:
			return nil, errForbidden("본인 승인이 허용되지 않습니다. 승인자에게 요청하십시오")
		case !own && !tools.CanApprove(id.Roles):
			return nil, errForbidden("승인자 역할이 필요합니다")
		case !own && !s.Policy.SpaceAllowed(ctx, req.SpaceKey):
			return nil, errForbidden("MCP 접근이 허용되지 않은 공간의 변경입니다")
		case !own && !s.canSeeTarget(ctx, r, id, req):
			return nil, errForbidden("승인자가 Confluence 에서 대상 문서를 볼 수 없어 승인할 수 없습니다")
		}
	} else if !own && !tools.CanApprove(id.Roles) {
		return nil, errForbidden("승인자 역할이 필요합니다")
	}
	out, err := s.Approvals.Decide(ctx, reqID, approve, id.User.Username, note)
	if err != nil {
		return nil, errBadRequest(err.Error())
	}
	s.Audit.Write(ctx, audit.Entry{
		Category: audit.CatApproval, Action: "approval.decide", KeycloakSub: id.KeycloakSub,
		KeycloakUsername: id.User.Username, ToolName: req.ToolName, SpaceKey: req.SpaceKey, ContentID: req.TargetID,
		ApprovalID: &reqID, Success: true, Decision: map[bool]string{true: "approve", false: "reject"}[approve],
		Detail: map[string]any{"self": own, "requester": req.Username},
	})
	return out, nil
}

func (s *Server) myOperations(w http.ResponseWriter, r *http.Request) {
	list, err := s.Operations.List(r.Context(), r.URL.Query().Get("status"), identityOf(r).KeycloakSub, 100)
	if err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, list)
}

// ---------- uploads ----------

func (s *Server) myUploads(w http.ResponseWriter, r *http.Request) {
	list, err := s.Uploads.List(r.Context(), identityOf(r).User.ID)
	if err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, list)
}

func (s *Server) uploadFile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := identityOf(r)
	if !apikey.HasScope(id.Scopes, tools.ScopeAttachmentWrite) {
		httpx.FailCode(w, http.StatusForbidden, "SCOPE_DENIED", tools.ScopeAttachmentWrite+" 스코프가 필요합니다")
		return
	}
	limits, _ := s.Store.Limits(ctx)
	max := int64(limits.UploadMaxMB) << 20
	r.Body = http.MaxBytesReader(w, r.Body, max+(1<<20))
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		httpx.FailCode(w, http.StatusRequestEntityTooLarge, "LIMIT_EXCEEDED", "파일이 너무 크거나 형식이 올바르지 않습니다")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		httpx.Fail(w, http.StatusBadRequest, "file 필드가 필요합니다")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, max+1))
	if err != nil {
		httpx.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	up, err := s.Uploads.Save(ctx, id.User.ID, header.Filename, header.Header.Get("Content-Type"), data, max,
		time.Duration(limits.UploadTTLMin)*time.Minute)
	if err != nil {
		code := http.StatusBadRequest
		if strings.HasPrefix(err.Error(), "LIMIT_EXCEEDED") {
			code = http.StatusRequestEntityTooLarge
		}
		httpx.Fail(w, code, strings.TrimPrefix(err.Error(), "LIMIT_EXCEEDED: "))
		return
	}
	s.Audit.Write(ctx, audit.Entry{Category: audit.CatFile, Action: "upload.create", KeycloakUsername: id.User.Username,
		Success: true, Detail: map[string]any{"uploadId": up.ID, "filename": up.Filename, "size": up.Size, "sha256": up.SHA256}})
	httpx.JSON(w, http.StatusCreated, up)
}

func (s *Server) deleteUpload(w http.ResponseWriter, r *http.Request) {
	upID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.Fail(w, http.StatusBadRequest, "업로드 ID 형식이 올바르지 않습니다")
		return
	}
	if err := s.Uploads.Delete(r.Context(), identityOf(r).User.ID, upID); err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// downloadAttachment streams an attachment for a signed, short-lived link.
// The link names the requester; their permission is decided again here.
func (s *Server) downloadAttachment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tok, err := s.Signer.OpenDownload(r.URL.Query().Get("t"))
	if err != nil || tok.AttachmentID != chi.URLParam(r, "id") {
		httpx.Fail(w, http.StatusForbidden, "유효하지 않거나 만료된 다운로드 링크입니다")
		return
	}
	m, err := s.Mapper.BySub(ctx, tok.Sub)
	if err != nil || !m.Active {
		httpx.Fail(w, http.StatusForbidden, "Confluence 사용자 매핑을 확인할 수 없습니다")
		return
	}
	var userID int64
	_ = s.Pool.QueryRow(ctx, `SELECT id FROM users WHERE keycloak_sub=$1 OR 'local:'||lower(username)=$1 LIMIT 1`, tok.Sub).Scan(&userID)
	subj := permission.Subject{UserID: userID, UserKey: m.ConfluenceUserKey, Username: m.ConfluenceUsername}
	d, err := s.Resolver.Check(ctx, subj, permission.ContentTarget(tok.AttachmentID), permission.OpRead)
	if err != nil || !d.Allowed(permission.OpRead) {
		httpx.Fail(w, http.StatusForbidden, "첨부를 내려받을 권한이 없습니다")
		return
	}
	adapter, conf, err := s.Provider.Adapter(ctx)
	if err != nil {
		httpx.Fail(w, http.StatusBadGateway, err.Error())
		return
	}
	perm, _ := s.Store.Permission(ctx)
	var cred confluence.Credential
	if conf.ExecutionMode == "delegated" || perm.Mode == "delegated" {
		cred, _, err = s.Provider.UserCredential(ctx, userID)
	} else {
		cred, err = s.Provider.ServiceCredential(ctx)
	}
	if err != nil {
		httpx.Fail(w, http.StatusBadGateway, err.Error())
		return
	}
	att, err := adapter.Content(ctx, cred, tok.AttachmentID, "version", "container", "space")
	if err != nil || att.Type != "attachment" {
		httpx.Fail(w, http.StatusNotFound, "첨부를 찾을 수 없습니다")
		return
	}
	limits, _ := s.Store.Limits(ctx)
	body, ctype, err := adapter.Download(ctx, cred, att, int64(limits.UploadMaxMB)<<22)
	if err != nil {
		httpx.Fail(w, http.StatusBadGateway, err.Error())
		return
	}
	defer body.Close()
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Disposition", `attachment; filename*=UTF-8''`+urlPathEscape(att.Title))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	httpx.NoCache(w)
	n, _ := io.Copy(w, body)
	s.Audit.Write(ctx, audit.Entry{Category: audit.CatFile, Action: "attachment.download", KeycloakSub: tok.Sub,
		KeycloakUsername: m.KeycloakUsername, ConfluenceUserKey: m.ConfluenceUserKey, ConfluenceUsername: m.ConfluenceUsername,
		ExecutedAs: cred.Actor(), ContentID: att.ID, SpaceKey: att.SpaceKey, Success: true, Detail: map[string]any{"bytes": n}})
}

func urlPathEscape(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || strings.IndexByte("-._~", c) >= 0 {
			b.WriteByte(c)
		} else {
			b.WriteString("%" + strings.ToUpper(string("0123456789abcdef"[c>>4])+string("0123456789abcdef"[c&15])))
		}
	}
	return b.String()
}

// ---------- OAuth connections ----------

func (s *Server) myConnections(w http.ResponseWriter, r *http.Request) {
	list, err := s.OAuth.Grants(r.Context(), identityOf(r).User.ID, true)
	if err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, list)
}

func (s *Server) revokeMyConnection(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := identityOf(r)
	gid, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.Fail(w, http.StatusBadRequest, "ID 형식이 올바르지 않습니다")
		return
	}
	owner, err := s.OAuth.GrantOwner(ctx, gid)
	if err != nil || (owner != id.User.ID && !id.User.IsServiceAdmin) {
		httpx.Fail(w, http.StatusNotFound, "연결을 찾을 수 없습니다")
		return
	}
	if err := s.OAuth.RevokeGrant(ctx, gid); err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Audit.Write(ctx, audit.Entry{Category: audit.CatAuth, Action: "oauth.revoke", KeycloakUsername: id.User.Username,
		Success: true, Detail: map[string]any{"grantId": gid}})
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// mcpConfig gives copy-paste client configuration for this gateway.
func (s *Server) mcpConfig(w http.ResponseWriter, r *http.Request) {
	url := s.mcpResource(r)
	httpx.JSON(w, http.StatusOK, map[string]any{
		"mcpUrl": url,
		"oauth": map[string]any{
			"claudeCode": "claude mcp add --transport http confmcp " + url,
			"json":       map[string]any{"mcpServers": map[string]any{"confmcp": map[string]any{"type": "http", "url": url}}},
		},
		"apiKey": map[string]any{
			"claudeCode": `claude mcp add --transport http confmcp ` + url + ` --header "Authorization: Bearer <API 키>"`,
			"json": map[string]any{"mcpServers": map[string]any{"confmcp": map[string]any{"type": "http", "url": url,
				"headers": map[string]string{"Authorization": "Bearer <API 키>"}}}},
		},
	})
}

// ---------- audit, tools, permissions ----------

func (s *Server) myAudit(w http.ResponseWriter, r *http.Request) {
	id := identityOf(r)
	entries, total, err := s.Audit.List(r.Context(), audit.Query{
		Username: id.User.Username, Exact: true,
		Category: r.URL.Query().Get("category"), Tool: r.URL.Query().Get("tool"),
		Success: queryBoolPtr(r, "success"),
		Limit:   queryInt(r, "limit", 50), Offset: queryInt(r, "offset", 0),
	})
	if err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"values": entries, "total": total})
}

func (s *Server) myTools(w http.ResponseWriter, r *http.Request) {
	recs, err := s.Executor.Available(r.Context(), s.principal(r, identityOf(r)))
	if err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, recs)
}

func (s *Server) myPermissions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := identityOf(r)
	if id.Mapping == nil {
		httpx.FailCode(w, http.StatusConflict, "IDENTITY_UNMAPPED", "Confluence 사용자 매핑이 없습니다: "+id.MappingErr)
		return
	}
	space := strings.TrimSpace(r.URL.Query().Get("spaceKey"))
	contentID := strings.TrimSpace(r.URL.Query().Get("contentId"))
	adapter, _, _ := s.Provider.Adapter(ctx)
	var cred confluence.Credential
	conf, _ := s.Store.Confluence(ctx)
	perm, _ := s.Store.Permission(ctx)
	if conf.ExecutionMode == "delegated" || perm.Mode == "delegated" {
		cred, _, _ = s.Provider.UserCredential(ctx, id.User.ID)
	} else {
		cred, _ = s.Provider.ServiceCredential(ctx)
	}
	out, err := tools.CheckAll(ctx, s.Executor, s.principal(r, id), adapter, cred, space, contentID)
	if err != nil {
		var te *tools.Error
		if errors.As(err, &te) {
			httpx.FailCode(w, http.StatusBadGateway, te.Code, te.Message)
			return
		}
		httpx.Fail(w, http.StatusBadGateway, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// ---------- AI ----------

// aiChat relays a streaming AI completion as server-sent events.
func (s *Server) aiChat(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := identityOf(r)
	if !apikey.HasScope(id.Scopes, apikey.ScopeAIInvoke) {
		httpx.FailCode(w, http.StatusForbidden, "SCOPE_DENIED", "ai:invoke 스코프가 필요합니다")
		return
	}
	var body aiRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Fail(w, http.StatusBadRequest, "요청 형식이 올바르지 않습니다")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpx.Fail(w, http.StatusInternalServerError, "스트리밍을 지원하지 않습니다")
		return
	}
	httpx.NoCache(w)
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	send := func(event string, payload any) {
		writeSSE(w, event, payload)
		flusher.Flush()
	}
	started := time.Now()
	err := s.AI.Stream(ctx, body.toProxyRequest(), func(e aiEvent) error {
		send("message", e)
		return nil
	})
	if err != nil {
		send("message", aiEvent{Type: "error", Error: err.Error()})
	}
	send("message", aiEvent{Type: "end"})
	s.Audit.Write(ctx, audit.Entry{Category: audit.CatAI, Action: "ai.chat", KeycloakUsername: id.User.Username,
		Success: err == nil, Message: errString(err), LatencyMS: int(time.Since(started).Milliseconds())})
}

func (s *Server) aiModels(w http.ResponseWriter, r *http.Request) {
	cfg, _, err := s.AI.Config(r.Context())
	if err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	models := cfg.Models
	if len(models) == 0 && cfg.Model != "" {
		models = []string{cfg.Model}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"enabled":       cfg.Enabled,
		"provider":      cfg.Provider,
		"model":         cfg.Model,
		"models":        models,
		"maxTokens":     cfg.MaxTokens,
		"contextLimit":  cfg.ContextLimit,
		"streaming":     cfg.Streaming,
		"maxTokenLimit": maxTokenCeiling(),
	})
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
