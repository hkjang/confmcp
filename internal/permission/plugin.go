package permission

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hkjang/confmcp/internal/settings"
)

// Plugin REST paths. These are the confmcp plugin's own resources, not part
// of Confluence's REST API.
const (
	PluginCheckPath  = "/rest/confmcp/1.0/permissions/check"
	PluginBatchPath  = "/rest/confmcp/1.0/permissions/batch"
	PluginHealthPath = "/rest/confmcp/1.0/health"
	PluginUserPath   = "/rest/confmcp/1.0/users"
	maxPluginBatch   = 100
)

// PluginClient talks to the confmcp permission plugin inside Confluence.
//
// Every request is signed: HMAC-SHA256 over method, path, body hash,
// timestamp and a one-time nonce, so the plugin can reject anything not sent
// by this gateway and replays of what was. Ordinary Confluence users therefore
// cannot ask the plugin about somebody else's user key.
type PluginClient struct {
	store *settings.Store

	mu   sync.Mutex
	key  string
	http *http.Client
}

// NewPluginClient builds the client.
func NewPluginClient(store *settings.Store) *PluginClient {
	return &PluginClient{store: store}
}

// Reset drops the cached HTTP client.
func (p *PluginClient) Reset() {
	p.mu.Lock()
	p.http, p.key = nil, ""
	p.mu.Unlock()
}

