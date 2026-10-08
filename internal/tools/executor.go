package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/hkjang/confmcp/internal/approval"
	"github.com/hkjang/confmcp/internal/attachment"
	"github.com/hkjang/confmcp/internal/audit"
	"github.com/hkjang/confmcp/internal/confluence"
	"github.com/hkjang/confmcp/internal/content"
	"github.com/hkjang/confmcp/internal/operation"
	"github.com/hkjang/confmcp/internal/permission"
	"github.com/hkjang/confmcp/internal/policy"
	"github.com/hkjang/confmcp/internal/settings"
)

// Error codes returned to MCP clients.
const (
	CodeAuthRequired       = "AUTH_REQUIRED"
	CodeUnknownTool        = "UNKNOWN_TOOL"
	CodeToolDisabled       = "TOOL_DISABLED"
	CodeRoleDenied         = "ROLE_DENIED"
	CodeScopeDenied        = "SCOPE_DENIED"
	CodeIdentityMissing    = "IDENTITY_UNMAPPED"
	CodePolicyDenied       = "POLICY_DENIED"
	CodePermissionDenied   = "PERMISSION_DENIED"
	CodePermissionUnknown  = "PERMISSION_UNKNOWN"
	CodeApprovalRequired   = "APPROVAL_REQUIRED"
	CodeApprovalStale      = "APPROVAL_STALE"
	CodeApprovalDenied     = "APPROVAL_DENIED"
	CodeVersionConflict    = "VERSION_CONFLICT"
	CodeOutcomeUnknown     = "OUTCOME_UNKNOWN"
	CodeUpstreamAuth       = "UPSTREAM_AUTH_FAILED"
	CodeUpstreamUnavailable = "UPSTREAM_UNAVAILABLE"
	CodeUnsupportedContent = "UNSUPPORTED_CONTENT"
	CodeLimitExceeded      = "LIMIT_EXCEEDED"
	CodeBadArguments       = "BAD_ARGUMENTS"
	CodeInternal           = "INTERNAL_ERROR"
)

// hiddenMessage is the single answer for "does not exist" and "may not see":
// telling them apart would reveal that restricted content exists.
const hiddenMessage = "대상을 찾을 수 없거나 접근 권한이 없습니다"

