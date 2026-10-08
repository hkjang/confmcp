package tools

import (
	"context"
	"strings"

	"github.com/hkjang/confmcp/internal/approval"
	"github.com/hkjang/confmcp/internal/confluence"
	"github.com/hkjang/confmcp/internal/permission"
	"github.com/hkjang/confmcp/internal/policy"
	"github.com/hkjang/confmcp/internal/settings"
)

// Principal is the authenticated caller of a tool.
type Principal struct {
	UserID             int64    `json:"userId"`
	KeycloakIssuer     string   `json:"keycloakIssuer,omitempty"`
	KeycloakSub        string   `json:"keycloakSub"`
	Username           string   `json:"username"`
	DisplayName        string   `json:"displayName"`
	Roles              []string `json:"roles"`
	Scopes             []string `json:"scopes"`
	IsServiceAdmin     bool     `json:"isServiceAdmin"`
	ConfluenceUserKey  string   `json:"confluenceUserKey"`
	ConfluenceUsername string   `json:"confluenceUsername"`
	AuthMode           string   `json:"authMode"` // oauth | apikey | session
	Client             string   `json:"client"`
	IP                 string   `json:"ip"`
	RequestID          string   `json:"requestId,omitempty"`
}

// Subject is the permission subject for this principal.
func (p Principal) Subject() permission.Subject {
	return permission.Subject{UserID: p.UserID, UserKey: p.ConfluenceUserKey, Username: p.ConfluenceUsername}
}

// Target names the Confluence object a call acts on, as the caller asked.
// The executor never trusts SpaceKey for content: it resolves the real space
// and ancestors from the content ID.
type Target struct {
	SpaceKey  string
	ContentID string
	ParentID  string
}

// Resolved is the server-side view of a target after checks.
type Resolved struct {
	SpaceKey    string
	ContentID   string
	ContentType string
	Ancestors   []string
	Content     *confluence.Content
	Decision    permission.Decision
	Verdict     policy.Verdict
}

// Call carries everything a tool handler needs.
type Call struct {
	Args      Args
	Principal Principal
	Adapter   confluence.Adapter
	Cred      confluence.Credential
	Cfg       settings.Confluence
	Limits    settings.Limits
	MCP       settings.MCP
	Target    Target
	Resolved  Resolved
	exec      *Executor
}

// Plan is a write prepared but not yet sent: the exact change, what it is
// bound to, and the preview an approver reads.
type Plan struct {
	Action        string
	TargetID      string
	SpaceKey      string
	BaseVersion   *int
	BaseHash      string
	Preview       approval.Preview
	// Apply sends the write. It returns the ID of the content to verify.
	Apply func(ctx context.Context) (verifyID string, result any, err error)
	// Verify optionally checks the written result; it returns the version seen.
	Verify func(ctx context.Context, id string) (int, error)
}

// Definition is one MCP tool.
type Definition struct {
	Name            string
	Title           string
	Description     string
	Group           string
	Risk            string
	RequiredPerm    string // permission.Op or "NONE"
	Scope           string
	Priority        string // P0 | P1 | P2
	DefaultEnabled  bool
	DefaultApproval bool
	MinRole         string
	HighLevel       bool
	InputSchema     map[string]any
	Resolve         func(Args) Target
	Handle          func(context.Context, *Call) (any, error)
	Prepare         func(context.Context, *Call) (*Plan, error)
}

// IsWrite reports whether the tool changes Confluence.
func (d Definition) IsWrite() bool { return d.Prepare != nil }

// Result wraps a tool payload with provenance so that a model consuming it can
// tell document content is untrusted input, not instructions.
type Result struct {
	OK         bool     `json:"ok"`
	Data       any      `json:"data"`
	Sources    []Source `json:"sources,omitempty"`
	Pagination *PageOut `json:"pagination,omitempty"`
	Warnings   []string `json:"warnings,omitempty"`
	Trust      string   `json:"trust"`
	RequestID  string   `json:"requestId,omitempty"`
	ExecutedAs string   `json:"executedAs,omitempty"`
}

// Source describes one piece of Confluence content a result came from.
type Source struct {
	ContentID string `json:"contentId,omitempty"`
	Type      string `json:"type,omitempty"`
	Title     string `json:"title,omitempty"`
	URL       string `json:"url,omitempty"`
	SpaceKey  string `json:"spaceKey,omitempty"`
	Version   int    `json:"version,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

// PageOut is the paging envelope returned to clients. Only the number of
// items actually returned is reported: a total over allowed items is not
// known, and the upstream total would count hidden content.
type PageOut struct {
	Returned   int    `json:"returned"`
	Limit      int    `json:"limit"`
	NextCursor string `json:"nextCursor,omitempty"`
	HasMore    bool   `json:"hasMore"`
}

func (c *Call) result(data any, sources []Source, page *PageOut, warnings ...string) Result {
	return Result{OK: true, Data: data, Sources: sources, Pagination: page, Warnings: nonEmpty(warnings),
		Trust: "untrusted", RequestID: c.Principal.RequestID, ExecutedAs: c.Cred.Actor()}
}

func nonEmpty(in []string) []string {
	out := []string{}
	for _, s := range in {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// source builds the provenance of one content item.
func (c *Call) source(ct *confluence.Content) Source {
	return Source{ContentID: ct.ID, Type: ct.Type, Title: ct.Title, URL: c.Adapter.WebURL(ct.WebPath),
		SpaceKey: ct.SpaceKey, Version: ct.Version.Number, UpdatedAt: ct.Version.When}
}

// limit clamps a requested list size to the configured bounds.
func (c *Call) limit() int {
	n := c.Args.OptInt("limit", c.Limits.ListDefault)
	if n <= 0 {
		n = c.Limits.ListDefault
	}
	if n > c.Limits.ListMax {
		n = c.Limits.ListMax
	}
	return n
}