func (p *PluginClient) client(cfg settings.Permission, insecure bool) *http.Client {
	key := fmt.Sprintf("%d|%t", cfg.TimeoutSec, insecure)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.key == key && p.http != nil {
		return p.http
	}
	timeout := time.Duration(cfg.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if insecure {
		tlsCfg.InsecureSkipVerify = true // #nosec G402 - operator opt-in for internal CAs
	}
	p.http = &http.Client{
		Timeout:       timeout,
		Transport:     &http.Transport{Proxy: http.ProxyFromEnvironment, TLSClientConfig: tlsCfg},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	p.key = key
	return p.http
}

// PluginResult is one target's answer from the plugin.
type PluginResult struct {
	Target  Target            `json:"target"`
	Results map[string]string `json:"results"`
	Reasons map[string]string `json:"reasons"`
	Content *struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Status   string `json:"status"`
		SpaceKey string `json:"spaceKey"`
	} `json:"content"`
	Space *struct {
		Key string `json:"key"`
	} `json:"space"`
	Error string `json:"error"`
}

type pluginBatchRequest struct {
	UserKey       string  `json:"userKey"`
	Checks        []Check `json:"checks"`
	CorrelationID string  `json:"correlationId"`
}

type pluginBatchResponse struct {
	UserKey     string         `json:"userKey"`
	UserStatus  string         `json:"userStatus"`
	EvaluatedAt string         `json:"evaluatedAt"`
	Results     []PluginResult `json:"results"`
}

// Batch asks the plugin about several targets at once, splitting long lists.
func (p *PluginClient) Batch(ctx context.Context, subj Subject, checks []Check, correlationID string) ([]Decision, error) {
	out := make([]Decision, 0, len(checks))
	for start := 0; start < len(checks); start += maxPluginBatch {
		end := start + maxPluginBatch
		if end > len(checks) {
			end = len(checks)
		}
		part, err := p.batch(ctx, subj, checks[start:end], correlationID)
		if err != nil {
			return nil, err
		}
		out = append(out, part...)
	}
	return out, nil
}

func (p *PluginClient) batch(ctx context.Context, subj Subject, checks []Check, correlationID string) ([]Decision, error) {
	var resp pluginBatchResponse
	if err := p.post(ctx, PluginBatchPath, pluginBatchRequest{
		UserKey: subj.UserKey, Checks: checks, CorrelationID: correlationID,
	}, &resp); err != nil {
		return nil, err
	}
	if !strings.EqualFold(resp.UserKey, subj.UserKey) {
		return nil, errors.New("권한 플러그인이 다른 사용자에 대해 응답했습니다")
	}
	if resp.UserStatus != "" && !strings.EqualFold(resp.UserStatus, "active") {
		// A deactivated or unlicensed account may do nothing, whatever its
		// stored grants say.
		decs := make([]Decision, 0, len(checks))
		for _, c := range checks {
			d := unknownDecision(c.Target, c.Ops, "plugin", "USER_"+strings.ToUpper(resp.UserStatus))
			for _, op := range c.Ops {
				d.Results[op] = Deny
			}
			decs = append(decs, d)
		}
		return decs, nil
	}
	if len(resp.Results) != len(checks) {
		return nil, fmt.Errorf("권한 플러그인 응답 건수가 맞지 않습니다 (%d/%d)", len(resp.Results), len(checks))
	}
	evaluated, _ := time.Parse(time.RFC3339, resp.EvaluatedAt)
	if evaluated.IsZero() {
		evaluated = time.Now().UTC()
	}
	decs := make([]Decision, 0, len(checks))
	for i, c := range checks {
		r := resp.Results[i]
		if r.Target.key() != c.Target.key() && !(c.Target.Kind == "space" && strings.EqualFold(r.Target.SpaceKey, c.Target.SpaceKey)) {
			return nil, errors.New("권한 플러그인 응답 순서가 요청과 다릅니다")
		}
		d := Decision{Target: c.Target, Results: map[Op]string{}, Reasons: map[Op]string{},
			Source: "plugin", EvaluatedAt: evaluated}
		for _, op := range c.Ops {
			v := strings.ToLower(r.Results[string(op)])
			switch v {
			case Allow, Deny:
			default:
				v = Unknown
			}
			d.Results[op] = v
			if reason := r.Reasons[string(op)]; reason != "" {
				d.Reasons[op] = reason
			} else if v == Unknown && r.Error != "" {
				d.Reasons[op] = r.Error
			}
		}
		if r.Content != nil {
			d.SpaceKey, d.ContentType, d.Status = r.Content.SpaceKey, r.Content.Type, r.Content.Status
		} else if r.Space != nil {
			d.SpaceKey = r.Space.Key
		}
		decs = append(decs, d)
	}
	return decs, nil
}

// PluginUser is the plugin's view of a Confluence account.
type PluginUser struct {
	UserKey     string `json:"userKey"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Email       string `json:"email"`
	Status      string `json:"status"`
	Directory   string `json:"directory"`
	Duplicates  int    `json:"duplicates"`
}

// User looks up an account by username or key, including whether it is
// active, which the 7.2.0 REST user resource does not report.
func (p *PluginClient) User(ctx context.Context, username, key string) (*PluginUser, error) {
	var out PluginUser
	if err := p.post(ctx, PluginUserPath, map[string]string{"username": username, "userKey": key}, &out); err != nil {
		return nil, err
	}
	if out.UserKey == "" {
		return nil, errors.New("권한 플러그인이 사용자를 찾지 못했습니다")
	}
	return &out, nil
}

// PluginHealth is the plugin's health answer.
type PluginHealth struct {
	Status            string `json:"status"`
	PluginVersion     string `json:"pluginVersion"`
	ConfluenceVersion string `json:"confluenceVersion"`
	BuildNumber       string `json:"buildNumber"`
}

// Health pings the plugin with a signed request.
func (p *PluginClient) Health(ctx context.Context) (*PluginHealth, error) {
	var out PluginHealth
	if err := p.do(ctx, http.MethodGet, PluginHealthPath, nil, &out); err != nil {
		return nil, err
	}
	if !strings.EqualFold(out.Status, "ok") {
		return &out, fmt.Errorf("권한 플러그인 상태: %s", out.Status)
	}
	return &out, nil
}

func (p *PluginClient) post(ctx context.Context, path string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return p.do(ctx, http.MethodPost, path, body, out)
}

// Sign computes the request signature shared with the plugin.
func Sign(secret, method, path string, body []byte, ts, nonce string) string {
	sum := sha256.Sum256(body)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strings.ToUpper(method) + "\n" + path + "\n" + hex.EncodeToString(sum[:]) + "\n" + ts + "\n" + nonce))
	return hex.EncodeToString(mac.Sum(nil))
}

func newNonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (p *PluginClient) do(ctx context.Context, method, path string, body []byte, out any) error {
	cfg, err := p.store.Permission(ctx)
	if err != nil {
		return err
	}
	conf, _ := p.store.Confluence(ctx)
	base := strings.TrimRight(strings.TrimSpace(cfg.PluginBaseURL), "/")
	if base == "" {
		base = strings.TrimRight(strings.TrimSpace(conf.BaseURL), "/")
	}
	if base == "" {
		return errors.New("권한 플러그인 URL(또는 Confluence 기본 URL)이 설정되지 않았습니다")
	}
	secret, err := p.store.Reveal(cfg.PluginSecretEnc)
	if err != nil {
		return err
	}
	if secret == "" {
		return errors.New("권한 플러그인 공유 비밀이 설정되지 않았습니다")
	}
	u, err := url.Parse(base + path)
	if err != nil {
		return err
	}
	if body == nil {
		body = []byte{}
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := newNonce()
	// The plugin sees the full request path, context path included.
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Atlassian-Token", "no-check")
	req.Header.Set("X-Confmcp-Timestamp", ts)
	req.Header.Set("X-Confmcp-Nonce", nonce)
	req.Header.Set("X-Confmcp-Signature", Sign(secret, method, u.EscapedPath(), body, ts, nonce))

	resp, err := p.client(cfg, conf.InsecureSkipTLS).Do(req)
	if err != nil {
		return fmt.Errorf("권한 플러그인 호출 실패: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 300 {
			msg = msg[:300]
		}
		if resp.StatusCode == http.StatusNotFound {
			return fmt.Errorf("권한 플러그인이 설치되지 않았거나 경로가 다릅니다 (404 %s)", path)
		}
		return fmt.Errorf("권한 플러그인 %d: %s", resp.StatusCode, msg)
	}
	if ct := resp.Header.Get("Content-Type"); strings.Contains(strings.ToLower(ct), "text/html") {
		return errors.New("권한 플러그인이 JSON 대신 HTML 을 반환했습니다 (로그인 화면 또는 프록시)")
	}
	return json.Unmarshal(raw, out)
}
