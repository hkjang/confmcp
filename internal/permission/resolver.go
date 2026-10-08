package permission

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/hkjang/confmcp/internal/confluence"
	"github.com/hkjang/confmcp/internal/settings"
)

// Resolver decides operations for a requester through the configured mode.
type Resolver struct {
	store    *settings.Store
	provider *confluence.Provider
	plugin   *PluginClient

	mu    sync.Mutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	dec Decision
	exp time.Time
}

// NewResolver builds the resolver.
func NewResolver(store *settings.Store, provider *confluence.Provider) *Resolver {
	return &Resolver{store: store, provider: provider, plugin: NewPluginClient(store), cache: map[string]cacheEntry{}}
}

// Plugin exposes the plugin client for health checks and user lookups.
func (r *Resolver) Plugin() *PluginClient { return r.plugin }

// Reset clears the cache and plugin client after a settings or policy change.
func (r *Resolver) Reset() {
	r.mu.Lock()
	r.cache = map[string]cacheEntry{}
	r.mu.Unlock()
	r.plugin.Reset()
}

// Mode returns the configured permission mode.
func (r *Resolver) Mode(ctx context.Context) string {
	cfg, err := r.store.Permission(ctx)
	if err != nil {
		return ""
	}
	return cfg.Mode
}

// Check decides ops on one target.
func (r *Resolver) Check(ctx context.Context, subj Subject, target Target, ops ...Op) (Decision, error) {
	decs, err := r.Batch(ctx, subj, []Check{{Target: target, Ops: ops}})
	if err != nil {
		return Decision{}, err
	}
	return decs[0], nil
}

