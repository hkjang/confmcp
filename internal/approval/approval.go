// Package approval implements server-side approval for Confluence writes.
//
// An approval is a change proposal stored in confmcp's own database: the
// exact arguments, the target's version and content hash at proposal time,
// the policy generation and a preview (diff) the approver reads in the
// console. It is never satisfied by a value the model sends such as
// confirmed=true. If the arguments, the target or the policy change after the
// proposal, the approval goes stale and the caller must propose again.
package approval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hkjang/confmcp/internal/crypto"
)

// Statuses of an approval request.
const (
	StatusPending  = "pending"
	StatusApproved = "approved"
	StatusRejected = "rejected"
	StatusExpired  = "expired"
	StatusConsumed = "consumed"
)

// Errors returned to MCP callers.
var (
	ErrRequired = errors.New("APPROVAL_REQUIRED")
	ErrStale    = errors.New("APPROVAL_STALE")
	ErrDenied   = errors.New("APPROVAL_DENIED")
)

// Preview is what the approver sees: the real body to be written and its
// effect. It is sealed at rest because it holds document content.
type Preview struct {
	Action       string         `json:"action"`
	Summary      string         `json:"summary"`
	SpaceKey     string         `json:"spaceKey,omitempty"`
	Title        string         `json:"title,omitempty"`
	OldTitle     string         `json:"oldTitle,omitempty"`
	TargetURL    string         `json:"targetUrl,omitempty"`
	ParentID     string         `json:"parentId,omitempty"`
	ParentTitle  string         `json:"parentTitle,omitempty"`
	NewParentID  string         `json:"newParentId,omitempty"`
	BaseVersion  int            `json:"baseVersion,omitempty"`
	Diff         string         `json:"diff,omitempty"`
	NewBody      string         `json:"newBody,omitempty"`
	BodyFormat   string         `json:"bodyFormat,omitempty"`
	Labels       []string       `json:"labels,omitempty"`
	Attachment   map[string]any `json:"attachment,omitempty"`
	Warnings     []string       `json:"warnings,omitempty"`
	Visibility   string         `json:"visibility,omitempty"`
	Stats        map[string]int `json:"stats,omitempty"`
}

// Request is an approval record.
type Request struct {
	ID               uuid.UUID      `json:"id"`
	KeycloakSub      string         `json:"keycloakSub"`
	UserID           *int64         `json:"userId,omitempty"`
	Username         string         `json:"username"`
	InstanceID       string         `json:"instanceId"`
	ToolName         string         `json:"toolName"`
	Risk             string         `json:"risk"`
	ArgumentsHash    string         `json:"argumentsHash"`
	Arguments        map[string]any `json:"arguments,omitempty"`
	Resource         string         `json:"resource"`
	SpaceKey         string         `json:"spaceKey,omitempty"`
	TargetID         string         `json:"targetId,omitempty"`
	TargetVersion    *int           `json:"targetVersion,omitempty"`
	TargetHash       string         `json:"targetHash,omitempty"`
	PolicyGeneration int64          `json:"policyGeneration"`
	Preview          *Preview       `json:"preview,omitempty"`
	RequiresApprover bool           `json:"requiresApprover"`
	Status           string         `json:"status"`
	DecidedBy        string         `json:"decidedBy,omitempty"`
	DecisionNote     string         `json:"decisionNote,omitempty"`
	CreatedAt        time.Time      `json:"createdAt"`
	ExpiresAt        time.Time      `json:"expiresAt"`
	ApprovedAt       *time.Time     `json:"approvedAt,omitempty"`
	ConsumedAt       *time.Time     `json:"consumedAt,omitempty"`
}

// Engine stores and checks approvals.
type Engine struct {
	pool   *pgxpool.Pool
	sealer *crypto.Sealer
}

// NewEngine builds the engine.
func NewEngine(pool *pgxpool.Pool, sealer *crypto.Sealer) *Engine {
	return &Engine{pool: pool, sealer: sealer}
}

