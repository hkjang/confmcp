package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/hkjang/confmcp/internal/audit"
	"github.com/hkjang/confmcp/internal/httpx"
	"github.com/hkjang/confmcp/internal/identity"
	"github.com/hkjang/confmcp/internal/permission"
	"github.com/hkjang/confmcp/internal/policy"
	"github.com/hkjang/confmcp/internal/tools"
)

func auditQuery(limit int) audit.Query { return audit.Query{Limit: limit} }

func (s *Server) adminAudit(r *http.Request, action string, detail map[string]any) {
	s.Audit.Write(r.Context(), audit.Entry{Category: audit.CatAdmin, Action: action,
		KeycloakUsername: identityOf(r).User.Username, Success: true, Detail: detail})
}

// ---------- identity mapping ----------

func (s *Server) listMappings(w http.ResponseWriter, r *http.Request) {
	list, err := s.Mapper.List(r.Context(), r.URL.Query().Get("q"), r.URL.Query().Get("state"), queryInt(r, "limit", 200))
	if err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, list)
}

func (s *Server) findMappingByUsername(ctx context.Context, username string) (*identity.Mapping, error) {
	list, err := s.Mapper.List(ctx, username, "", 50)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if strings.EqualFold(list[i].KeycloakUsername, username) || strings.EqualFold(list[i].ConfluenceUsername, username) {
			return &list[i], nil
		}
	}
	return nil, errors.New("이 사용자의 Confluence 매핑이 없습니다: " + username)
}

