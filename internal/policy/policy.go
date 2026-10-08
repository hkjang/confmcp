// Package policy narrows what MCP may touch on top of Confluence's own
// permissions. A user can never gain access here, only lose it.
//
// Semantics:
//   - space: only spaces matched by an allow rule are reachable at all
//     (explicit allow). A matching deny always wins.
//   - page_tree: a deny rule blocks a page and every descendant. Allow rules
//     scoped to a space turn that space into "only these subtrees".
//   - content_type: page, blogpost, comment, attachment. Deny blocks the type;
//     if any allow rule exists, the type must match one.
//
// Labels are deliberately not a policy target: ordinary editors can change
// them, so they cannot be a security boundary.
package policy

import (
	"context"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Rule kinds and effects.
const (
	KindSpace       = "space"
	KindPageTree    = "page_tree"
	KindContentType = "content_type"

	EffectAllow = "allow"
	EffectDeny  = "deny"
)

// Rule is one ACL entry.
type Rule struct {
	ID        int64     `json:"id"`
	Kind      string    `json:"kind"`
	Pattern   string    `json:"pattern"`
	SpaceKey  string    `json:"spaceKey,omitempty"`
	Effect    string    `json:"effect"`
	RiskCap   string    `json:"riskCap,omitempty"`
	Priority  int       `json:"priority"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Target is what a verdict is asked for.
type Target struct {
	SpaceKey    string   `json:"spaceKey"`
	ContentID   string   `json:"contentId,omitempty"`
	Ancestors   []string `json:"ancestors,omitempty"`
	ContentType string   `json:"contentType,omitempty"`
}

// Verdict is the outcome of evaluating a target against the ACL.
type Verdict struct {
	Allowed    bool   `json:"allowed"`
	RiskCap    string `json:"riskCap,omitempty"`
	MatchedBy  string `json:"matchedBy,omitempty"`
	Reason     string `json:"reason,omitempty"`
	Generation int64  `json:"generation"`
}

// Engine evaluates ACL rules with a short-lived cache.
type Engine struct {
	pool *pgxpool.Pool

	mu     sync.RWMutex
	rules  []Rule
	gen    int64
	loaded time.Time
	ttl    time.Duration
}

// NewEngine builds the engine.
func NewEngine(pool *pgxpool.Pool) *Engine {
	return &Engine{pool: pool, ttl: 5 * time.Second}
}

// Reset forces a reload on the next evaluation.
func (e *Engine) Reset() {
	e.mu.Lock()
	e.loaded = time.Time{}
	e.mu.Unlock()
}

func (e *Engine) load(ctx context.Context) ([]Rule, int64, error) {
	e.mu.RLock()
	if !e.loaded.IsZero() && time.Since(e.loaded) < e.ttl {
		rules, gen := e.rules, e.gen
		e.mu.RUnlock()
		return rules, gen, nil
	}
	e.mu.RUnlock()

	var gen int64
	if err := e.pool.QueryRow(ctx, `SELECT generation FROM policy_state WHERE id=1`).Scan(&gen); err != nil {
		return nil, 0, err
	}
	rows, err := e.pool.Query(ctx, `
		SELECT id, kind, pattern, space_key, effect, COALESCE(risk_cap,''), priority, note, created_at, updated_at
		FROM policy_rules ORDER BY priority ASC, id ASC`)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Rule{}
	for rows.Next() {
		var r Rule
		if err := rows.Scan(&r.ID, &r.Kind, &r.Pattern, &r.SpaceKey, &r.Effect, &r.RiskCap,
			&r.Priority, &r.Note, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	e.mu.Lock()
	e.rules, e.gen, e.loaded = out, gen, time.Now()
	e.mu.Unlock()
	return out, gen, nil
}

// Rules returns every rule, highest priority first.
func (e *Engine) Rules(ctx context.Context) ([]Rule, error) {
	rules, _, err := e.load(ctx)
	return rules, err
}

// Generation returns the current policy generation.
func (e *Engine) Generation(ctx context.Context) (int64, error) {
	_, gen, err := e.load(ctx)
	return gen, err
}

func (e *Engine) bump(ctx context.Context) {
	_, _ = e.pool.Exec(ctx, `UPDATE policy_state SET generation = generation + 1, updated_at = NOW() WHERE id=1`)
	e.Reset()
}

// Create inserts a rule.
func (e *Engine) Create(ctx context.Context, r Rule) (int64, error) {
	var id int64
	err := e.pool.QueryRow(ctx, `
		INSERT INTO policy_rules(kind, pattern, space_key, effect, risk_cap, priority, note)
		VALUES ($1,$2,$3,$4,NULLIF($5,''),$6,$7) RETURNING id`,
		r.Kind, strings.TrimSpace(r.Pattern), strings.ToUpper(strings.TrimSpace(r.SpaceKey)),
		r.Effect, r.RiskCap, r.Priority, r.Note).Scan(&id)
	e.bump(ctx)
	return id, err
}

// Update replaces a rule.
func (e *Engine) Update(ctx context.Context, r Rule) error {
	_, err := e.pool.Exec(ctx, `
		UPDATE policy_rules SET kind=$2, pattern=$3, space_key=$4, effect=$5,
		       risk_cap=NULLIF($6,''), priority=$7, note=$8, updated_at=NOW() WHERE id=$1`,
		r.ID, r.Kind, strings.TrimSpace(r.Pattern), strings.ToUpper(strings.TrimSpace(r.SpaceKey)),
		r.Effect, r.RiskCap, r.Priority, r.Note)
	e.bump(ctx)
	return err
}

// Delete removes a rule.
func (e *Engine) Delete(ctx context.Context, id int64) error {
	_, err := e.pool.Exec(ctx, `DELETE FROM policy_rules WHERE id=$1`, id)
	e.bump(ctx)
	return err
}

// Evaluate checks a target against the ACL.
func (e *Engine) Evaluate(ctx context.Context, t Target) (Verdict, error) {
	rules, gen, err := e.load(ctx)
	if err != nil {
		return Verdict{}, err
	}
	v := evaluate(rules, t)
	v.Generation = gen
	return v, nil
}

// SpaceAllowed is a cheap pre-check used to narrow searches and listings.
func (e *Engine) SpaceAllowed(ctx context.Context, spaceKey string) bool {
	v, err := e.Evaluate(ctx, Target{SpaceKey: spaceKey})
	return err == nil && v.Allowed
}

// AllowedSpaceKeys returns the exact space keys allowed by literal (non-glob)
// allow rules, and whether any glob allow rule exists. Search uses it to put
// a space restriction into the CQL itself when it can.
func (e *Engine) AllowedSpaceKeys(ctx context.Context) ([]string, bool, error) {
	rules, _, err := e.load(ctx)
	if err != nil {
		return nil, false, err
	}
	keys := []string{}
	glob := false
	for _, r := range rules {
		if r.Kind != KindSpace || r.Effect != EffectAllow {
			continue
		}
		if strings.ContainsAny(r.Pattern, "*?[") {
			glob = true
			continue
		}
		keys = append(keys, strings.ToUpper(strings.TrimSpace(r.Pattern)))
	}
	return keys, glob, nil
}

func evaluate(rules []Rule, t Target) Verdict {
	space := strings.ToUpper(strings.TrimSpace(t.SpaceKey))
	if space == "" {
		return Verdict{Allowed: false, Reason: "대상 공간을 확인할 수 없습니다"}
	}
	riskCap := ""
	tighten := func(c string) {
		if c != "" && (riskCap == "" || riskRank(c) < riskRank(riskCap)) {
			riskCap = c
		}
	}

	// Spaces: explicit allow, deny wins.
	matchedAllow := false
	for _, r := range rules {
		if r.Kind != KindSpace || !matchPattern(r.Pattern, space) {
			continue
		}
		if r.Effect == EffectDeny {
			return Verdict{Allowed: false, MatchedBy: "space:" + r.Pattern, Reason: denyReason(r)}
		}
		matchedAllow = true
		tighten(r.RiskCap)
	}
	if !matchedAllow {
		return Verdict{Allowed: false, MatchedBy: "space:허용목록",
			Reason: "MCP 접근이 허용되지 않은 공간입니다: " + space}
	}

	// Page trees.
	if t.ContentID != "" || len(t.Ancestors) > 0 {
		chain := append(append([]string{}, t.Ancestors...), t.ContentID)
		hasScopedAllow, inAllowedTree := false, false
		for _, r := range rules {
			if r.Kind != KindPageTree {
				continue
			}
			if r.SpaceKey != "" && !strings.EqualFold(r.SpaceKey, space) {
				continue
			}
			inTree := contains(chain, strings.TrimSpace(r.Pattern))
			if r.Effect == EffectDeny {
				if inTree {
					return Verdict{Allowed: false, MatchedBy: "page_tree:" + r.Pattern, Reason: denyReason(r)}
				}
				continue
			}
			if r.SpaceKey != "" {
				hasScopedAllow = true
			}
			if inTree {
				inAllowedTree = true
				tighten(r.RiskCap)
			}
		}
		if hasScopedAllow && !inAllowedTree {
			return Verdict{Allowed: false, MatchedBy: "page_tree:허용목록",
				Reason: "이 공간에서는 허용된 페이지 트리 아래의 문서만 접근할 수 있습니다"}
		}
	}

	// Content types.
	if ct := strings.ToLower(strings.TrimSpace(t.ContentType)); ct != "" {
		hasAllow, matched := false, false
		for _, r := range rules {
			if r.Kind != KindContentType {
				continue
			}
			hit := strings.EqualFold(strings.TrimSpace(r.Pattern), ct) || r.Pattern == "*"
			if r.Effect == EffectDeny {
				if hit {
					return Verdict{Allowed: false, MatchedBy: "content_type:" + r.Pattern, Reason: denyReason(r)}
				}
				continue
			}
			hasAllow = true
			if hit {
				matched = true
				tighten(r.RiskCap)
			}
		}
		if hasAllow && !matched {
			return Verdict{Allowed: false, MatchedBy: "content_type:허용목록",
				Reason: "MCP 접근이 허용되지 않은 콘텐츠 유형입니다: " + ct}
		}
	}
	return Verdict{Allowed: true, RiskCap: riskCap}
}

func contains(list []string, v string) bool {
	if v == "" {
		return false
	}
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func denyReason(r Rule) string {
	if r.Note != "" {
		return "정책으로 차단됨: " + r.Note
	}
	return "정책으로 차단됨 (" + KoKind(r.Kind) + " " + r.Pattern + ")"
}

// KoKind is the Korean label of a rule kind.
func KoKind(kind string) string {
	switch kind {
	case KindSpace:
		return "공간"
	case KindPageTree:
		return "페이지 트리"
	case KindContentType:
		return "콘텐츠 유형"
	default:
		return kind
	}
}

// matchPattern supports "*", globs and exact matches, case-insensitively
// because Confluence space keys are case-insensitive.
func matchPattern(pattern, value string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}
	if pattern == "*" {
		return true
	}
	p, v := strings.ToUpper(pattern), strings.ToUpper(value)
	if p == v {
		return true
	}
	ok, err := path.Match(p, v)
	return err == nil && ok
}

func riskRank(level string) int {
	switch strings.ToUpper(level) {
	case "READ":
		return 1
	case "WRITE":
		return 2
	case "EXECUTE":
		return 3
	case "ADMIN":
		return 4
	default:
		return 99
	}
}

// RiskAllowed reports whether a tool's risk level fits within a cap.
func RiskAllowed(toolRisk, cap string) bool {
	if cap == "" {
		return true
	}
	return riskRank(toolRisk) <= riskRank(cap)
}
