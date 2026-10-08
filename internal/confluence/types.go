// Package confluence adapts Confluence Server 7.2.0 to the needs of confmcp.
//
// Every tool talks to the Adapter interface rather than to REST directly, so
// that a later Confluence version only needs a new Adapter implementation
// while the MCP tools, Keycloak integration and console stay unchanged. Only
// the Server /rest/api surface is used; Cloud v2 endpoints are never mixed in.
package confluence

import (
	"context"
	"errors"
	"io"
	"strings"
)

// Credential identifies which identity performs a REST call. Confluence 7.2.0
// has no personal access tokens, so both modes use HTTPS Basic.
type Credential struct {
	Mode     string // service | delegated
	Username string
	Password string
}

// Actor is how the credential is recorded in the audit log.
func (c Credential) Actor() string {
	if c.Mode == "" {
		return c.Username
	}
	return c.Mode + ":" + c.Username
}

// Page is the paging state of a Confluence list response.
type Page struct {
	Start   int  `json:"start"`
	Limit   int  `json:"limit"`
	Size    int  `json:"size"`
	HasNext bool `json:"hasNext"`
}

// User is a Confluence user. UserKey is the durable identifier: a username
// can be renamed, the key cannot.
type User struct {
	Type        string `json:"type,omitempty"`
	Username    string `json:"username"`
	UserKey     string `json:"userKey"`
	DisplayName string `json:"displayName"`
	Email       string `json:"email,omitempty"`
	// Status is reported by the permission plugin ("active", "deactivated").
	// The 7.2.0 REST user resource does not expose it.
	Status string `json:"status,omitempty"`
}

// Space is a Confluence space.
type Space struct {
	ID          int64    `json:"id"`
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Status      string   `json:"status,omitempty"`
	Description string   `json:"description,omitempty"`
	Homepage    *Ref     `json:"homepage,omitempty"`
	WebURL      string   `json:"webUrl,omitempty"`
	Labels      []string `json:"labels,omitempty"`
}

// Ref is a lightweight reference to a piece of content.
type Ref struct {
	ID    string `json:"id"`
	Type  string `json:"type,omitempty"`
	Title string `json:"title,omitempty"`
}

// Version is a content version stamp.
type Version struct {
	Number    int    `json:"number"`
	When      string `json:"when,omitempty"`
	By        string `json:"by,omitempty"`
	ByKey     string `json:"byKey,omitempty"`
	Message   string `json:"message,omitempty"`
	MinorEdit bool   `json:"minorEdit,omitempty"`
}

// Content is a page, blog post, comment or attachment.
type Content struct {
	ID        string   `json:"id"`
	Type      string   `json:"type"`
	Status    string   `json:"status"`
	Title     string   `json:"title"`
	SpaceKey  string   `json:"spaceKey,omitempty"`
	SpaceName string   `json:"spaceName,omitempty"`
	Version   Version  `json:"version"`
	Ancestors []Ref    `json:"ancestors,omitempty"`
	Container *Ref     `json:"container,omitempty"`
	Storage   string   `json:"-"`
	HasBody   bool     `json:"-"`
	Labels    []string `json:"labels,omitempty"`
	CreatedAt string   `json:"createdAt,omitempty"`
	CreatedBy string   `json:"createdBy,omitempty"`
	WebPath   string   `json:"-"`
	// Attachment metadata.
	MediaType    string `json:"mediaType,omitempty"`
	FileSize     int64  `json:"fileSize,omitempty"`
	Comment      string `json:"comment,omitempty"`
	DownloadPath string `json:"-"`
	// Comment threading: the comment this one replies to, when known.
	ParentCommentID string `json:"parentCommentId,omitempty"`
}

// AncestorIDs lists the ancestor content IDs, root first.
func (c *Content) AncestorIDs() []string {
	out := make([]string, 0, len(c.Ancestors))
	for _, a := range c.Ancestors {
		out = append(out, a.ID)
	}
	return out
}

// Label is a content label.
type Label struct {
	Prefix string `json:"prefix"`
	Name   string `json:"name"`
	ID     string `json:"id,omitempty"`
}

// ContentQuery filters GET /rest/api/content.
type ContentQuery struct {
	Type     string // page | blogpost
	SpaceKey string
	Title    string
	Start    int
	Limit    int
	Expand   []string
}

// NewContent creates a page, blog post or comment.
type NewContent struct {
	Type        string // page | blogpost | comment
	Title       string
	SpaceKey    string
	ParentID    string // page parent (ancestors) or comment container
	ContainerID string // comment container page
	ContainerType string
	Storage     string
}