// Batch decides several targets. An error means nothing could be decided;
// individual Unknown verdicts are returned in the decisions and must be
// treated as denials by the caller.
func (r *Resolver) Batch(ctx context.Context, subj Subject, checks []Check) ([]Decision, error) {
	if len(checks) == 0 {
		return []Decision{}, nil
	}
	if strings.TrimSpace(subj.UserKey) == "" {
		return nil, fmt.Errorf("%w: 요청자의 Confluence userKey 가 확인되지 않았습니다", ErrUnavailable)
	}
	cfg, err := r.store.Permission(ctx)
	if err != nil {
		return nil, err
	}
	ttl := time.Duration(cfg.CacheTTLSec) * time.Second

	out := make([]Decision, len(checks))
	missing := []int{}
	for i, c := range checks {
		if d, ok := r.cached(subj, c, ttl); ok {
			out[i] = d
			continue
		}
		missing = append(missing, i)
	}
	if len(missing) == 0 {
		return out, nil
	}
	todo := make([]Check, 0, len(missing))
	for _, i := range missing {
		todo = append(todo, checks[i])
	}

	var decs []Decision
	switch cfg.Mode {
	case "plugin":
		decs, err = r.plugin.Batch(ctx, subj, todo, "")
	case "delegated":
		decs, err = r.delegated(ctx, subj, todo)
	default:
		err = fmt.Errorf("알 수 없는 권한 판정 모드입니다: %q", cfg.Mode)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	for j, i := range missing {
		out[i] = decs[j]
		r.remember(subj, checks[i], decs[j], ttl)
	}
	return out, nil
}

func cacheKey(subj Subject, c Check) string {
	ops := make([]string, 0, len(c.Ops))
	for _, op := range c.Ops {
		ops = append(ops, string(op))
	}
	return subj.UserKey + "|" + c.Target.key() + "|" + strings.Join(ops, ",")
}

func (r *Resolver) cached(subj Subject, c Check, ttl time.Duration) (Decision, bool) {
	if ttl <= 0 {
		return Decision{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.cache[cacheKey(subj, c)]
	if !ok || time.Now().After(e.exp) {
		return Decision{}, false
	}
	return e.dec, true
}

func (r *Resolver) remember(subj Subject, c Check, d Decision, ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	// Only definite answers are cached; an Unknown is asked again next time.
	for _, v := range d.Results {
		if v == Unknown {
			return
		}
	}
	r.mu.Lock()
	r.cache[cacheKey(subj, c)] = cacheEntry{dec: d, exp: time.Now().Add(ttl)}
	r.mu.Unlock()
}

// delegated answers by calling Confluence with the requester's own
// credential: what Confluence lets that credential do is the answer.
func (r *Resolver) delegated(ctx context.Context, subj Subject, checks []Check) ([]Decision, error) {
	cred, key, err := r.provider.UserCredential(ctx, subj.UserID)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(key, subj.UserKey) {
		return nil, errors.New("연결된 Confluence 자격증명이 매핑된 사용자와 다릅니다")
	}
	adapter, _, err := r.provider.Adapter(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Decision, 0, len(checks))
	for _, c := range checks {
		out = append(out, r.delegatedOne(ctx, adapter, cred, c))
	}
	return out, nil
}

func (r *Resolver) delegatedOne(ctx context.Context, adapter confluence.Adapter, cred confluence.Credential, c Check) Decision {
	d := Decision{Target: c.Target, Results: map[Op]string{}, Reasons: map[Op]string{},
		Source: "delegated", EvaluatedAt: time.Now().UTC()}
	denyAll := func(reason string) Decision {
		for _, op := range c.Ops {
			d.Results[op], d.Reasons[op] = Deny, reason
		}
		return d
	}
	unknownAll := func(reason string) Decision {
		for _, op := range c.Ops {
			d.Results[op], d.Reasons[op] = Unknown, reason
		}
		return d
	}

	kind, id := "content", c.Target.ID
	if c.Target.Kind == "space" {
		kind, id = "space", c.Target.SpaceKey
		sp, err := adapter.Space(ctx, cred, id)
		if err != nil {
			if confluence.IsStatus(err, 403, 404) {
				return denyAll("NOT_VISIBLE")
			}
			return unknownAll(err.Error())
		}
		d.SpaceKey = sp.Key
	} else {
		content, err := adapter.Content(ctx, cred, id, "space", "version")
		if err != nil {
			if confluence.IsStatus(err, 403, 404) {
				return denyAll("NOT_VISIBLE")
			}
			return unknownAll(err.Error())
		}
		d.SpaceKey, d.ContentType, d.Status = content.SpaceKey, content.Type, content.Status
	}

	needOps := false
	for _, op := range c.Ops {
		if op == OpRead {
			d.Results[op] = Allow
			continue
		}
		needOps = true
	}
	if !needOps {
		return d
	}
	ops, err := adapter.Operations(ctx, cred, kind, id)
	if err != nil {
		for _, op := range c.Ops {
			if op != OpRead {
				d.Results[op], d.Reasons[op] = Unknown, "OPERATIONS_UNAVAILABLE"
			}
		}
		return d
	}
	has := func(operation string, targets ...string) bool {
		for _, o := range ops {
			if !strings.EqualFold(o.Operation, operation) {
				continue
			}
			for _, t := range targets {
				if strings.EqualFold(o.TargetType, t) {
					return true
				}
			}
		}
		return false
	}
	for _, op := range c.Ops {
		if op == OpRead {
			continue
		}
		var ok, known bool
		switch op {
		case OpCreate:
			ok, known = has("create", "page", "blogpost"), true
		case OpEdit:
			ok, known = has("update", "page", "blogpost", "comment"), kind == "content"
		case OpComment:
			ok, known = has("create", "comment"), true
		case OpAttach:
			ok, known = has("create", "attachment"), true
		case OpDelete:
			ok, known = has("delete", "page", "blogpost", "comment", "attachment"), kind == "content"
		case OpMove:
			// 7.2.0 does not report a move operation reliably; moving needs edit
			// on the page plus create under the new parent, checked separately.
			ok, known = has("update", "page"), kind == "content"
		}
		switch {
		case !known:
			d.Results[op], d.Reasons[op] = Unknown, "OPERATION_NOT_REPORTED"
		case ok:
			d.Results[op] = Allow
		default:
			d.Results[op], d.Reasons[op] = Deny, "NOT_PERMITTED"
		}
	}
	return d
}

// Health reports whether the configured mode can answer at all.
func (r *Resolver) Health(ctx context.Context) (map[string]any, error) {
	cfg, err := r.store.Permission(ctx)
	if err != nil {
		return nil, err
	}
	switch cfg.Mode {
	case "plugin":
		h, err := r.plugin.Health(ctx)
		if err != nil {
			return map[string]any{"mode": "plugin"}, err
		}
		return map[string]any{"mode": "plugin", "plugin": h}, nil
	case "delegated":
		conf, _ := r.store.Confluence(ctx)
		if !conf.AllowUserCredential {
			return map[string]any{"mode": "delegated"}, errors.New("사용자 위임 모드인데 사용자 자격증명 연결이 허용되지 않았습니다")
		}
		return map[string]any{"mode": "delegated", "detail": "사용자별 위임 자격증명으로 판정합니다"}, nil
	default:
		return nil, fmt.Errorf("알 수 없는 권한 판정 모드입니다: %q", cfg.Mode)
	}
}