// Hash canonicalises tool arguments into a stable digest. The approval id
// itself is excluded so that submitting the approval does not change the hash.
func Hash(toolName string, args map[string]any) string {
	keys := make([]string, 0, len(args))
	for k := range args {
		if strings.EqualFold(k, "approvalId") || strings.EqualFold(k, "approval_id") {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	sb.WriteString(toolName)
	for _, k := range keys {
		b, _ := json.Marshal(normalise(args[k]))
		sb.WriteString("\n")
		sb.WriteString(k)
		sb.WriteString("=")
		sb.Write(b)
	}
	return crypto.SHA256Hex(sb.String())
}

// normalise makes numbers and strings hash the same however they arrived:
// a JSON client may send 7 or 7.0 or "7".
func normalise(v any) any {
	switch t := v.(type) {
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%g", t)
	case int:
		return fmt.Sprintf("%d", t)
	case int64:
		return fmt.Sprintf("%d", t)
	case string:
		return strings.TrimSpace(t)
	case []any:
		out := make([]any, len(t))
		for i := range t {
			out[i] = normalise(t[i])
		}
		return out
	case map[string]any:
		out := map[string]any{}
		for k, x := range t {
			out[k] = normalise(x)
		}
		return out
	default:
		return v
	}
}

const cols = `id, keycloak_sub, user_id, username, instance_id, tool_name, risk_level, arguments_hash,
	arguments_redacted, resource, space_key, target_id, target_version, target_hash, policy_generation,
	COALESCE(preview_enc,''), requires_approver, status, COALESCE(decided_by,''), COALESCE(decision_note,''),
	created_at, expires_at, approved_at, consumed_at`

func (e *Engine) scan(row pgx.Row, withPreview bool) (*Request, error) {
	var r Request
	var args []byte
	var previewEnc string
	if err := row.Scan(&r.ID, &r.KeycloakSub, &r.UserID, &r.Username, &r.InstanceID, &r.ToolName, &r.Risk,
		&r.ArgumentsHash, &args, &r.Resource, &r.SpaceKey, &r.TargetID, &r.TargetVersion, &r.TargetHash,
		&r.PolicyGeneration, &previewEnc, &r.RequiresApprover, &r.Status, &r.DecidedBy, &r.DecisionNote,
		&r.CreatedAt, &r.ExpiresAt, &r.ApprovedAt, &r.ConsumedAt); err != nil {
		return nil, err
	}
	if len(args) > 0 {
		_ = json.Unmarshal(args, &r.Arguments)
	}
	if withPreview && previewEnc != "" && e.sealer != nil {
		if plain, err := e.sealer.Open(previewEnc); err == nil {
			var p Preview
			if json.Unmarshal([]byte(plain), &p) == nil {
				r.Preview = &p
			}
		}
	}
	if r.Status == StatusPending && time.Now().After(r.ExpiresAt) {
		r.Status = StatusExpired
	}
	return &r, nil
}

// CreateInput describes a new proposal.
type CreateInput struct {
	Sub              string
	UserID           *int64
	Username         string
	InstanceID       string
	ToolName         string
	Risk             string
	Args             map[string]any
	Resource         string
	SpaceKey         string
	TargetID         string
	TargetVersion    *int
	TargetHash       string
	PolicyGeneration int64
	Preview          *Preview
	RequiresApprover bool
	TTL              time.Duration
}

// Create opens a pending approval request.
func (e *Engine) Create(ctx context.Context, in CreateInput) (*Request, error) {
	if in.TTL <= 0 {
		in.TTL = 30 * time.Minute
	}
	body, _ := json.Marshal(redactArgs(in.Args))
	previewEnc := ""
	if in.Preview != nil && e.sealer != nil {
		raw, _ := json.Marshal(in.Preview)
		var err error
		if previewEnc, err = e.sealer.Seal(string(raw)); err != nil {
			return nil, err
		}
	}
	if in.InstanceID == "" {
		in.InstanceID = "default"
	}
	id := uuid.New()
	_, err := e.pool.Exec(ctx, `
		INSERT INTO approval_requests(id, keycloak_sub, user_id, username, instance_id, tool_name, risk_level,
			arguments_hash, arguments_redacted, resource, space_key, target_id, target_version, target_hash,
			policy_generation, preview_enc, requires_approver, status, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,NULLIF($16,''),$17,$18,$19)`,
		id, in.Sub, in.UserID, in.Username, in.InstanceID, in.ToolName, in.Risk, Hash(in.ToolName, in.Args),
		body, in.Resource, in.SpaceKey, in.TargetID, in.TargetVersion, in.TargetHash, in.PolicyGeneration,
		previewEnc, in.RequiresApprover, StatusPending, time.Now().Add(in.TTL))
	if err != nil {
		return nil, err
	}
	return e.ByID(ctx, id)
}

// ByID loads one request with its preview.
func (e *Engine) ByID(ctx context.Context, id uuid.UUID) (*Request, error) {
	r, err := e.scan(e.pool.QueryRow(ctx, `SELECT `+cols+` FROM approval_requests WHERE id=$1`, id), true)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("승인 요청을 찾을 수 없습니다")
	}
	return r, err
}

// List returns requests, optionally filtered by status or requester. Previews
// are omitted; the console loads one request at a time to show a diff.
func (e *Engine) List(ctx context.Context, status, sub string, limit int) ([]Request, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := e.pool.Query(ctx, `SELECT `+cols+` FROM approval_requests
		WHERE ($1='' OR status=$1 OR ($1='expired' AND status='pending' AND expires_at < NOW()))
		  AND ($2='' OR keycloak_sub=$2)
		ORDER BY created_at DESC LIMIT $3`, status, sub, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Request{}
	for rows.Next() {
		r, err := e.scan(rows, false)
		if err != nil {
			return nil, err
		}
		if status == StatusPending && r.Status != StatusPending {
			continue
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// Decide approves or rejects a request.
func (e *Engine) Decide(ctx context.Context, id uuid.UUID, approve bool, decidedBy, note string) (*Request, error) {
	req, err := e.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if req.Status == StatusExpired {
		_, _ = e.pool.Exec(ctx, `UPDATE approval_requests SET status=$2 WHERE id=$1 AND status='pending'`, id, StatusExpired)
		return nil, fmt.Errorf("만료된 요청입니다")
	}
	if req.Status != StatusPending {
		return nil, fmt.Errorf("이미 처리된 요청입니다 (%s)", req.Status)
	}
	status := StatusRejected
	if approve {
		status = StatusApproved
	}
	tag, err := e.pool.Exec(ctx, `
		UPDATE approval_requests
		SET status = $2::VARCHAR, decided_by = $3, decision_note = $4,
		    approved_at = CASE WHEN $2::VARCHAR = 'approved' THEN NOW() ELSE NULL END
		WHERE id = $1 AND status = 'pending'`, id, status, decidedBy, note)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, fmt.Errorf("이미 처리된 요청입니다")
	}
	return e.ByID(ctx, id)
}

// Current is the live state the approval is compared against.
type Current struct {
	Version          *int
	Hash             string
	PolicyGeneration int64
}

// Check validates an approval for a tool call and atomically consumes it.
//
// A pinned target version or content hash that cannot be confirmed now is a
// refusal, not a pass. Consumption is a conditional UPDATE, so one approval is
// spent exactly once even under concurrent calls.
func (e *Engine) Check(ctx context.Context, id uuid.UUID, sub, toolName string, args map[string]any, cur Current) (*Request, error) {
	req, err := e.ByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDenied, err)
	}
	switch {
	case req.KeycloakSub != sub:
		return nil, fmt.Errorf("%w: 승인 요청자가 아닙니다", ErrDenied)
	case req.ToolName != toolName:
		return nil, fmt.Errorf("%w: 다른 도구의 승인입니다 (%s)", ErrDenied, req.ToolName)
	case req.Status == StatusRejected:
		return nil, fmt.Errorf("%w: 승인이 거절되었습니다", ErrDenied)
	case req.Status == StatusConsumed:
		return nil, fmt.Errorf("%w: 이미 사용된 승인입니다", ErrStale)
	case req.Status == StatusExpired:
		_, _ = e.pool.Exec(ctx, `UPDATE approval_requests SET status=$2 WHERE id=$1 AND status='pending'`, id, StatusExpired)
		return nil, fmt.Errorf("%w: 승인이 만료되었습니다", ErrStale)
	case req.Status != StatusApproved:
		return nil, fmt.Errorf("%w: 아직 승인되지 않았습니다. 콘솔에서 변경안을 확인하고 승인하십시오", ErrRequired)
	case time.Now().After(req.ExpiresAt):
		_, _ = e.pool.Exec(ctx, `UPDATE approval_requests SET status=$2 WHERE id=$1`, id, StatusExpired)
		return nil, fmt.Errorf("%w: 승인이 만료되었습니다", ErrStale)
	case req.ArgumentsHash != Hash(toolName, args):
		return nil, fmt.Errorf("%w: 승인 이후 인자가 변경되었습니다", ErrStale)
	case req.PolicyGeneration != 0 && cur.PolicyGeneration != req.PolicyGeneration:
		return nil, fmt.Errorf("%w: 승인 이후 접근 정책이 변경되었습니다", ErrStale)
	}
	if req.TargetVersion != nil {
		if cur.Version == nil {
			return nil, fmt.Errorf("%w: 대상의 현재 버전을 확인할 수 없습니다", ErrStale)
		}
		if *cur.Version != *req.TargetVersion {
			return nil, fmt.Errorf("%w: 승인 시 버전 %d, 현재 버전 %d", ErrStale, *req.TargetVersion, *cur.Version)
		}
	}
	if req.TargetHash != "" && cur.Hash != req.TargetHash {
		return nil, fmt.Errorf("%w: 승인 이후 대상 원문이 변경되었습니다", ErrStale)
	}
	tag, err := e.pool.Exec(ctx,
		`UPDATE approval_requests SET status=$2::VARCHAR, consumed_at=NOW() WHERE id=$1 AND status='approved'`,
		id, StatusConsumed)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, fmt.Errorf("%w: 이미 사용된 승인입니다", ErrStale)
	}
	return req, nil
}

// ExpireStale marks timed-out pending requests as expired.
func (e *Engine) ExpireStale(ctx context.Context) error {
	_, err := e.pool.Exec(ctx,
		`UPDATE approval_requests SET status=$1 WHERE status=$2 AND expires_at < NOW()`,
		StatusExpired, StatusPending)
	return err
}

// PurgePreviews drops document content from proposals that can no longer be
// used, so stored drafts do not outlive their purpose.
func (e *Engine) PurgePreviews(ctx context.Context, olderThan time.Duration) error {
	_, err := e.pool.Exec(ctx, `UPDATE approval_requests SET preview_enc=NULL
		WHERE preview_enc IS NOT NULL AND status IN ('consumed','rejected','expired')
		  AND created_at < NOW() - make_interval(secs => $1)`, olderThan.Seconds())
	return err
}

// sensitiveKeyParts are matched as substrings against the lowercased argument
// name. Bodies stay readable to an approver; oversized values are truncated.
var sensitiveKeyParts = []string{
	"token", "secret", "password", "passwd", "credential", "authorization", "bearer",
	"apikey", "api_key", "accesskey", "access_key", "privatekey", "private_key",
}

func redactArgs(args map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range args {
		lk := strings.ToLower(k)
		if containsAny(lk, sensitiveKeyParts) {
			out[k] = "[redacted]"
			continue
		}
		if s, ok := v.(string); ok && len(s) > 1000 {
			out[k] = s[:1000] + "…"
			continue
		}
		out[k] = v
	}
	return out
}

func containsAny(s string, parts []string) bool {
	for _, p := range parts {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}