// createMapping binds a console user to a Confluence account after the
// administrator confirmed the ownership.
func (s *Server) createMapping(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body struct {
		Username           string `json:"username"`
		ConfluenceUsername string `json:"confluenceUsername"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Fail(w, http.StatusBadRequest, "요청 형식이 올바르지 않습니다")
		return
	}
	body.Username, body.ConfluenceUsername = strings.TrimSpace(body.Username), strings.TrimSpace(body.ConfluenceUsername)
	if body.Username == "" || body.ConfluenceUsername == "" {
		httpx.Fail(w, http.StatusBadRequest, "콘솔 사용자명과 Confluence 사용자명이 필요합니다")
		return
	}
	u, err := s.Users.ByUsername(ctx, body.Username)
	if err != nil {
		httpx.Fail(w, http.StatusBadRequest, "콘솔 사용자를 찾을 수 없습니다. 사용자가 한 번 로그인했거나 로컬 계정이어야 합니다")
		return
	}
	sub := u.KeycloakSub
	if sub == "" {
		sub = "local:" + strings.ToLower(u.Username)
	}
	m, err := s.Mapper.ManualMap(ctx, u.KeycloakIssuer, sub, u.Username, body.ConfluenceUsername, identityOf(r).User.Username)
	if err != nil {
		httpx.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	s.adminAudit(r, "mapping.create", map[string]any{"user": u.Username, "confluenceUser": m.ConfluenceUsername, "userKey": m.ConfluenceUserKey})
	httpx.JSON(w, http.StatusOK, m)
}

func (s *Server) verifyMapping(w http.ResponseWriter, r *http.Request) {
	m, err := s.Mapper.Verify(r.Context(), chi.URLParam(r, "sub"), identityOf(r).User.Username)
	if err != nil {
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "mapping": m})
}

func (s *Server) setMappingActive(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Active bool `json:"active"`
	}
	_ = httpx.Decode(r, &body)
	sub := chi.URLParam(r, "sub")
	if err := s.Mapper.SetActive(r.Context(), sub, body.Active, identityOf(r).User.Username); err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.adminAudit(r, "mapping.active", map[string]any{"sub": sub, "active": body.Active})
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) deleteMapping(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "sub")
	if err := s.Mapper.Delete(r.Context(), sub, identityOf(r).User.Username); err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.adminAudit(r, "mapping.delete", map[string]any{"sub": sub})
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) mappingHistory(w http.ResponseWriter, r *http.Request) {
	list, err := s.Mapper.History(r.Context(), r.URL.Query().Get("sub"), queryInt(r, "limit", 100))
	if err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, list)
}

func (s *Server) listMappingErrors(w http.ResponseWriter, r *http.Request) {
	list, err := s.Mapper.Errors(r.Context(), queryInt(r, "limit", 100))
	if err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, list)
}

func (s *Server) clearMappingErrors(w http.ResponseWriter, r *http.Request) {
	if err := s.Mapper.ClearErrors(r.Context()); err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------- tools ----------

func (s *Server) listTools(w http.ResponseWriter, r *http.Request) {
	recs, err := s.Registry.List(r.Context())
	if err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, recs)
}

func (s *Server) patchTool(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	_, rec, err := s.Registry.Get(r.Context(), name)
	if err != nil {
		httpx.Fail(w, http.StatusNotFound, err.Error())
		return
	}
	body := struct {
		Enabled          *bool   `json:"enabled"`
		RequiresApproval *bool   `json:"requiresApproval"`
		MinRole          *string `json:"minRole"`
	}{}
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Fail(w, http.StatusBadRequest, "요청 형식이 올바르지 않습니다")
		return
	}
	enabled, approval, minRole := rec.Enabled, rec.RequiresApproval, rec.MinRole
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	if body.RequiresApproval != nil {
		approval = *body.RequiresApproval
	}
	if body.MinRole != nil {
		minRole = *body.MinRole
	}
	// EXECUTE-risk tools (move, trash) may never run unapproved.
	if rec.Risk == tools.RiskExecute {
		approval = true
	}
	if err := s.Registry.Update(r.Context(), name, enabled, approval, minRole); err != nil {
		httpx.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	s.Audit.Write(r.Context(), audit.Entry{Category: audit.CatAdmin, Action: "tool.update",
		KeycloakUsername: identityOf(r).User.Username, ToolName: name, Success: true,
		Detail: map[string]any{"enabled": enabled, "requiresApproval": approval, "minRole": minRole}})
	_, updated, _ := s.Registry.Get(r.Context(), name)
	httpx.JSON(w, http.StatusOK, updated)
}

func (s *Server) syncTools(w http.ResponseWriter, r *http.Request) {
	if err := s.Registry.Sync(r.Context()); err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	recs, _ := s.Registry.List(r.Context())
	httpx.JSON(w, http.StatusOK, recs)
}

// ---------- policy ----------

func (s *Server) listRules(w http.ResponseWriter, r *http.Request) {
	rules, err := s.Policy.Rules(r.Context())
	if err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	gen, _ := s.Policy.Generation(r.Context())
	httpx.JSON(w, http.StatusOK, map[string]any{"rules": rules, "generation": gen})
}

func validRule(r policy.Rule) error {
	switch r.Kind {
	case policy.KindSpace, policy.KindPageTree, policy.KindContentType:
	default:
		return errBadRequest("유형은 공간(space), 페이지 트리(page_tree), 콘텐츠 유형(content_type) 중 하나여야 합니다")
	}
	if r.Effect != policy.EffectAllow && r.Effect != policy.EffectDeny {
		return errBadRequest("효과는 허용(allow) 또는 차단(deny) 이어야 합니다")
	}
	p := strings.TrimSpace(r.Pattern)
	if p == "" {
		return errBadRequest("대상(pattern)이 필요합니다")
	}
	if r.Kind == policy.KindPageTree {
		if _, err := strconv.ParseInt(p, 10, 64); err != nil {
			return errBadRequest("페이지 트리 대상은 페이지 ID(숫자)여야 합니다")
		}
	}
	if r.Kind == policy.KindContentType {
		switch p {
		case "page", "blogpost", "comment", "attachment", "*":
		default:
			return errBadRequest("콘텐츠 유형은 page, blogpost, comment, attachment 중 하나입니다")
		}
	}
	switch strings.ToUpper(r.RiskCap) {
	case "", "READ", "WRITE", "EXECUTE":
	default:
		return errBadRequest("위험도 상한 값이 올바르지 않습니다")
	}
	return nil
}

func (s *Server) createRule(w http.ResponseWriter, r *http.Request) {
	var rule policy.Rule
	if err := httpx.Decode(r, &rule); err != nil {
		httpx.Fail(w, http.StatusBadRequest, "요청 형식이 올바르지 않습니다")
		return
	}
	rule.RiskCap = strings.ToUpper(rule.RiskCap)
	if err := validRule(rule); err != nil {
		writeErr(w, err)
		return
	}
	id, err := s.Policy.Create(r.Context(), rule)
	if err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	rule.ID = id
	s.Resolver.Reset()
	s.adminAudit(r, "policy.create", map[string]any{"kind": rule.Kind, "pattern": rule.Pattern, "effect": rule.Effect, "spaceKey": rule.SpaceKey})
	httpx.JSON(w, http.StatusCreated, rule)
}

func (s *Server) updateRule(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Fail(w, http.StatusBadRequest, "ID 형식이 올바르지 않습니다")
		return
	}
	var rule policy.Rule
	if err := httpx.Decode(r, &rule); err != nil {
		httpx.Fail(w, http.StatusBadRequest, "요청 형식이 올바르지 않습니다")
		return
	}
	rule.ID = id
	rule.RiskCap = strings.ToUpper(rule.RiskCap)
	if err := validRule(rule); err != nil {
		writeErr(w, err)
		return
	}
	if err := s.Policy.Update(r.Context(), rule); err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Resolver.Reset()
	s.adminAudit(r, "policy.update", map[string]any{"id": id, "kind": rule.Kind, "pattern": rule.Pattern, "effect": rule.Effect})
	httpx.JSON(w, http.StatusOK, rule)
}

func (s *Server) deleteRule(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Fail(w, http.StatusBadRequest, "ID 형식이 올바르지 않습니다")
		return
	}
	if err := s.Policy.Delete(r.Context(), id); err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Resolver.Reset()
	s.adminAudit(r, "policy.delete", map[string]any{"id": id})
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// evaluatePolicy dry-runs the ACL, and with a username the requester's own
// Confluence permission, against a space or a piece of content.
func (s *Server) evaluatePolicy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body struct {
		SpaceKey    string   `json:"spaceKey"`
		ContentID   string   `json:"contentId"`
		Ancestors   []string `json:"ancestors"`
		ContentType string   `json:"contentType"`
		Username    string   `json:"username"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Fail(w, http.StatusBadRequest, "요청 형식이 올바르지 않습니다")
		return
	}
	target := policy.Target{SpaceKey: body.SpaceKey, ContentID: body.ContentID, Ancestors: body.Ancestors, ContentType: body.ContentType}
	// With a content ID, read the real space and ancestors from Confluence.
	if body.ContentID != "" {
		if adapter, _, err := s.Provider.Adapter(ctx); err == nil {
			if cred, err := s.Provider.ServiceCredential(ctx); err == nil {
				if ct, err := adapter.Content(ctx, cred, body.ContentID, "space", "ancestors"); err == nil {
					target = policy.Target{SpaceKey: ct.SpaceKey, ContentID: ct.ID, Ancestors: ct.AncestorIDs(), ContentType: ct.Type}
				}
			}
		}
	}
	verdict, err := s.Policy.Evaluate(ctx, target)
	if err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := map[string]any{"policy": verdict, "target": target}
	if body.Username != "" {
		m, err := s.findMappingByUsername(ctx, body.Username)
		if err != nil {
			out["permissionError"] = err.Error()
		} else {
			pt := permission.SpaceTarget(target.SpaceKey)
			if body.ContentID != "" {
				pt = permission.ContentTarget(body.ContentID)
			}
			var userID int64
			_ = s.Pool.QueryRow(ctx, `SELECT id FROM users WHERE username=$1`, m.KeycloakUsername).Scan(&userID)
			d, err := s.Resolver.Check(ctx, permission.Subject{UserID: userID, UserKey: m.ConfluenceUserKey, Username: m.ConfluenceUsername},
				pt, permission.AllOps...)
			if err != nil {
				out["permissionError"] = err.Error()
			} else {
				out["permission"] = d
			}
		}
	}
	httpx.JSON(w, http.StatusOK, out)
}

