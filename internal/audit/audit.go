// Package audit records every security-relevant action. Secrets, tokens,
// Authorization headers and document bodies are deliberately excluded.
package audit

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Categories used across the service.
const (
	CatAuth     = "auth"
	CatTool     = "tool"
	CatWrite    = "write"
	CatDenied   = "denied"
	CatApproval = "approval"
	CatAdmin    = "admin"
	CatKey      = "key"
	CatError    = "error"
	CatAI       = "ai"
	CatFile     = "file"
)

// Entry is one audit row. The requester (Keycloak and Confluence identity)
// and the account the REST call actually ran as are recorded separately.
type Entry struct {
	ID                 int64          `json:"id"`
	OccurredAt         time.Time      `json:"occurredAt"`
	Category           string         `json:"category"`
	Action             string         `json:"action"`
	RequestID          string         `json:"requestId,omitempty"`
	KeycloakSub        string         `json:"keycloakSub,omitempty"`
	KeycloakUsername   string         `json:"keycloakUsername,omitempty"`
	ConfluenceUserKey  string         `json:"confluenceUserKey,omitempty"`
	ConfluenceUsername string         `json:"confluenceUsername,omitempty"`
	ExecutedAs         string         `json:"executedAs,omitempty"`
	MCPClient          string         `json:"mcpClient,omitempty"`
	AuthMode           string         `json:"authMode,omitempty"`
	ToolName           string         `json:"toolName,omitempty"`
	SpaceKey           string         `json:"spaceKey,omitempty"`
	ContentID          string         `json:"contentId,omitempty"`
	ContentVersion     *int           `json:"contentVersion,omitempty"`
	Decision           string         `json:"decision,omitempty"`
	ApprovalID         *uuid.UUID     `json:"approvalId,omitempty"`
	Success            bool           `json:"success"`
	ErrorCode          string         `json:"errorCode,omitempty"`
	Message            string         `json:"message,omitempty"`
	LatencyMS          int            `json:"latencyMs"`
	IP                 string         `json:"ip,omitempty"`
	Detail             map[string]any `json:"detail,omitempty"`
}

// Logger writes audit entries.
type Logger struct{ pool *pgxpool.Pool }

// New builds a logger.
func New(pool *pgxpool.Pool) *Logger { return &Logger{pool: pool} }

// redactKeys are never stored, even if a caller passes them in Detail.
var redactKeys = []string{"authorization", "password", "secret", "token", "apikey", "api_key",
	"body", "storage", "content", "diff", "prompt", "messages", "credential"}

func redact(detail map[string]any) map[string]any {
	if detail == nil {
		return nil
	}
	out := make(map[string]any, len(detail))
	for k, v := range detail {
		lk := strings.ToLower(k)
		dropped := false
		for _, bad := range redactKeys {
			if strings.Contains(lk, bad) && lk != "contentid" {
				out[k] = "[redacted]"
				dropped = true
				break
			}
		}
		if !dropped {
			out[k] = v
		}
	}
	return out
}

// Write persists an entry. Audit failures never break the request path.
func (l *Logger) Write(ctx context.Context, e Entry) {
	e.Detail = redact(e.Detail)
	var detail []byte
	if e.Detail != nil {
		detail, _ = json.Marshal(e.Detail)
	}
	if len(e.Message) > 2000 {
		e.Message = e.Message[:2000]
	}
	// Use a detached context so a cancelled request still leaves a trace.
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_, _ = l.pool.Exec(wctx, `
		INSERT INTO audit_log(category, action, request_id, keycloak_sub, keycloak_username,
			confluence_user_key, confluence_username, executed_as, mcp_client, auth_mode, tool_name,
			space_key, content_id, content_version, decision, approval_id, success, error_code, message,
			latency_ms, ip, detail)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)`,
		e.Category, e.Action, nz(e.RequestID), nz(e.KeycloakSub), nz(e.KeycloakUsername),
		nz(e.ConfluenceUserKey), nz(e.ConfluenceUsername), nz(e.ExecutedAs), nz(e.MCPClient),
		nz(e.AuthMode), nz(e.ToolName), nz(e.SpaceKey), nz(e.ContentID), e.ContentVersion,
		nz(e.Decision), e.ApprovalID, e.Success, nz(e.ErrorCode), nz(e.Message), e.LatencyMS, nz(e.IP), detail)
}

// Query filters audit rows for the console.
type Query struct {
	Category string
	Username string
	Tool     string
	Space    string
	Content  string
	Code     string
	Success  *bool
	Since    *time.Time
	Until    *time.Time
	Limit    int
	Offset   int
	// Exact restricts Username to an exact match (personal audit view).
	Exact bool
}