// ContentUpdate replaces the body (and optionally title or parent) of content.
type ContentUpdate struct {
	Type          string
	Title         string
	NewVersion    int
	Storage       string
	ParentID      string // set for a move
	VersionNote   string
	KeepBody      bool // a move keeps the current body; Storage is still sent
}

// ServerInfo is what a connection probe found.
type ServerInfo struct {
	Version     string `json:"version,omitempty"`
	BuildNumber string `json:"buildNumber,omitempty"`
	BaseURL     string `json:"baseUrl"`
	User        *User  `json:"user,omitempty"`
	Features    map[string]bool `json:"features"`
}

// Adapter is the Confluence capability surface confmcp depends on.
type Adapter interface {
	// Identity
	CurrentUser(ctx context.Context, cred Credential) (*User, error)
	UserByUsername(ctx context.Context, cred Credential, username string) (*User, error)
	UserByKey(ctx context.Context, cred Credential, key string) (*User, error)

	// Spaces
	Spaces(ctx context.Context, cred Credential, start, limit int) ([]Space, Page, error)
	Space(ctx context.Context, cred Credential, key string) (*Space, error)

	// Content
	ContentList(ctx context.Context, cred Credential, q ContentQuery) ([]Content, Page, error)
	Content(ctx context.Context, cred Credential, id string, expand ...string) (*Content, error)
	Search(ctx context.Context, cred Credential, cql string, start, limit int) ([]Content, Page, error)
	Children(ctx context.Context, cred Credential, id string, start, limit int) ([]Content, Page, error)
	Comments(ctx context.Context, cred Credential, id string, start, limit int) ([]Content, Page, error)
	Attachments(ctx context.Context, cred Credential, id string, start, limit int) ([]Content, Page, error)
	Labels(ctx context.Context, cred Credential, id string, start, limit int) ([]Label, Page, error)
	Operations(ctx context.Context, cred Credential, kind, id string) ([]Operation, error)

	// Writes
	CreateContent(ctx context.Context, cred Credential, in NewContent) (*Content, error)
	UpdateContent(ctx context.Context, cred Credential, id string, in ContentUpdate) (*Content, error)
	AddLabels(ctx context.Context, cred Credential, id string, names []string) ([]Label, error)
	RemoveLabel(ctx context.Context, cred Credential, id, name string) error
	TrashContent(ctx context.Context, cred Credential, id string) error
	UploadAttachment(ctx context.Context, cred Credential, pageID, filename, mediaType string, data []byte, comment string) (*Content, error)
	Download(ctx context.Context, cred Credential, att *Content, maxBytes int64) (io.ReadCloser, string, error)

	// Health
	Probe(ctx context.Context, cred Credential) (*ServerInfo, error)
	WebURL(path string) string
}

// Operation is one entry of an `operations` expansion: what the credential's
// own user may do. It is only meaningful for a delegated credential.
type Operation struct {
	Operation  string `json:"operation"`
	TargetType string `json:"targetType"`
}

// Errors that callers classify.
var (
	// ErrOutcomeUnknown means a write may or may not have reached Confluence.
	ErrOutcomeUnknown = errors.New("OUTCOME_UNKNOWN")
	// ErrAuthPage means Confluence answered with a login page or an SSO
	// redirect instead of an API response.
	ErrAuthPage = errors.New("UPSTREAM_AUTH_FAILED")
	// ErrNoServiceCredential means the admin has not configured the service account.
	ErrNoServiceCredential = errors.New("Confluence 서비스 계정이 설정되지 않았습니다")
	// ErrNoUserCredential means the user has not connected their own account.
	ErrNoUserCredential = errors.New("연결된 Confluence 사용자 자격증명이 없습니다")
)

// APIError carries a Confluence REST failure.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return "Confluence " + itoa(e.Status) + ": " + e.Message
	}
	return "Confluence " + itoa(e.Status)
}

// IsStatus reports whether err is an APIError with one of the statuses.
func IsStatus(err error, statuses ...int) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	for _, s := range statuses {
		if apiErr.Status == s {
			return true
		}
	}
	return false
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// CQLString quotes a value for a CQL string literal.
func CQLString(v string) string {
	var sb strings.Builder
	sb.WriteByte('"')
	for _, r := range v {
		switch r {
		case '"', '\\':
			sb.WriteByte('\\')
			sb.WriteRune(r)
		case '\n', '\r', '\t':
			sb.WriteByte(' ')
		default:
			if r < 0x20 {
				continue
			}
			sb.WriteRune(r)
		}
	}
	sb.WriteByte('"')
	return sb.String()
}
