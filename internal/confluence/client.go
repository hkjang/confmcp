package confluence

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/hkjang/confmcp/internal/settings"
)

// Client is the Confluence Server 7.2.0 REST adapter.
type Client struct {
	cfg  settings.Confluence
	http *http.Client
	base *url.URL
}

var _ Adapter = (*Client)(nil)

// NewClient builds a REST client from settings. The base URL may carry a
// context path such as https://wiki.example.internal/confluence.
func NewClient(cfg settings.Confluence) (*Client, error) {
	raw := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if raw == "" {
		return nil, errors.New("Confluence 기본 URL이 설정되지 않았습니다")
	}
	base, err := url.Parse(raw)
	if err != nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") {
		return nil, fmt.Errorf("Confluence 기본 URL 형식이 올바르지 않습니다: %s", raw)
	}
	if cfg.TimeoutSec <= 0 {
		cfg.TimeoutSec = 15
	}
	tr := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		MaxIdleConnsPerHost: 16,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
	}
	if cfg.InsecureSkipTLS {
		tr.TLSClientConfig.InsecureSkipVerify = true // #nosec G402 - operator opt-in for internal CAs
	}
	return &Client{
		cfg:  cfg,
		base: base,
		http: &http.Client{
			Transport: tr,
			Timeout:   time.Duration(cfg.TimeoutSec) * time.Second,
			// A REST call is never redirected: a 3xx here is an SSO or login
			// redirect, and following it would hand the Basic credential to
			// whatever host the redirect names.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

// Config exposes the settings the client was built with.
func (c *Client) Config() settings.Confluence { return c.cfg }

// WebURL joins a Confluence web path (as returned in _links.webui) onto the
// configured base, refusing anything that is not a path on this server.
func (c *Client) WebURL(path string) string {
	if path == "" || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return ""
	}
	return strings.TrimRight(c.base.String(), "/") + path
}

// sameOrigin reports whether a link Confluence returned stays on this server
// and under its context path.
func (c *Client) sameOrigin(raw string) (string, bool) {
	// Links in a 7.2.0 response are relative to the context path.
	if strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, "//") {
		return strings.TrimRight(c.base.String(), "/") + raw, true
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	if !strings.EqualFold(u.Scheme, c.base.Scheme) || !strings.EqualFold(u.Host, c.base.Host) {
		return "", false
	}
	if !strings.HasPrefix(u.Path, strings.TrimRight(c.base.Path, "/")+"/") {
		return "", false
	}
	return u.String(), true
}

type request struct {
	method      string
	path        string // below the base URL, e.g. /rest/api/content
	query       url.Values
	body        []byte
	contentType string
	headers     map[string]string
}

// do performs a request. GETs are retried a few times on 429/502/503/504,
// honouring Retry-After; writes are never retried, and a transport failure on
// a write is reported as ErrOutcomeUnknown because the write may have landed.
func (c *Client) do(ctx context.Context, cred Credential, rq request) ([]byte, http.Header, error) {
	if cred.Username == "" || cred.Password == "" {
		return nil, nil, errors.New("Confluence 자격증명이 없습니다")
	}
	target := strings.TrimRight(c.base.String(), "/") + rq.path
	if len(rq.query) > 0 {
		target += "?" + rq.query.Encode()
	}
	attempts := 1
	if rq.method == http.MethodGet {
		attempts = 3
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		var body io.Reader
		if rq.body != nil {
			body = bytes.NewReader(rq.body)
		}
		req, err := http.NewRequestWithContext(ctx, rq.method, target, body)
		if err != nil {
			return nil, nil, err
		}
		req.SetBasicAuth(cred.Username, cred.Password)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("X-Atlassian-Token", "no-check")
		if rq.contentType != "" {
			req.Header.Set("Content-Type", rq.contentType)
		}
		for k, v := range rq.headers {
			req.Header.Set(k, v)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			if rq.method != http.MethodGet {
				return nil, nil, fmt.Errorf("%w: Confluence 쓰기 요청의 응답을 받지 못했습니다: %v", ErrOutcomeUnknown, err)
			}
			lastErr = fmt.Errorf("Confluence 요청 실패: %w", err)
			if isTimeout(err) && i < attempts-1 {
				continue
			}
			return nil, nil, lastErr
		}
		raw, rerr := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		resp.Body.Close()
		if rerr != nil {
			if rq.method != http.MethodGet {
				return nil, nil, fmt.Errorf("%w: 응답 본문을 끝까지 읽지 못했습니다: %v", ErrOutcomeUnknown, rerr)
			}
			return nil, nil, rerr
		}
		switch {
		case resp.StatusCode >= 300 && resp.StatusCode < 400:
			return nil, nil, fmt.Errorf("%w: Confluence 가 로그인/SSO 리다이렉트(%d)로 응답했습니다. 서비스 계정 Basic 인증이 허용되는지 확인하십시오",
				ErrAuthPage, resp.StatusCode)
		case resp.StatusCode == http.StatusUnauthorized:
			return nil, nil, fmt.Errorf("%w: Confluence 인증 실패(401). 자격증명을 확인하십시오. 반복 시도하면 계정이 잠길 수 있습니다", ErrAuthPage)
		case resp.StatusCode == 429 || resp.StatusCode == 502 || resp.StatusCode == 503 || resp.StatusCode == 504:
			lastErr = parseAPIError(resp.StatusCode, raw)
			if rq.method == http.MethodGet && i < attempts-1 {
				if !sleepRetry(ctx, resp.Header.Get("Retry-After"), i) {
					return nil, nil, ctx.Err()
				}
				continue
			}
			return nil, nil, lastErr
		case resp.StatusCode >= 400:
			return nil, nil, parseAPIError(resp.StatusCode, raw)
		}
		if looksLikeHTML(resp.Header.Get("Content-Type"), raw) && !strings.HasPrefix(rq.path, "/download/") {
			return nil, nil, fmt.Errorf("%w: Confluence 가 API 응답 대신 HTML(로그인 화면)을 반환했습니다", ErrAuthPage)
		}
		return raw, resp.Header, nil
	}
	return nil, nil, lastErr
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func sleepRetry(ctx context.Context, retryAfter string, attempt int) bool {
	wait := time.Duration(300*(attempt+1)) * time.Millisecond
	if s, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && s > 0 {
		wait = time.Duration(s) * time.Second
	}
	if wait > 5*time.Second {
		wait = 5 * time.Second
	}
	select {
	case <-ctx.Done():
		return false
	case <-time.After(wait):
		return true
	}
}

func looksLikeHTML(contentType string, raw []byte) bool {
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "text/html") {
		return true
	}
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == '<' && !strings.Contains(ct, "xml")
}

func parseAPIError(status int, raw []byte) error {
	var env struct {
		Message    string `json:"message"`
		StatusCode int    `json:"statusCode"`
		Data       struct {
			Errors []struct {
				Message struct {
					Key string `json:"key"`
				} `json:"message"`
			} `json:"errors"`
		} `json:"data"`
	}
	e := &APIError{Status: status}
	if json.Unmarshal(raw, &env) == nil && env.Message != "" {
		e.Message = truncate(env.Message, 500)
	}
	if e.Message == "" {
		e.Message = http.StatusText(status)
	}
	return e
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func (c *Client) getJSON(ctx context.Context, cred Credential, path string, q url.Values, out any) error {
	raw, _, err := c.do(ctx, cred, request{method: http.MethodGet, path: path, query: q})
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("Confluence 응답을 해석할 수 없습니다: %w", err)
	}
	return nil
}

func (c *Client) sendJSON(ctx context.Context, cred Credential, method, path string, q url.Values, in, out any) error {
	var body []byte
	if in != nil {
		var err error
		if body, err = json.Marshal(in); err != nil {
			return err
		}
	}
	raw, _, err := c.do(ctx, cred, request{method: method, path: path, query: q, body: body, contentType: "application/json"})
	if err != nil {
		return err
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		// The write itself succeeded; only the answer is unreadable.
		return fmt.Errorf("%w: 쓰기 응답을 해석할 수 없습니다: %v", ErrOutcomeUnknown, err)
	}
	return nil
}

func paging(start, limit int) url.Values {
	q := url.Values{}
	if start > 0 {
		q.Set("start", strconv.Itoa(start))
	}
	if limit <= 0 {
		limit = 25
	}
	q.Set("limit", strconv.Itoa(limit))
	return q
}

// ---------- wire shapes ----------

type rawList[T any] struct {
	Results []T `json:"results"`
	Start   int `json:"start"`
	Limit   int `json:"limit"`
	Size    int `json:"size"`
	Links   struct {
		Next string `json:"next"`
	} `json:"_links"`
}

func (l rawList[T]) page() Page {
	return Page{Start: l.Start, Limit: l.Limit, Size: l.Size, HasNext: l.Links.Next != ""}
}

type rawUser struct {
	Type        string `json:"type"`
	Username    string `json:"username"`
	UserKey     string `json:"userKey"`
	DisplayName string `json:"displayName"`
	Email       string `json:"email"`
}

func (u rawUser) user() *User {
	return &User{Type: u.Type, Username: u.Username, UserKey: u.UserKey, DisplayName: u.DisplayName, Email: u.Email}
}

type rawSpace struct {
	ID          int64  `json:"id"`
	Key         string `json:"key"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Status      string `json:"status"`
	Description struct {
		Plain struct {
			Value string `json:"value"`
		} `json:"plain"`
	} `json:"description"`
	Homepage *struct {
		ID    string `json:"id"`
		Type  string `json:"type"`
		Title string `json:"title"`
	} `json:"homepage"`
	Links struct {
		WebUI string `json:"webui"`
	} `json:"_links"`
}

func (c *Client) space(s rawSpace) Space {
	out := Space{ID: s.ID, Key: s.Key, Name: s.Name, Type: s.Type, Status: s.Status,
		Description: s.Description.Plain.Value, WebURL: c.WebURL(s.Links.WebUI)}
	if s.Homepage != nil && s.Homepage.ID != "" {
		out.Homepage = &Ref{ID: s.Homepage.ID, Type: s.Homepage.Type, Title: s.Homepage.Title}
	}
	return out
}

type rawContent struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Status string `json:"status"`
	Title  string `json:"title"`
	Space  *struct {
		Key  string `json:"key"`
		Name string `json:"name"`
	} `json:"space"`
	Version *struct {
		Number    int    `json:"number"`
		When      string `json:"when"`
		Message   string `json:"message"`
		MinorEdit bool   `json:"minorEdit"`
		By        *rawUser `json:"by"`
	} `json:"version"`
	History *struct {
		CreatedDate string   `json:"createdDate"`
		CreatedBy   *rawUser `json:"createdBy"`
	} `json:"history"`
	Ancestors []struct {
		ID    string `json:"id"`
		Type  string `json:"type"`
		Title string `json:"title"`
	} `json:"ancestors"`
	Container *struct {
		ID    any    `json:"id"`
		Type  string `json:"type"`
		Title string `json:"title"`
	} `json:"container"`
	Body *struct {
		Storage *struct {
			Value string `json:"value"`
		} `json:"storage"`
	} `json:"body"`
	Metadata *struct {
		MediaType string `json:"mediaType"`
		Comment   string `json:"comment"`
		Labels    *struct {
			Results []Label `json:"results"`
		} `json:"labels"`
	} `json:"metadata"`
	Extensions *struct {
		MediaType string `json:"mediaType"`
		FileSize  int64  `json:"fileSize"`
		Comment   string `json:"comment"`
	} `json:"extensions"`
	Links struct {
		WebUI    string `json:"webui"`
		Download string `json:"download"`
	} `json:"_links"`
}

func (r rawContent) content() Content {
	out := Content{ID: r.ID, Type: r.Type, Status: r.Status, Title: r.Title, WebPath: r.Links.WebUI,
		DownloadPath: r.Links.Download}
	if r.Space != nil {
		out.SpaceKey, out.SpaceName = r.Space.Key, r.Space.Name
	}
	if r.Version != nil {
		out.Version = Version{Number: r.Version.Number, When: r.Version.When,
			Message: r.Version.Message, MinorEdit: r.Version.MinorEdit}
		if r.Version.By != nil {
			out.Version.By, out.Version.ByKey = r.Version.By.DisplayName, r.Version.By.UserKey
		}
	}
	if r.History != nil {
		out.CreatedAt = r.History.CreatedDate
		if r.History.CreatedBy != nil {
			out.CreatedBy = r.History.CreatedBy.DisplayName
		}
	}
	for _, a := range r.Ancestors {
		out.Ancestors = append(out.Ancestors, Ref{ID: a.ID, Type: a.Type, Title: a.Title})
	}
	if r.Container != nil {
		out.Container = &Ref{ID: fmt.Sprint(r.Container.ID), Type: r.Container.Type, Title: r.Container.Title}
	}
	if r.Body != nil && r.Body.Storage != nil {
		out.Storage, out.HasBody = r.Body.Storage.Value, true
	}
	if r.Metadata != nil {
		out.MediaType, out.Comment = r.Metadata.MediaType, r.Metadata.Comment
		if r.Metadata.Labels != nil {
			for _, l := range r.Metadata.Labels.Results {
				out.Labels = append(out.Labels, l.Name)
			}
		}
	}
	if r.Extensions != nil {
		if r.Extensions.MediaType != "" {
			out.MediaType = r.Extensions.MediaType
		}
		out.FileSize = r.Extensions.FileSize
		if r.Extensions.Comment != "" {
			out.Comment = r.Extensions.Comment
		}
	}
	// A reply's ancestors are the comments it answers, nearest last.
	if r.Type == "comment" && len(r.Ancestors) > 0 {
		out.ParentCommentID = r.Ancestors[len(r.Ancestors)-1].ID
	}
	return out
}

func contents(in []rawContent) []Content {
	out := make([]Content, 0, len(in))
	for _, r := range in {
		out = append(out, r.content())
	}
	return out
}

// ---------- identity ----------

// CurrentUser returns the user the credential belongs to. In service mode
// that is the service account, never the requester.
func (c *Client) CurrentUser(ctx context.Context, cred Credential) (*User, error) {
	var u rawUser
	if err := c.getJSON(ctx, cred, "/rest/api/user/current", nil, &u); err != nil {
		return nil, err
	}
	if u.Type == "anonymous" || u.UserKey == "" {
		return nil, fmt.Errorf("%w: 익명 사용자로 응답했습니다", ErrAuthPage)
	}
	return u.user(), nil
}

// UserByUsername resolves a username to its user key.
func (c *Client) UserByUsername(ctx context.Context, cred Credential, username string) (*User, error) {
	var u rawUser
	if err := c.getJSON(ctx, cred, "/rest/api/user", url.Values{"username": {username}}, &u); err != nil {
		return nil, err
	}
	if u.UserKey == "" {
		return nil, &APIError{Status: 404, Message: "사용자를 찾을 수 없습니다"}
	}
	return u.user(), nil
}

// UserByKey resolves a user key.
func (c *Client) UserByKey(ctx context.Context, cred Credential, key string) (*User, error) {
	var u rawUser
	if err := c.getJSON(ctx, cred, "/rest/api/user", url.Values{"key": {key}}, &u); err != nil {
		return nil, err
	}
	if u.UserKey == "" {
		return nil, &APIError{Status: 404, Message: "사용자를 찾을 수 없습니다"}
	}
	return u.user(), nil
}

// ---------- spaces ----------

// Spaces lists spaces visible to the credential.
func (c *Client) Spaces(ctx context.Context, cred Credential, start, limit int) ([]Space, Page, error) {
	q := paging(start, limit)
	q.Set("expand", "description.plain,homepage")
	var l rawList[rawSpace]
	if err := c.getJSON(ctx, cred, "/rest/api/space", q, &l); err != nil {
		return nil, Page{}, err
	}
	out := make([]Space, 0, len(l.Results))
	for _, s := range l.Results {
		out = append(out, c.space(s))
	}
	return out, l.page(), nil
}

// Space returns one space.
func (c *Client) Space(ctx context.Context, cred Credential, key string) (*Space, error) {
	var s rawSpace
	q := url.Values{"expand": {"description.plain,homepage"}}
	if err := c.getJSON(ctx, cred, "/rest/api/space/"+url.PathEscape(key), q, &s); err != nil {
		return nil, err
	}
	out := c.space(s)
	return &out, nil
}

// ---------- content ----------

const listExpand = "space,version,ancestors"

// ContentList lists pages or blog posts.
func (c *Client) ContentList(ctx context.Context, cred Credential, cq ContentQuery) ([]Content, Page, error) {
	q := paging(cq.Start, cq.Limit)
	if cq.Type != "" {
		q.Set("type", cq.Type)
	}
	if cq.SpaceKey != "" {
		q.Set("spaceKey", cq.SpaceKey)
	}
	if cq.Title != "" {
		q.Set("title", cq.Title)
	}
	expand := listExpand
	if len(cq.Expand) > 0 {
		expand = strings.Join(cq.Expand, ",")
	}
	q.Set("expand", expand)
	var l rawList[rawContent]
	if err := c.getJSON(ctx, cred, "/rest/api/content", q, &l); err != nil {
		return nil, Page{}, err
	}
	return contents(l.Results), l.page(), nil
}

// Content reads one piece of content. Only the expansions asked for are
// requested; body.view is never asked for, because it renders macros with the
// credential's (possibly wider) authority.
func (c *Client) Content(ctx context.Context, cred Credential, id string, expand ...string) (*Content, error) {
	if len(expand) == 0 {
		expand = []string{"space", "version", "ancestors"}
	}
	for _, e := range expand {
		if strings.HasPrefix(e, "body.view") || strings.HasPrefix(e, "body.export_view") || strings.HasPrefix(e, "body.styled_view") {
			return nil, errors.New("렌더링된 본문(body.view)은 요청할 수 없습니다")
		}
	}
	var r rawContent
	q := url.Values{"expand": {strings.Join(expand, ",")}}
	if err := c.getJSON(ctx, cred, "/rest/api/content/"+url.PathEscape(id), q, &r); err != nil {
		return nil, err
	}
	out := r.content()
	return &out, nil
}

// Search runs a CQL query built by the caller.
func (c *Client) Search(ctx context.Context, cred Credential, cql string, start, limit int) ([]Content, Page, error) {
	q := paging(start, limit)
	q.Set("cql", cql)
	q.Set("expand", listExpand)
	var l rawList[rawContent]
	if err := c.getJSON(ctx, cred, "/rest/api/content/search", q, &l); err != nil {
		return nil, Page{}, err
	}
	return contents(l.Results), l.page(), nil
}

func (c *Client) child(ctx context.Context, cred Credential, id, kind, expand string, start, limit int, extra url.Values) ([]Content, Page, error) {
	q := paging(start, limit)
	q.Set("expand", expand)
	for k, v := range extra {
		q[k] = v
	}
	var l rawList[rawContent]
	if err := c.getJSON(ctx, cred, "/rest/api/content/"+url.PathEscape(id)+"/child/"+kind, q, &l); err != nil {
		return nil, Page{}, err
	}
	return contents(l.Results), l.page(), nil
}

// Children lists the direct child pages. The full descendant tree is walked by
// the caller under depth and node limits; /descendant/page is not assumed.
func (c *Client) Children(ctx context.Context, cred Credential, id string, start, limit int) ([]Content, Page, error) {
	return c.child(ctx, cred, id, "page", listExpand, start, limit, nil)
}

// Comments lists the comments on a page, replies included.
func (c *Client) Comments(ctx context.Context, cred Credential, id string, start, limit int) ([]Content, Page, error) {
	return c.child(ctx, cred, id, "comment", "body.storage,version,ancestors,container,history",
		start, limit, url.Values{"depth": {"all"}})
}

// Attachments lists attachment metadata.
func (c *Client) Attachments(ctx context.Context, cred Credential, id string, start, limit int) ([]Content, Page, error) {
	return c.child(ctx, cred, id, "attachment", "version,container,metadata", start, limit, nil)
}

// Labels lists content labels.
func (c *Client) Labels(ctx context.Context, cred Credential, id string, start, limit int) ([]Label, Page, error) {
	var l rawList[Label]
	if err := c.getJSON(ctx, cred, "/rest/api/content/"+url.PathEscape(id)+"/label", paging(start, limit), &l); err != nil {
		return nil, Page{}, err
	}
	return l.Results, l.page(), nil
}

// Operations returns what the credential's own user may do on a space or a
// piece of content. Only a delegated credential's answer says anything about
// the requester.
func (c *Client) Operations(ctx context.Context, cred Credential, kind, id string) ([]Operation, error) {
	var env struct {
		Operations []Operation `json:"operations"`
	}
	path := "/rest/api/content/" + url.PathEscape(id)
	if kind == "space" {
		path = "/rest/api/space/" + url.PathEscape(id)
	}
	if err := c.getJSON(ctx, cred, path, url.Values{"expand": {"operations"}}, &env); err != nil {
		return nil, err
	}
	if env.Operations == nil {
		return nil, errors.New("operations 확장을 지원하지 않는 응답입니다")
	}
	return env.Operations, nil
}

// ---------- writes ----------

type storageBody struct {
	Storage struct {
		Value          string `json:"value"`
		Representation string `json:"representation"`
	} `json:"storage"`
}

func newBody(v string) storageBody {
	var b storageBody
	b.Storage.Value, b.Storage.Representation = v, "storage"
	return b
}

// CreateContent creates a page, blog post or comment.
func (c *Client) CreateContent(ctx context.Context, cred Credential, in NewContent) (*Content, error) {
	payload := map[string]any{
		"type": in.Type,
		"body": newBody(in.Storage),
	}
	switch in.Type {
	case "comment":
		ctype := in.ContainerType
		if ctype == "" {
			ctype = "page"
		}
		payload["container"] = map[string]any{"id": in.ContainerID, "type": ctype}
		if in.SpaceKey != "" {
			payload["space"] = map[string]any{"key": in.SpaceKey}
		}
		if in.ParentID != "" {
			payload["ancestors"] = []map[string]any{{"id": in.ParentID}}
		}
	default:
		payload["title"] = in.Title
		payload["space"] = map[string]any{"key": in.SpaceKey}
		if in.ParentID != "" {
			payload["ancestors"] = []map[string]any{{"id": in.ParentID}}
		}
	}
	var r rawContent
	if err := c.sendJSON(ctx, cred, http.MethodPost, "/rest/api/content",
		url.Values{"expand": {"space,version,ancestors"}}, payload, &r); err != nil {
		return nil, err
	}
	out := r.content()
	if out.ID == "" {
		return nil, fmt.Errorf("%w: 생성 응답에 ID가 없습니다", ErrOutcomeUnknown)
	}
	return &out, nil
}

// UpdateContent writes a new version. The caller supplies NewVersion as the
// current version plus one; Confluence answers 409 if it moved meanwhile.
func (c *Client) UpdateContent(ctx context.Context, cred Credential, id string, in ContentUpdate) (*Content, error) {
	payload := map[string]any{
		"id":      id,
		"type":    in.Type,
		"title":   in.Title,
		"version": map[string]any{"number": in.NewVersion, "message": in.VersionNote},
		"body":    newBody(in.Storage),
	}
	if in.ParentID != "" {
		payload["ancestors"] = []map[string]any{{"id": in.ParentID}}
	}
	var r rawContent
	if err := c.sendJSON(ctx, cred, http.MethodPut, "/rest/api/content/"+url.PathEscape(id),
		url.Values{"expand": {"space,version,ancestors"}}, payload, &r); err != nil {
		return nil, err
	}
	out := r.content()
	return &out, nil
}

// AddLabels adds global labels.
func (c *Client) AddLabels(ctx context.Context, cred Credential, id string, names []string) ([]Label, error) {
	payload := make([]map[string]string, 0, len(names))
	for _, n := range names {
		payload = append(payload, map[string]string{"prefix": "global", "name": n})
	}
	var l rawList[Label]
	if err := c.sendJSON(ctx, cred, http.MethodPost, "/rest/api/content/"+url.PathEscape(id)+"/label", nil, payload, &l); err != nil {
		return nil, err
	}
	return l.Results, nil
}

// RemoveLabel removes one label, passing the name as a query parameter so
// that special characters survive.
func (c *Client) RemoveLabel(ctx context.Context, cred Credential, id, name string) error {
	return c.sendJSON(ctx, cred, http.MethodDelete, "/rest/api/content/"+url.PathEscape(id)+"/label",
		url.Values{"name": {name}}, nil, nil)
}

// TrashContent moves current content to the trash. Purging trashed content is
// not offered: the status=trashed form of the call is never sent.
func (c *Client) TrashContent(ctx context.Context, cred Credential, id string) error {
	return c.sendJSON(ctx, cred, http.MethodDelete, "/rest/api/content/"+url.PathEscape(id), nil, nil, nil)
}

// UploadAttachment attaches a new file to a page.
func (c *Client) UploadAttachment(ctx context.Context, cred Credential, pageID, filename, mediaType string, data []byte, comment string) (*Content, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, escapeQuotes(filename)))
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	h.Set("Content-Type", mediaType)
	part, err := mw.CreatePart(h)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(data); err != nil {
		return nil, err
	}
	if comment != "" {
		_ = mw.WriteField("comment", comment)
	}
	_ = mw.WriteField("minorEdit", "true")
	if err := mw.Close(); err != nil {
		return nil, err
	}
	raw, _, err := c.do(ctx, cred, request{
		method: http.MethodPost, path: "/rest/api/content/" + url.PathEscape(pageID) + "/child/attachment",
		body: buf.Bytes(), contentType: mw.FormDataContentType(),
		headers: map[string]string{"X-Atlassian-Token": "nocheck"},
	})
	if err != nil {
		return nil, err
	}
	var l rawList[rawContent]
	if err := json.Unmarshal(raw, &l); err != nil || len(l.Results) == 0 {
		return nil, fmt.Errorf("%w: 첨부 응답을 해석할 수 없습니다", ErrOutcomeUnknown)
	}
	out := l.Results[0].content()
	return &out, nil
}

func escapeQuotes(s string) string {
	return strings.NewReplacer("\\", "\\\\", `"`, "\\\"", "\r", "", "\n", "").Replace(s)
}