// List returns matching entries, newest first.
func (l *Logger) List(ctx context.Context, q Query) ([]Entry, int, error) {
	if q.Limit <= 0 || q.Limit > 500 {
		q.Limit = 100
	}
	userCond := `($2='' OR keycloak_username ILIKE '%'||$2||'%' OR confluence_username ILIKE '%'||$2||'%')`
	if q.Exact {
		userCond = `keycloak_username = $2`
	}
	where := `WHERE ($1='' OR category=$1)
		AND ` + userCond + `
		AND ($3='' OR tool_name=$3)
		AND ($4='' OR space_key ILIKE $4)
		AND ($5='' OR content_id=$5)
		AND ($6='' OR error_code=$6)
		AND ($7::BOOLEAN IS NULL OR success=$7)
		AND ($8::TIMESTAMPTZ IS NULL OR occurred_at >= $8)
		AND ($9::TIMESTAMPTZ IS NULL OR occurred_at < $9)`
	args := []any{q.Category, q.Username, q.Tool, q.Space, q.Content, q.Code, q.Success, q.Since, q.Until}

	var total int
	if err := l.pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_log `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := l.pool.Query(ctx, `
		SELECT id, occurred_at, category, action, COALESCE(request_id,''), COALESCE(keycloak_sub,''),
		       COALESCE(keycloak_username,''), COALESCE(confluence_user_key,''), COALESCE(confluence_username,''),
		       COALESCE(executed_as,''), COALESCE(mcp_client,''), COALESCE(auth_mode,''), COALESCE(tool_name,''),
		       COALESCE(space_key,''), COALESCE(content_id,''), content_version, COALESCE(decision,''),
		       approval_id, success, COALESCE(error_code,''), COALESCE(message,''),
		       COALESCE(latency_ms,0), COALESCE(ip,''), detail
		FROM audit_log `+where+`
		ORDER BY occurred_at DESC, id DESC
		LIMIT `+itoa(q.Limit)+` OFFSET `+itoa(q.Offset), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := []Entry{}
	for rows.Next() {
		var e Entry
		var detail []byte
		if err := rows.Scan(&e.ID, &e.OccurredAt, &e.Category, &e.Action, &e.RequestID, &e.KeycloakSub,
			&e.KeycloakUsername, &e.ConfluenceUserKey, &e.ConfluenceUsername, &e.ExecutedAs, &e.MCPClient,
			&e.AuthMode, &e.ToolName, &e.SpaceKey, &e.ContentID, &e.ContentVersion, &e.Decision,
			&e.ApprovalID, &e.Success, &e.ErrorCode, &e.Message, &e.LatencyMS, &e.IP, &detail); err != nil {
			return nil, 0, err
		}
		if len(detail) > 0 {
			_ = json.Unmarshal(detail, &e.Detail)
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}

// Stats summarises tool traffic for the dashboard.
type Stats struct {
	Calls      int     `json:"calls"`
	Failures   int     `json:"failures"`
	Denied     int     `json:"denied"`
	ErrorRate  float64 `json:"errorRate"`
	P50MS      int     `json:"p50Ms"`
	P95MS      int     `json:"p95Ms"`
	AvgMS      int     `json:"avgMs"`
	Series     []Point `json:"series"`
}

// Point is one bucket of the traffic series.
type Point struct {
	At       time.Time `json:"at"`
	Calls    int       `json:"calls"`
	Failures int       `json:"failures"`
	P95MS    int       `json:"p95Ms"`
}

// Summary computes call volume, error rate and latency over a window.
func (l *Logger) Summary(ctx context.Context, since time.Duration, bucket string) (Stats, error) {
	var s Stats
	err := l.pool.QueryRow(ctx, `
		SELECT COUNT(*), COUNT(*) FILTER (WHERE NOT success AND category <> 'denied' AND category <> 'approval'),
		       COUNT(*) FILTER (WHERE category = 'denied'),
		       COALESCE(percentile_cont(0.5) WITHIN GROUP (ORDER BY latency_ms),0)::INT,
		       COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY latency_ms),0)::INT,
		       COALESCE(AVG(latency_ms),0)::INT
		FROM audit_log WHERE tool_name IS NOT NULL AND occurred_at > NOW() - make_interval(secs => $1)`,
		since.Seconds()).Scan(&s.Calls, &s.Failures, &s.Denied, &s.P50MS, &s.P95MS, &s.AvgMS)
	if err != nil {
		return s, err
	}
	if s.Calls > 0 {
		s.ErrorRate = float64(s.Failures) / float64(s.Calls)
	}
	if bucket != "hour" && bucket != "day" {
		bucket = "hour"
	}
	rows, err := l.pool.Query(ctx, `
		SELECT date_trunc('`+bucket+`', occurred_at) AS b, COUNT(*),
		       COUNT(*) FILTER (WHERE NOT success AND category <> 'denied' AND category <> 'approval'),
		       COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY latency_ms),0)::INT
		FROM audit_log WHERE tool_name IS NOT NULL AND occurred_at > NOW() - make_interval(secs => $1)
		GROUP BY b ORDER BY b`, since.Seconds())
	if err != nil {
		return s, err
	}
	defer rows.Close()
	s.Series = []Point{}
	for rows.Next() {
		var p Point
		if err := rows.Scan(&p.At, &p.Calls, &p.Failures, &p.P95MS); err != nil {
			return s, err
		}
		s.Series = append(s.Series, p)
	}
	return s, rows.Err()
}

// Purge deletes entries older than the retention window.
func (l *Logger) Purge(ctx context.Context, retainDays int) error {
	if retainDays <= 0 {
		return nil
	}
	_, err := l.pool.Exec(ctx,
		`DELETE FROM audit_log WHERE occurred_at < NOW() - make_interval(days => $1)`, retainDays)
	return err
}

func nz(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func itoa(n int) string {
	if n < 0 {
		n = 0
	}
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}
