package tools

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Record is a tool's effective configuration: the shipped definition merged
// with the administrator's stored overrides.
type Record struct {
	Name             string         `json:"name"`
	Title            string         `json:"title"`
	Description      string         `json:"description"`
	Group            string         `json:"group"`
	Risk             string         `json:"risk"`
	RequiredPerm     string         `json:"requiredPermission"`
	Scope            string         `json:"scope"`
	Priority         string         `json:"priority"`
	HighLevel        bool           `json:"highLevel"`
	Write            bool           `json:"write"`
	Enabled          bool           `json:"enabled"`
	RequiresApproval bool           `json:"requiresApproval"`
	MinRole          string         `json:"minRole"`
	InputSchema      map[string]any `json:"inputSchema,omitempty"`
	UpdatedAt        time.Time      `json:"updatedAt,omitempty"`
}

// Registry merges code definitions with database overrides.
type Registry struct {
	pool *pgxpool.Pool
	defs map[string]Definition

	mu     sync.RWMutex
	cache  map[string]Record
	loaded time.Time
	ttl    time.Duration
}

// NewRegistry builds the registry over the shipped definitions.
func NewRegistry(pool *pgxpool.Pool) *Registry {
	defs := map[string]Definition{}
	for _, d := range All() {
		defs[d.Name] = d
	}
	return &Registry{pool: pool, defs: defs, ttl: 10 * time.Second}
}

// Definitions exposes the shipped definitions.
func (r *Registry) Definitions() map[string]Definition { return r.defs }

// Sync reconciles the mcp_tools table with the shipped definitions, inserting
// new tools, refreshing descriptions and removing tools that no longer exist.
// Administrator choices (enabled, approval, min role) survive the sync.
func (r *Registry) Sync(ctx context.Context) error {
	names := make([]string, 0, len(r.defs))
	for name, d := range r.defs {
		names = append(names, name)
		minRole := d.MinRole
		if minRole == "" {
			minRole = defaultMinRole(d.Risk)
		}
		if _, err := r.pool.Exec(ctx, `
			INSERT INTO mcp_tools(name, title, description, tool_group, risk_level,
				required_perm, priority, enabled, requires_approval, min_role, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NOW())
			ON CONFLICT (name) DO UPDATE
			   SET title = EXCLUDED.title,
			       description = EXCLUDED.description,
			       tool_group = EXCLUDED.tool_group,
			       risk_level = EXCLUDED.risk_level,
			       required_perm = EXCLUDED.required_perm,
			       priority = EXCLUDED.priority,
			       updated_at = NOW()`,
			name, d.Title, d.Description, d.Group, d.Risk, d.RequiredPerm, d.Priority,
			d.DefaultEnabled, d.DefaultApproval, minRole); err != nil {
			return err
		}
	}
	if _, err := r.pool.Exec(ctx, `DELETE FROM mcp_tools WHERE name <> ALL($1)`, names); err != nil {
		return err
	}
	r.Reset()
	return nil
}

// Reset forces a reload of the stored overrides.
func (r *Registry) Reset() {
	r.mu.Lock()
	r.loaded = time.Time{}
	r.mu.Unlock()
}

func (r *Registry) fill(rec *Record, d Definition) {
	rec.Scope = d.Scope
	rec.HighLevel = d.HighLevel
	rec.InputSchema = d.InputSchema
	rec.Write = d.IsWrite()
	rec.Priority = d.Priority
}