// Error is a structured tool failure.
type Error struct {
	Code     string            `json:"code"`
	Message  string            `json:"message"`
	Approval *approval.Request `json:"approval,omitempty"`
	Details  map[string]any    `json:"details,omitempty"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func toolErr(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Deps are the collaborators the executor needs.
type Deps struct {
	Registry   *Registry
	Provider   *confluence.Provider
	Resolver   *permission.Resolver
	Policy     *policy.Engine
	Approvals  *approval.Engine
	Operations *operation.Store
	Uploads    *attachment.Store
	Audit      *audit.Logger
	Store      *settings.Store
	Signer     *Signer
}

// Executor authorises and runs tool calls.
type Executor struct{ Deps }

// NewExecutor builds the executor.
func NewExecutor(d Deps) *Executor { return &Executor{Deps: d} }

// Available returns the tools a principal may see. tools/call checks again.
func (e *Executor) Available(ctx context.Context, p Principal) ([]Record, error) {
	all, err := e.Registry.List(ctx)
	if err != nil {
		return nil, err
	}
	mcpCfg, err := e.Store.MCP(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Record, 0, len(all))
	for _, rec := range all {
		if !rec.Enabled {
			continue
		}
		if rec.HighLevel && !mcpCfg.ExposeHighLevel {
			continue
		}
		if !RoleSatisfied(p.Roles, rec.MinRole) {
			continue
		}
		if rec.Scope != "" && !hasScope(p.Scopes, rec.Scope) {
			continue
		}
		out = append(out, rec)
	}
	return out, nil
}

// trace collects what the audit record needs as the pipeline runs.
type trace struct {
	started    time.Time
	spaceKey   string
	contentID  string
	version    *int
	decision   string
	executedAs string
	approval   *uuid.UUID
	detail     map[string]any
}

// Invoke runs the full authorisation pipeline and then the tool.
func (e *Executor) Invoke(ctx context.Context, p Principal, name string, args Args) (any, error) {
	return e.invoke(ctx, p, name, args, false)
}

// Propose prepares a write without sending it and opens an approval request
// bound to the prepared change. It backs confluence_prepare_change.
func (e *Executor) Propose(ctx context.Context, p Principal, name string, args Args) (any, error) {
	return e.invoke(ctx, p, name, args, true)
}

func (e *Executor) invoke(ctx context.Context, p Principal, name string, args Args, propose bool) (any, error) {
	tr := &trace{started: time.Now(), detail: map[string]any{}}
	if args == nil {
		args = Args{}
	}
	if p.RequestID == "" {
		p.RequestID = uuid.NewString()
	}

	def, rec, err := e.Registry.Get(ctx, name)
	if err != nil {
		if errors.Is(err, ErrUnknownTool) {
			return nil, e.fail(ctx, p, name, rec, tr, toolErr(CodeUnknownTool, "%s", err.Error()))
		}
		return nil, e.fail(ctx, p, name, rec, tr, toolErr(CodeInternal, "%s", err.Error()))
	}
	if propose && !def.IsWrite() {
		return nil, e.fail(ctx, p, name, rec, tr, toolErr(CodeBadArguments, "%s 는 변경 도구가 아닙니다", name))
	}
	if !rec.Enabled {
		return nil, e.fail(ctx, p, name, rec, tr, toolErr(CodeToolDisabled, "관리자가 비활성화한 도구입니다: %s", name))
	}
	if !RoleSatisfied(p.Roles, rec.MinRole) {
		return nil, e.fail(ctx, p, name, rec, tr, toolErr(CodeRoleDenied, "%s 역할이 필요합니다", rec.MinRole))
	}
	if rec.Scope != "" && !hasScope(p.Scopes, rec.Scope) {
		return nil, e.fail(ctx, p, name, rec, tr, toolErr(CodeScopeDenied, "%s 스코프가 필요합니다", rec.Scope))
	}
	needOp, needsPerm := permission.ParseOp(rec.RequiredPerm)
	if strings.TrimSpace(p.ConfluenceUserKey) == "" && (needsPerm || rec.RequiredPerm != "NONE") {
		return nil, e.fail(ctx, p, name, rec, tr,
			toolErr(CodeIdentityMissing, "Confluence 사용자 매핑이 없어 권한을 확인할 수 없습니다. 콘솔의 '내 Confluence 연결'을 확인하십시오"))
	}

	cfg, err := e.Store.Confluence(ctx)
	if err != nil {
		return nil, e.fail(ctx, p, name, rec, tr, toolErr(CodeInternal, "%v", err))
	}
	limits, _ := e.Store.Limits(ctx)
	mcpCfg, _ := e.Store.MCP(ctx)
	if cfg.ToolTimeoutSec > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(cfg.ToolTimeoutSec)*time.Second)
		defer cancel()
	}

	call := &Call{Args: args, Principal: p, Cfg: cfg, Limits: limits, MCP: mcpCfg, exec: e}
	if def.Name != "confluence_prepare_change" {
		adapter, cred, cerr := e.connection(ctx, p)
		if cerr != nil && rec.RequiredPerm != "NONE" {
			return nil, e.fail(ctx, p, name, rec, tr, cerr)
		}
		call.Adapter, call.Cred = adapter, cred
		tr.executedAs = cred.Actor()
	}
	if def.Resolve != nil {
		call.Target = def.Resolve(args)
	}
	tr.spaceKey, tr.contentID = call.Target.SpaceKey, call.Target.ContentID

	if needsPerm && (call.Target.ContentID != "" || call.Target.SpaceKey != "") {
		res, terr := e.resolveTarget(ctx, call, needOp, rec)
		if terr != nil {
			tr.decision = "deny"
			return nil, e.fail(ctx, p, name, rec, tr, terr)
		}
		call.Resolved = res
		tr.spaceKey, tr.contentID, tr.decision = res.SpaceKey, res.ContentID, "allow"
		if res.Content != nil {
			v := res.Content.Version.Number
			tr.version = &v
		}
	}

	if def.IsWrite() {
		out, werr := e.write(ctx, call, def, rec, tr, propose)
		if werr != nil {
			return nil, e.fail(ctx, p, name, rec, tr, werr)
		}
		e.record(ctx, p, name, rec, tr, true, "", "")
		return out, nil
	}

	out, err := def.Handle(ctx, call)
	if err != nil {
		return nil, e.fail(ctx, p, name, rec, tr, classify(err))
	}
	e.record(ctx, p, name, rec, tr, true, "", "")
	return out, nil
}

// connection picks the adapter and the credential a call runs under.
func (e *Executor) connection(ctx context.Context, p Principal) (confluence.Adapter, confluence.Credential, *Error) {
	adapter, cfg, err := e.Provider.Adapter(ctx)
	if err != nil {
		return nil, confluence.Credential{}, toolErr(CodeUpstreamUnavailable, "%v", err)
	}
	perm, _ := e.Store.Permission(ctx)
	if cfg.ExecutionMode == "delegated" || perm.Mode == "delegated" {
		cred, key, err := e.Provider.UserCredential(ctx, p.UserID)
		if err != nil {
			return adapter, confluence.Credential{}, toolErr(CodeUpstreamAuth,
				"사용자 위임 모드입니다. 콘솔의 '내 Confluence 연결'에서 계정을 연결하고 검증하십시오 (%v)", err)
		}
		if !strings.EqualFold(key, p.ConfluenceUserKey) {
			return adapter, confluence.Credential{}, toolErr(CodeIdentityMissing,
				"연결된 Confluence 자격증명이 매핑된 사용자와 다릅니다")
		}
		return adapter, cred, nil
	}
	cred, err := e.Provider.ServiceCredential(ctx)
	if err != nil {
		return adapter, confluence.Credential{}, toolErr(CodeUpstreamAuth, "%v", err)
	}
	return adapter, cred, nil
}

// resolveTarget decides read (and the tool's own operation) for the target,
// then applies the MCP policy to the target's real space and page tree.
func (e *Executor) resolveTarget(ctx context.Context, c *Call, op permission.Op, rec Record) (Resolved, *Error) {
	var res Resolved
	ops := []permission.Op{permission.OpRead}
	if op != permission.OpRead {
		ops = append(ops, op)
	}
	var pt permission.Target
	if c.Target.ContentID != "" {
		pt = permission.ContentTarget(c.Target.ContentID)
	} else {
		pt = permission.SpaceTarget(c.Target.SpaceKey)
	}
	dec, err := e.Resolver.Check(ctx, c.Principal.Subject(), pt, ops...)
	if err != nil {
		return res, toolErr(CodePermissionUnknown, "%v", err)
	}
	res.Decision = dec
	switch dec.Verdict(permission.OpRead) {
	case permission.Unknown:
		return res, toolErr(CodePermissionUnknown, "요청자의 열람 권한을 확인할 수 없습니다 (%s)", dec.Reasons[permission.OpRead])
	case permission.Deny:
		return res, toolErr(CodePermissionDenied, hiddenMessage)
	}

	if c.Target.ContentID != "" {
		ct, err := c.Adapter.Content(ctx, c.Cred, c.Target.ContentID, "space", "version", "ancestors", "container")
		if err != nil {
			if confluence.IsStatus(err, 403, 404) {
				return res, toolErr(CodePermissionDenied, hiddenMessage)
			}
			return res, classify(err)
		}
		if dec.SpaceKey != "" && ct.SpaceKey != "" && !strings.EqualFold(dec.SpaceKey, ct.SpaceKey) {
			return res, toolErr(CodePermissionUnknown, "권한 판정과 실제 문서의 공간이 다릅니다")
		}
		if ct.Status != "" && ct.Status != "current" {
			return res, toolErr(CodePermissionDenied, hiddenMessage)
		}
		res.Content = ct
		res.ContentID, res.SpaceKey, res.ContentType = ct.ID, ct.SpaceKey, ct.Type
		res.Ancestors = ct.AncestorIDs()
		// A comment or attachment sits in its container's tree.
		if ct.Container != nil && ct.Container.ID != "" && (ct.Type == "comment" || ct.Type == "attachment") {
			res.Ancestors = append(res.Ancestors, ct.Container.ID)
		}
	} else {
		res.SpaceKey = strings.ToUpper(c.Target.SpaceKey)
		if dec.SpaceKey != "" {
			res.SpaceKey = dec.SpaceKey
		}
	}

	verdict, err := e.Policy.Evaluate(ctx, policy.Target{
		SpaceKey: res.SpaceKey, ContentID: res.ContentID, Ancestors: res.Ancestors, ContentType: res.ContentType,
	})
	if err != nil {
		return res, toolErr(CodeInternal, "정책 평가 실패: %v", err)
	}
	if !verdict.Allowed {
		return res, toolErr(CodePolicyDenied, "%s", verdict.Reason)
	}
	if !policy.RiskAllowed(rec.Risk, verdict.RiskCap) {
		return res, toolErr(CodePolicyDenied, "정책이 이 대상에서 %s 등급 도구를 허용하지 않습니다 (상한 %s)", rec.Risk, verdict.RiskCap)
	}
	res.Verdict = verdict

	if op != permission.OpRead {
		switch dec.Verdict(op) {
		case permission.Allow:
		case permission.Deny:
			return res, toolErr(CodePermissionDenied, "요청자에게 이 대상의 %s 권한이 없습니다", opLabel(op))
		default:
			return res, toolErr(CodePermissionUnknown, "요청자의 %s 권한을 확인할 수 없습니다", opLabel(op))
		}
	}
	return res, nil
}

func opLabel(op permission.Op) string {
	switch op {
	case permission.OpRead:
		return "열람"
	case permission.OpCreate:
		return "작성"
	case permission.OpEdit:
		return "편집"
	case permission.OpComment:
		return "댓글"
	case permission.OpAttach:
		return "첨부"
	case permission.OpMove:
		return "이동"
	case permission.OpDelete:
		return "삭제"
	}
	return string(op)
}

// write runs the change pipeline: prepare → approval → reserve → apply →
// verify. The approval is consumed before the upstream call, so a failed
// write needs a new proposal; the operation record keeps the same approved
// change from ever being sent twice.
func (e *Executor) write(ctx context.Context, c *Call, def Definition, rec Record, tr *trace, propose bool) (any, *Error) {
	// A retry of a write that already ran with this approval reports the
	// earlier outcome; it is never prepared or sent again.
	if raw := strings.TrimSpace(c.Args.OptString("approvalId", "")); raw != "" && !propose {
		if id, err := uuid.Parse(raw); err == nil {
			if prev, perr := e.Operations.ByKey(ctx, operation.Key(c.Principal.KeycloakSub, def.Name,
				approval.Hash(def.Name, c.Args), &id)); perr == nil {
				tr.approval = &id
				switch prev.Status {
				case operation.StatusSucceeded:
					return c.result(map[string]any{"alreadyProcessed": true, "operationId": prev.ID, "status": prev.Status,
						"contentId": prev.UpstreamID, "resultVersion": prev.ResultVersion}, nil, nil,
						"이 승인으로 이미 적용된 변경입니다. 다시 보내지 않았습니다"), nil
				case operation.StatusFailed:
					return nil, toolErr(CodeApprovalStale, "이 승인으로 실행한 쓰기는 실패했습니다 (%s). 새 변경안을 만드십시오", prev.ErrorCode)
				case operation.StatusExecuting:
					return nil, toolErr(CodeOutcomeUnknown, "같은 승인으로 쓰기가 실행 중입니다 (운영 기록 %s). 다시 보내지 말고 잠시 후 결과를 조회하십시오", prev.ID)
				default:
					return nil, toolErr(CodeOutcomeUnknown, "이 승인으로 실행한 쓰기의 결과가 불명확합니다 (운영 기록 %s). 다시 보내지 않습니다", prev.ID)
				}
			}
		}
	}
	plan, err := def.Prepare(ctx, c)
	if err != nil {
		return nil, classify(err)
	}
	if plan.SpaceKey == "" {
		plan.SpaceKey = c.Resolved.SpaceKey
	}
	plan.Preview.SpaceKey = plan.SpaceKey
	if plan.BaseVersion != nil {
		plan.Preview.BaseVersion = *plan.BaseVersion
	}
	gen, err := e.Policy.Generation(ctx)
	if err != nil {
		return nil, toolErr(CodeInternal, "%v", err)
	}

	approvalID := strings.TrimSpace(c.Args.OptString("approvalId", c.Args.OptString("approval_id", "")))
	if propose || (rec.RequiresApproval && approvalID == "") {
		if propose && !rec.RequiresApproval {
			return c.result(map[string]any{
				"approvalRequired": false,
				"preview":          plan.Preview,
				"nextCall":         map[string]any{"tool": def.Name, "arguments": c.Args},
				"message":          "이 도구는 승인 없이 실행되도록 설정되어 있습니다. nextCall 로 실행하십시오.",
			}, nil, nil), nil
		}
		req, err := e.openApproval(ctx, c, def, rec, plan, gen)
		if err != nil {
			return nil, toolErr(CodeInternal, "%v", err)
		}
		approvalURL := e.consoleURL(c, "/me/approvals/"+req.ID.String())
		next := Args{}
		for k, v := range c.Args {
			next[k] = v
		}
		next["approvalId"] = req.ID.String()
		summary := map[string]any{
			"approvalId":       req.ID,
			"status":           req.Status,
			"expiresAt":        req.ExpiresAt,
			"requiresApprover": req.RequiresApprover,
			"approvalUrl":      approvalURL,
			"preview":          shortPreview(plan.Preview),
			"nextCall":         map[string]any{"tool": def.Name, "arguments": next},
		}
		id := req.ID
		tr.approval = &id
		if propose {
			return c.result(summary, nil, nil, "변경안이 저장되었습니다. 콘솔에서 승인한 뒤 nextCall 로 실행하십시오. Confluence 에는 아직 아무것도 쓰지 않았습니다."), nil
		}
		return nil, &Error{Code: CodeApprovalRequired, Approval: req, Details: summary,
			Message: fmt.Sprintf("이 작업은 승인이 필요합니다. 변경안 %s 이 생성되었습니다. 콘솔(%s)에서 내용을 확인하고 승인한 뒤 approvalId 를 넣어 다시 호출하십시오. Confluence 에는 아직 쓰지 않았습니다.",
				req.ID, approvalURL)}
	}

	var used *approval.Request
	if rec.RequiresApproval {
		id, err := uuid.Parse(approvalID)
		if err != nil {
			return nil, toolErr(CodeApprovalDenied, "approvalId 형식이 올바르지 않습니다")
		}
		tr.approval = &id
		req, err := e.Approvals.Check(ctx, id, c.Principal.KeycloakSub, def.Name, c.Args, approval.Current{
			Version: plan.BaseVersion, Hash: plan.BaseHash, PolicyGeneration: gen,
		})
		if err != nil {
			code := CodeApprovalDenied
			switch {
			case errors.Is(err, approval.ErrStale):
				code = CodeApprovalStale
			case errors.Is(err, approval.ErrRequired):
				code = CodeApprovalRequired
			}
			return nil, toolErr(code, "%v", err)
		}
		used = req
	}

	argsHash := approval.Hash(def.Name, c.Args)
	key := strings.TrimSpace(c.Args.OptString("idempotencyKey", ""))
	switch {
	case key != "":
		key = operation.Key(c.Principal.KeycloakSub, def.Name, "client:"+key, nil)
	case used != nil:
		key = operation.Key(c.Principal.KeycloakSub, def.Name, argsHash, &used.ID)
	default:
		// Without an approval or a client key, identical writes inside one
		// minute are treated as a retry of the same request.
		key = operation.Key(c.Principal.KeycloakSub, def.Name, argsHash+"|"+time.Now().UTC().Format("200601021504"), nil)
	}
	var approvalRef *uuid.UUID
	if used != nil {
		id := used.ID
		approvalRef = &id
	}
	op, err := e.Operations.Begin(ctx, operation.Record{
		IdempotencyKey: key, KeycloakSub: c.Principal.KeycloakSub, Username: c.Principal.Username,
		ToolName: def.Name, ArgumentsHash: argsHash, ApprovalID: approvalRef, TargetID: plan.TargetID,
		ExecutedBy: c.Cred.Actor(),
	})
	if err != nil {
		if errors.Is(err, operation.ErrInFlight) {
			return nil, toolErr(CodeOutcomeUnknown, "%v. 같은 요청을 다시 보내지 말고 운영 기록(%s)을 확인하십시오", err, op.ID)
		}
		return nil, toolErr(CodeInternal, "%v", err)
	}
	if op.Status == operation.StatusSucceeded || op.Status == operation.StatusFailed {
		return c.result(map[string]any{
			"alreadyProcessed": true, "operationId": op.ID, "status": op.Status, "contentId": op.UpstreamID,
			"resultVersion": op.ResultVersion,
		}, nil, nil, "같은 요청이 이미 처리되어 다시 보내지 않았습니다"), nil
	}
	tr.detail["operationId"] = op.ID.String()

	verifyID, result, err := plan.Apply(ctx)
	if err != nil {
		terr := classify(err)
		status := operation.StatusFailed
		if terr.Code == CodeOutcomeUnknown {
			status = operation.StatusOutcomeUnknown
			terr.Message += fmt.Sprintf(". 자동으로 다시 보내지 않습니다. Confluence 에서 결과를 확인한 뒤 운영 기록 %s 을 정리하십시오", op.ID)
		}
		e.Operations.Finish(ctx, op.ID, status, "", nil, terr.Code, terr.Message)
		return nil, terr
	}
	var version *int
	if plan.Verify != nil && verifyID != "" {
		v, verr := plan.Verify(ctx, verifyID)
		if verr != nil {
			msg := "쓰기 요청은 응답했지만 결과를 직접 조회해 확인하지 못했습니다: " + verr.Error()
			e.Operations.Finish(ctx, op.ID, operation.StatusOutcomeUnknown, verifyID, nil, CodeOutcomeUnknown, msg)
			return nil, toolErr(CodeOutcomeUnknown, "%s (운영 기록 %s)", msg, op.ID)
		}
		version = &v
		tr.version = &v
	}
	e.Operations.Finish(ctx, op.ID, operation.StatusSucceeded, verifyID, version, "", "")
	if verifyID != "" {
		tr.contentID = verifyID
	}
	out := c.result(result, nil, nil)
	out.Data = map[string]any{"operationId": op.ID, "result": result, "verifiedVersion": version}
	return out, nil
}

func shortPreview(p approval.Preview) map[string]any {
	diff := p.Diff
	if len(diff) > 4000 {
		diff = diff[:4000] + "\n… (전체 diff 는 콘솔에서 확인)"
	}
	return map[string]any{"action": p.Action, "summary": p.Summary, "title": p.Title, "spaceKey": p.SpaceKey,
		"targetUrl": p.TargetURL, "baseVersion": p.BaseVersion, "diff": diff, "warnings": p.Warnings, "stats": p.Stats}
}

func (e *Executor) openApproval(ctx context.Context, c *Call, def Definition, rec Record, plan *Plan, gen int64) (*approval.Request, error) {
	sec, _ := e.Store.Security(ctx)
	ttl := time.Duration(sec.ApprovalTTLMin) * time.Minute
	var userID *int64
	if c.Principal.UserID != 0 {
		id := c.Principal.UserID
		userID = &id
	}
	resource := plan.SpaceKey
	if plan.TargetID != "" {
		resource += "/" + plan.TargetID
	}
	if plan.Preview.Title != "" {
		resource += " " + plan.Preview.Title
	}
	return e.Approvals.Create(ctx, approval.CreateInput{
		Sub: c.Principal.KeycloakSub, UserID: userID, Username: c.Principal.Username,
		InstanceID: c.Cfg.InstanceID, ToolName: def.Name, Risk: rec.Risk, Args: c.Args,
		Resource: resource, SpaceKey: plan.SpaceKey, TargetID: plan.TargetID,
		TargetVersion: plan.BaseVersion, TargetHash: plan.BaseHash, PolicyGeneration: gen,
		Preview: &plan.Preview, RequiresApprover: rec.Risk == RiskExecute || !sec.AllowSelfApproval, TTL: ttl,
	})
}

// consoleURL builds a console link from the configured MCP resource URL.
func (e *Executor) consoleURL(c *Call, path string) string {
	base := strings.TrimRight(c.MCP.ResourceURL, "/")
	base = strings.TrimSuffix(base, "/mcp")
	return base + path
}

// classify maps an error onto a client error code.
func classify(err error) *Error {
	var te *Error
	if errors.As(err, &te) {
		return te
	}
	var apiErr *confluence.APIError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return toolErr(CodeUpstreamUnavailable, "도구 실행 시간 한도를 넘었습니다")
	case errors.Is(err, confluence.ErrOutcomeUnknown):
		return toolErr(CodeOutcomeUnknown, "%v", err)
	case errors.Is(err, confluence.ErrAuthPage):
		return toolErr(CodeUpstreamAuth, "%v", err)
	case errors.Is(err, content.ErrUnsupported):
		return toolErr(CodeUnsupportedContent, "%v", err)
	case errors.Is(err, permission.ErrUnavailable):
		return toolErr(CodePermissionUnknown, "%v", err)
	case errors.Is(err, attachment.ErrNotFound):
		return toolErr(CodeBadArguments, "%v", err)
	case errors.As(err, &apiErr):
		switch apiErr.Status {
		case 403, 404:
			return toolErr(CodePermissionDenied, hiddenMessage)
		case 409:
			return toolErr(CodeVersionConflict, "Confluence 가 버전 충돌을 알렸습니다. 최신 문서로 새 변경안을 만드십시오 (%s)", apiErr.Message)
		case 400:
			return toolErr(CodeBadArguments, "%s", apiErr.Message)
		case 413:
			return toolErr(CodeLimitExceeded, "%s", apiErr.Message)
		default:
			return toolErr(CodeUpstreamUnavailable, "%v", err)
		}
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "VERSION_CONFLICT"):
		return toolErr(CodeVersionConflict, "%s", strings.TrimPrefix(msg, "VERSION_CONFLICT: "))
	case strings.Contains(msg, "LIMIT_EXCEEDED"):
		return toolErr(CodeLimitExceeded, "%s", strings.TrimPrefix(msg, "LIMIT_EXCEEDED: "))
	case strings.Contains(msg, "필수 인자") || strings.Contains(msg, "비어 있습니다") || strings.Contains(msg, "인자 ") ||
		strings.Contains(msg, "형식") || strings.Contains(msg, "찾을 수 없습니다"):
		return toolErr(CodeBadArguments, "%s", msg)
	}
	return toolErr(CodeUpstreamUnavailable, "%s", msg)
}

// fail audits a denied or failed call and returns the error unchanged.
func (e *Executor) fail(ctx context.Context, p Principal, name string, rec Record, tr *trace, err *Error) error {
	if rec.Name == "" {
		rec.Name = name
	}
	e.record(ctx, p, name, rec, tr, false, err.Code, err.Message)
	return err
}

func (e *Executor) record(ctx context.Context, p Principal, name string, rec Record, tr *trace, success bool, code, message string) {
	category := audit.CatTool
	if rec.Write {
		category = audit.CatWrite
	}
	if !success {
		switch code {
		case CodeApprovalRequired:
			category = audit.CatApproval
		case CodePermissionDenied, CodePolicyDenied, CodePermissionUnknown, CodeRoleDenied, CodeScopeDenied, CodeIdentityMissing:
			category = audit.CatDenied
		default:
			if category == audit.CatTool {
				category = audit.CatError
			}
		}
	}
	decision := tr.decision
	if !success && decision == "" {
		decision = "deny"
	}
	detail := map[string]any{"risk": rec.Risk}
	for k, v := range tr.detail {
		detail[k] = v
	}
	e.Audit.Write(ctx, audit.Entry{
		Category:           category,
		Action:             "tool.invoke",
		RequestID:          p.RequestID,
		KeycloakSub:        p.KeycloakSub,
		KeycloakUsername:   p.Username,
		ConfluenceUserKey:  p.ConfluenceUserKey,
		ConfluenceUsername: p.ConfluenceUsername,
		ExecutedAs:         tr.executedAs,
		MCPClient:          p.Client,
		AuthMode:           p.AuthMode,
		ToolName:           name,
		SpaceKey:           tr.spaceKey,
		ContentID:          tr.contentID,
		ContentVersion:     tr.version,
		Decision:           decision,
		ApprovalID:         tr.approval,
		Success:            success,
		ErrorCode:          code,
		Message:            message,
		LatencyMS:          int(time.Since(tr.started).Milliseconds()),
		IP:                 p.IP,
		Detail:             detail,
	})
}

func hasScope(scopes []string, want string) bool {
	for _, s := range scopes {
		if strings.EqualFold(s, want) {
			return true
		}
	}
	return false
}