// ---------- approvals & operations ----------

func (s *Server) listApprovals(w http.ResponseWriter, r *http.Request) {
	list, err := s.Approvals.List(r.Context(), r.URL.Query().Get("status"), "", queryInt(r, "limit", 200))
	if err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, list)
}

func (s *Server) listOperations(w http.ResponseWriter, r *http.Request) {
	list, err := s.Operations.List(r.Context(), r.URL.Query().Get("status"), "", queryInt(r, "limit", 200))
	if err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, list)
}

// resolveOperation settles an outcome_unknown write after the administrator
// checked Confluence by hand.
func (s *Server) resolveOperation(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.Fail(w, http.StatusBadRequest, "ID 형식이 올바르지 않습니다")
		return
	}
	var body struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Fail(w, http.StatusBadRequest, "요청 형식이 올바르지 않습니다")
		return
	}
	if err := s.Operations.Resolve(r.Context(), id, body.Status, identityOf(r).User.Username, body.Note); err != nil {
		httpx.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	s.adminAudit(r, "operation.resolve", map[string]any{"operationId": id, "status": body.Status, "note": body.Note})
	rec, _ := s.Operations.ByID(r.Context(), id)
	httpx.JSON(w, http.StatusOK, rec)
}

// ---------- MCP OAuth clients ----------

func (s *Server) listOAuthClients(w http.ResponseWriter, r *http.Request) {
	list, err := s.OAuth.Clients(r.Context())
	if err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, list)
}

func (s *Server) disableOAuthClient(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.OAuth.DisableClient(r.Context(), id); err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.adminAudit(r, "oauth.client.disable", map[string]any{"clientId": id})
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) listOAuthGrants(w http.ResponseWriter, r *http.Request) {
	list, err := s.OAuth.Grants(r.Context(), int64(queryInt(r, "userId", 0)), r.URL.Query().Get("all") != "1")
	if err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, list)
}
