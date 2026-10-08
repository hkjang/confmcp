// Package permission answers "may this requester do this operation on this
// Confluence space or content?".
//
// Confluence has no single READ < WRITE < ADMIN ladder: viewing, editing,
// commenting, attaching, moving and deleting are decided separately, and a
// page's view restriction is inherited by its children while its edit
// restriction is not. So every operation is asked for on its own, and only two
// sources may answer:
//
//   - plugin: the confmcp permission plugin inside Confluence asks Confluence's
//     own permission engine on behalf of the requester's user key.
//   - delegated: the call runs with the requester's own Confluence credential,
//     so Confluence itself enforces the answer.
//
// The service account's own view of restrictions or `operations` is never
// used as the requester's permission. Anything that cannot be decided is
// Unknown, and Unknown is a denial.
package permission

import (
	"errors"
	"strings"
	"time"
)

// Op is one Confluence operation, decided independently of the others.
type Op string

// Operations confmcp asks about.
const (
	OpRead    Op = "read"
	OpCreate  Op = "create"  // create a page or blog post in a space / under a parent
	OpEdit    Op = "edit"    // update body or title, add/remove labels
	OpComment Op = "comment" // add a comment
	OpAttach  Op = "attach"  // add an attachment
	OpMove    Op = "move"
	OpDelete  Op = "delete" // move to trash
)

// AllOps lists every operation in a stable order.
var AllOps = []Op{OpRead, OpCreate, OpEdit, OpComment, OpAttach, OpMove, OpDelete}

// ParseOp maps a tool's required permission string onto an operation.
func ParseOp(s string) (Op, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || s == "none" {
		return "", false
	}
	for _, op := range AllOps {
		if string(op) == s {
			return op, true
		}
	}
	return OpRead, true
}

// Verdict values.
const (
	Allow   = "allow"
	Deny    = "deny"
	Unknown = "unknown"
)

// Target names what is being checked. Kind is "space" or "content".
type Target struct {
	Kind     string `json:"kind"`
	SpaceKey string `json:"spaceKey,omitempty"`
	ID       string `json:"id,omitempty"`
}

// SpaceTarget builds a space target.
func SpaceTarget(key string) Target { return Target{Kind: "space", SpaceKey: key} }

// ContentTarget builds a content target.
func ContentTarget(id string) Target { return Target{Kind: "content", ID: id} }

func (t Target) key() string {
	if t.Kind == "space" {
		return "space:" + strings.ToUpper(t.SpaceKey)
	}
	return "content:" + t.ID
}

// Subject is the requester, as the server verified them. Nothing a caller
// sends as an argument can change it.
type Subject struct {
	UserID   int64  `json:"-"`
	UserKey  string `json:"userKey"`
	Username string `json:"username"`
}

// Decision is the answer for one target.
type Decision struct {
	Target      Target            `json:"target"`
	Results     map[Op]string     `json:"results"`
	Reasons     map[Op]string     `json:"reasons,omitempty"`
	SpaceKey    string            `json:"spaceKey,omitempty"`
	ContentType string            `json:"contentType,omitempty"`
	Status      string            `json:"status,omitempty"`
	Source      string            `json:"source"`
	EvaluatedAt time.Time         `json:"evaluatedAt"`
}

// Allowed reports whether op was explicitly allowed.
func (d Decision) Allowed(op Op) bool { return d.Results[op] == Allow }

// Verdict returns the verdict for op, Unknown when it was not answered.
func (d Decision) Verdict(op Op) string {
	if v, ok := d.Results[op]; ok {
		return v
	}
	return Unknown
}

// Check is one target with the operations wanted on it.
type Check struct {
	Target Target `json:"target"`
	Ops    []Op   `json:"operations"`
}

// ErrUnavailable means effective permission could not be determined.
var ErrUnavailable = errors.New("PERMISSION_UNKNOWN")

// unknownDecision answers every op with Unknown.
func unknownDecision(t Target, ops []Op, source, reason string) Decision {
	d := Decision{Target: t, Results: map[Op]string{}, Reasons: map[Op]string{}, Source: source, EvaluatedAt: time.Now().UTC()}
	for _, op := range ops {
		d.Results[op] = Unknown
		d.Reasons[op] = reason
	}
	return d
}