// Download streams an attachment. The download link Confluence returned must
// stay on this server; redirects are refused so the credential never leaves.
func (c *Client) Download(ctx context.Context, cred Credential, att *Content, maxBytes int64) (io.ReadCloser, string, error) {
	link, ok := c.sameOrigin(att.DownloadPath)
	if !ok || att.DownloadPath == "" {
		return nil, "", errors.New("첨부 다운로드 링크가 이 Confluence 서버를 가리키지 않습니다")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, "", err
	}
	req.SetBasicAuth(cred.Username, cred.Password)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("첨부 다운로드 실패: %w", err)
	}
	if resp.StatusCode >= 300 {
		resp.Body.Close()
		if resp.StatusCode < 400 {
			return nil, "", fmt.Errorf("%w: 첨부 다운로드가 리다이렉트되었습니다", ErrAuthPage)
		}
		return nil, "", &APIError{Status: resp.StatusCode, Message: "첨부를 내려받을 수 없습니다"}
	}
	if maxBytes > 0 && resp.ContentLength > maxBytes {
		resp.Body.Close()
		return nil, "", fmt.Errorf("첨부 크기(%d)가 허용 한도(%d)를 넘습니다", resp.ContentLength, maxBytes)
	}
	var body io.ReadCloser = resp.Body
	if maxBytes > 0 {
		body = limitedReadCloser{io.LimitReader(resp.Body, maxBytes), resp.Body}
	}
	return body, resp.Header.Get("Content-Type"), nil
}