func (r *Registry) load(ctx context.Context) (map[string]Record, error) {
	r.mu.RLock()
	if !r.loaded.IsZero() && time.Since(r.loaded) < r.ttl {
		out := r.cache
		r.mu.RUnlock()
		return out, nil
	}
	r.mu.RUnlock()

	rows, err := r.pool.Query(ctx, `
		SELECT name, title, description, tool_group, risk_level, required_perm,
		       enabled, requires_approval, min_role, updated_at FROM mcp_tools`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]Record{}
	for rows.Next() {
		var rec Record
		if err := rows.Scan(&rec.Name, &rec.Title, &rec.Description, &rec.Group,
			&rec.Risk, &rec.RequiredPerm, &rec.Enabled, &rec.RequiresApproval,
			&rec.MinRole, &rec.UpdatedAt); err != nil {
			return nil, err
		}
		if d, ok := r.defs[rec.Name]; ok {
			r.fill(&rec, d)
		}
		out[rec.Name] = rec
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	r.mu.Lock()
	r.cache, r.loaded = out, time.Now()
	r.mu.Unlock()
	return out, nil
}

// List returns every tool record in definition order.
func (r *Registry) List(ctx context.Context) ([]Record, error) {
	m, err := r.load(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Record, 0, len(m))
	for _, d := range All() {
		if rec, ok := m[d.Name]; ok {
			out = append(out, rec)
		}
	}
	return out, nil
}

// ErrUnknownTool is returned for a name that is not registered.
var ErrUnknownTool = errors.New("알 수 없는 도구입니다")

// Get returns a definition with its effective record.
func (r *Registry) Get(ctx context.Context, name string) (Definition, Record, error) {
	def, ok := r.defs[name]
	if !ok {
		return Definition{}, Record{}, fmt.Errorf("%w: %s", ErrUnknownTool, name)
	}
	m, err := r.load(ctx)
	if err != nil {
		return def, Record{}, err
	}
	rec, ok := m[name]
	if !ok {
		rec = Record{
			Name: def.Name, Title: def.Title, Description: def.Description,
			Group: def.Group, Risk: def.Risk, RequiredPerm: def.RequiredPerm,
			Enabled: def.DefaultEnabled, RequiresApproval: def.DefaultApproval,
			MinRole: defaultMinRole(def.Risk),
		}
		r.fill(&rec, def)
	}
	return def, rec, nil
}

// Update stores administrator overrides for a tool.
func (r *Registry) Update(ctx context.Context, name string, enabled, requiresApproval bool, minRole string) error {
	if _, ok := r.defs[name]; !ok {
		return fmt.Errorf("%w: %s", ErrUnknownTool, name)
	}
	if minRole == "" {
		minRole = defaultMinRole(r.defs[name].Risk)
	}
	if roleRank(minRole) == 0 {
		return fmt.Errorf("알 수 없는 역할입니다: %s", minRole)
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE mcp_tools SET enabled=$2, requires_approval=$3, min_role=$4, updated_at=NOW()
		WHERE name=$1`, name, enabled, requiresApproval, minRole)
	r.Reset()
	return err
}

// Groups lists the distinct tool groups.
func (r *Registry) Groups(ctx context.Context) ([]string, error) {
	recs, err := r.List(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := []string{}
	for _, rec := range recs {
		if !seen[rec.Group] {
			seen[rec.Group] = true
			out = append(out, rec.Group)
		}
	}
	return out, nil
}

// Keycloak roles confmcp recognises, in increasing order of capability. The
// admin role manages settings; it does not widen anyone's Confluence access.
const (
	RoleReader   = "confluence-mcp-reader"
	RoleWriter   = "confluence-mcp-writer"
	RoleApprover = "confluence-mcp-approver"
	RoleAdmin    = "confluence-mcp-admin"
)

// Roles lists the roles for the UI.
func Roles() []map[string]string {
	return []map[string]string{
		{"name": RoleReader, "label": "조회자", "description": "조회 도구"},
		{"name": RoleWriter, "label": "작성자", "description": "조회 + 변경안 작성(본인 승인)"},
		{"name": RoleApprover, "label": "승인자", "description": "작성 + 이동·휴지통 및 타인 변경 승인"},
		{"name": RoleAdmin, "label": "관리자", "description": "설정 관리 (문서 권한 우회 없음)"},
	}
}

func defaultMinRole(risk string) string {
	switch risk {
	case RiskWrite:
		return RoleWriter
	case RiskExecute:
		return RoleApprover
	case RiskAdmin:
		return RoleAdmin
	default:
		return RoleReader
	}
}

func roleRank(role string) int {
	switch role {
	case RoleReader:
		return 1
	case RoleWriter:
		return 2
	case RoleApprover:
		return 3
	case RoleAdmin:
		return 4
	default:
		return 0
	}
}

// RoleSatisfied reports whether any held role meets the minimum.
func RoleSatisfied(held []string, min string) bool {
	need := roleRank(min)
	if need == 0 {
		return true
	}
	for _, r := range held {
		if roleRank(r) >= need {
			return true
		}
	}
	return false
}

// CanApprove reports whether the roles allow deciding other people's
// approvals and EXECUTE-risk changes.
func CanApprove(held []string) bool { return RoleSatisfied(held, RoleApprover) }

// Scope names. API keys carry a subset; OAuth tokens get them from roles.
const (
	ScopeRead            = "confluence:read"
	ScopeWrite           = "confluence:write"
	ScopeAttachmentRead  = "confluence:attachment:read"
	ScopeAttachmentWrite = "confluence:attachment:write"
	ScopeExecute         = "confluence:execute"
	ScopeAIInvoke        = "ai:invoke"
	ScopeAdminRead       = "admin:read"
	ScopeAdminWrite      = "admin:write"
)

// ScopesForRoles maps Keycloak roles onto API scopes, so an OAuth caller and
// an API key caller are authorised through the same scope checks.
func ScopesForRoles(roles []string) []string {
	out := map[string]bool{}
	for _, r := range roles {
		rank := roleRank(r)
		if rank >= 1 {
			out[ScopeRead], out[ScopeAttachmentRead] = true, true
		}
		if rank >= 2 {
			out[ScopeWrite], out[ScopeAttachmentWrite], out[ScopeAIInvoke] = true, true, true
		}
		if rank >= 3 {
			out[ScopeExecute] = true
		}
		if rank >= 4 {
			out[ScopeAdminRead], out[ScopeAdminWrite] = true, true
		}
	}
	list := make([]string, 0, len(out))
	for _, s := range []string{ScopeRead, ScopeAttachmentRead, ScopeWrite, ScopeAttachmentWrite, ScopeExecute,
		ScopeAIInvoke, ScopeAdminRead, ScopeAdminWrite} {
		if out[s] {
			list = append(list, s)
		}
	}
	return list
}