type limitedReadCloser struct {
	io.Reader
	io.Closer
}

// ---------- health ----------

// Probe checks connectivity and records which 7.2.0 features answer. Nothing
// unverified is reported as available.
func (c *Client) Probe(ctx context.Context, cred Credential) (*ServerInfo, error) {
	info := &ServerInfo{BaseURL: c.base.String(), Features: map[string]bool{}}
	u, err := c.CurrentUser(ctx, cred)
	if err != nil {
		return nil, err
	}
	info.User = u
	if raw, _, err := c.do(ctx, cred, request{method: http.MethodGet, path: "/rest/applinks/1.0/manifest",
		headers: map[string]string{"Accept": "application/xml"}}); err == nil {
		var m struct {
			Version     string `xml:"version"`
			BuildNumber string `xml:"buildNumber"`
		}
		if xml.Unmarshal(raw, &m) == nil {
			info.Version, info.BuildNumber = m.Version, m.BuildNumber
		}
	}
	_, _, err = c.Spaces(ctx, cred, 0, 1)
	info.Features["spaces"] = err == nil
	_, _, err = c.Search(ctx, cred, `type=page`, 0, 1)
	info.Features["cql"] = err == nil
	_, _, err = c.ContentList(ctx, cred, ContentQuery{Type: "page", Limit: 1})
	info.Features["content"] = err == nil
	return info, nil
}
