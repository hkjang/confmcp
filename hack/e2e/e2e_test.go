//go:build e2e

// Package e2e runs the acceptance scenarios of the requirements against a
// running confmcp with the mock Confluence (scripts/dev.sh up). These are mock
// verifications; the real Confluence 7.2.0 checks are listed in the admin guide.
//
//	CONFMCP_URL=http://127.0.0.1:18088 MOCK_CONFLUENCE=http://127.0.0.1:18090/confluence \
//	  go test -tags e2e -count=1 -v ./hack/e2e
package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strings"
	"sync"
	"testing"
)

var (
	base = env("CONFMCP_URL", "http://127.0.0.1:18088")
	mock = env("MOCK_CONFLUENCE", "http://127.0.0.1:18090/confluence")
)

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

type session struct {
	t      *testing.T
	client *http.Client
	key    string
}

func login(t *testing.T, user, pass string) *session {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	s := &session{t: t, client: &http.Client{Jar: jar}}
	if code, body := s.do("POST", "/api/auth/login", map[string]string{"username": user, "password": pass}); code != 200 {
		t.Fatalf("login %s: %d %s", user, code, body)
	}
	return s
}

func (s *session) do(method, path string, body any) (int, string) {
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, base+path, r)
	req.Header.Set("Content-Type", "application/json")
	if s.key != "" {
		req.Header.Set("Authorization", "Bearer "+s.key)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(out)
}

func (s *session) apiKey() string {
	_, body := s.do("POST", "/api/me/keys", map[string]any{"name": "e2e", "role": "executor"})
	var issued struct{ Secret string }
	_ = json.Unmarshal([]byte(body), &issued)
	if issued.Secret == "" {
		s.t.Fatalf("no key: %s", body)
	}
	return issued.Secret
}

// tool calls an MCP tool with an API key and returns (isError, payload).
func tool(t *testing.T, key, name string, args map[string]any) (bool, map[string]any) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": args}})
	req, _ := http.NewRequest("POST", base+"/mcp", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var env struct {
		Result struct {
			IsError           bool           `json:"isError"`
			StructuredContent map[string]any `json:"structuredContent"`
		} `json:"result"`
		Error *struct{ Message string } `json:"error"`
	}
	raw, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(raw, &env); err != nil || env.Error != nil {
		t.Fatalf("%s: bad response %d %s", name, resp.StatusCode, raw)
	}
	return env.Result.IsError, env.Result.StructuredContent
}

func code(m map[string]any) string { s, _ := m["code"].(string); return s }

func dump(v any) string { b, _ := json.Marshal(v); return string(b) }

var (
	once                   sync.Once
	alice, bob, carol, adm *session
	aliceKey, bobKey       string
	carolKey               string
)

func setup(t *testing.T) {
	once.Do(func() {
		adm = login(t, "admin", "admin-password-1")
		alice = login(t, "alice", "alice-password-1")
		bob = login(t, "bob", "bob-password-12")
		carol = login(t, "carol", "carol-password1")
		aliceKey, bobKey, carolKey = alice.apiKey(), bob.apiKey(), carol.apiKey()
	})
}

func TestAC01_Authentication(t *testing.T) {
	setup(t)
	if isErr, out := tool(t, aliceKey, "confluence_me", nil); isErr || dump(out) == "" {
		t.Fatalf("valid key rejected: %v", out)
	}
	for _, bad := range []string{"confmcp_bogus_key", "cmcp_at_forged", "not-a-token"} {
		req, _ := http.NewRequest("POST", base+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		req.Header.Set("Authorization", "Bearer "+bad)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 401 || !strings.Contains(resp.Header.Get("WWW-Authenticate"), "resource_metadata") {
			t.Fatalf("%s: status %d", bad, resp.StatusCode)
		}
	}
}

func TestAC03_SearchDoesNotLeak(t *testing.T) {
	setup(t)
	_, out := tool(t, aliceKey, "confluence_search", map[string]any{"query": "대외비"})
	if s := dump(out); strings.Contains(s, "보안 점검") || strings.Contains(s, "65541") {
		t.Fatalf("restricted page leaked: %s", s)
	}
	_, out = tool(t, bobKey, "confluence_search", map[string]any{"query": "대외비"})
	if !strings.Contains(dump(out), "65541") {
		t.Fatalf("bob (allowed) should find it: %s", dump(out))
	}
}

func TestAC04_InheritedViewRestriction(t *testing.T) {
	setup(t)
	for _, c := range []struct{ tool, key, id string }{
		{"confluence_get_page", "pageId", "65541"},
		{"confluence_get_page", "pageId", "65542"},
		{"confluence_attachments", "pageId", "65541"},
		{"confluence_comments", "pageId", "65542"},
		{"confluence_download_attachment", "attachmentId", "att65581"},
		{"confluence_get_page", "pageId", "99999"},
	} {
		isErr, out := tool(t, aliceKey, c.tool, map[string]any{c.key: c.id})
		if !isErr || code(out) != "PERMISSION_DENIED" || !strings.Contains(dump(out), "찾을 수 없거나") {
			t.Fatalf("%s %s: %v", c.tool, c.id, out)
		}
	}
	_, out := tool(t, aliceKey, "confluence_children", map[string]any{"pageId": "65537"})
	if strings.Contains(dump(out), "65541") {
		t.Fatal("restricted child listed")
	}
}

func TestAC05_EditRestrictionNotInherited(t *testing.T) {
	setup(t)
	_, out := tool(t, aliceKey, "confluence_update_page", map[string]any{"pageId": "65544", "expectedVersion": 1, "mode": "append", "body": "x"})
	if code(out) != "PERMISSION_DENIED" {
		t.Fatalf("restricted parent: %v", out)
	}
	_, out = tool(t, aliceKey, "confluence_update_page", map[string]any{"pageId": "65545", "expectedVersion": 1, "mode": "append", "body": "x"})
	if code(out) != "APPROVAL_REQUIRED" {
		t.Fatalf("child should be editable (approval next): %v", out)
	}
}

func TestAC06_RequesterNotServiceAccount(t *testing.T) {
	setup(t)
	// alice only reads OPS; the service account could write there.
	_, out := tool(t, aliceKey, "confluence_update_page", map[string]any{"pageId": "65546", "expectedVersion": 3, "mode": "append", "body": "x"})
	if code(out) != "PERMISSION_DENIED" {
		t.Fatalf("requester without edit could write: %v", out)
	}
	_, out = tool(t, carolKey, "confluence_add_comment", map[string]any{"pageId": "65538", "body": "x"})
	if code(out) != "ROLE_DENIED" {
		t.Fatalf("reader role could write: %v", out)
	}
}

func TestAC07_PluginFailureBlocks(t *testing.T) {
	setup(t)
	put := func(secret string) {
		if c, b := adm.do("PUT", "/api/admin/settings/permission_plugin", map[string]any{
			"value":   map[string]any{"mode": "plugin", "cacheTtlSec": 0, "timeoutSec": 10},
			"secrets": map[string]string{"pluginSecret": secret}}); c != 200 {
			t.Fatalf("settings: %d %s", c, b)
		}
	}
	put("wrong-secret")
	defer put(env("MOCK_PLUGIN_SECRET", "mock-plugin-secret"))
	_, out := tool(t, aliceKey, "confluence_get_page", map[string]any{"pageId": "65538"})
	if code(out) != "PERMISSION_UNKNOWN" {
		t.Fatalf("plugin failure did not block: %v", out)
	}
	_, out = tool(t, aliceKey, "confluence_spaces", nil)
	if code(out) != "PERMISSION_UNKNOWN" && strings.Contains(dump(out), "DEV") {
		t.Fatalf("listing leaked without a permission source: %v", out)
	}
}

func TestAC08_IncludeMacroNotExpanded(t *testing.T) {
	setup(t)
	_, out := tool(t, aliceKey, "confluence_get_page", map[string]any{"pageId": "65539"})
	s := dump(out)
	if strings.Contains(s, "보안 점검 결과") || strings.Contains(s, "대외비") || strings.Contains(s, "RENDERED") {
		t.Fatalf("include target leaked: %s", s)
	}
	if !strings.Contains(s, "내용은 확장되지 않음") {
		t.Fatalf("no macro marker: %s", s)
	}
}

func approve(t *testing.T, s *session, id string) {
	t.Helper()
	if c, b := s.do("POST", "/api/me/approvals/"+id+"/decide", map[string]any{"approve": true}); c != 200 {
		t.Fatalf("approve: %d %s", c, b)
	}
}

func propose(t *testing.T, key, name string, args map[string]any) string {
	t.Helper()
	_, out := tool(t, key, name, args)
	if code(out) != "APPROVAL_REQUIRED" {
		t.Fatalf("expected APPROVAL_REQUIRED: %v", out)
	}
	return out["approvalId"].(string)
}

func currentVersion(t *testing.T, id string) int {
	req, _ := http.NewRequest("GET", mock+"/rest/api/content/"+id+"?expand=version", nil)
	req.SetBasicAuth("confmcp-svc", "svc-password")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var c struct{ Version struct{ Number int } }
	_ = json.NewDecoder(resp.Body).Decode(&c)
	return c.Version.Number
}

func TestAC09_StaleApproval(t *testing.T) {
	setup(t)
	v := currentVersion(t, "65540")
	args := map[string]any{"pageId": "65540", "expectedVersion": v, "mode": "append", "body": "추가 1"}
	id := propose(t, aliceKey, "confluence_update_page", args)
	approve(t, alice, id)
	// Someone edits the page directly in Confluence after the approval.
	body := fmt.Sprintf(`{"type":"page","title":"인증 API","version":{"number":%d},"body":{"storage":{"value":"<p>외부 수정</p>","representation":"storage"}}}`, v+1)
	req, _ := http.NewRequest("PUT", mock+"/rest/api/content/65540", strings.NewReader(body))
	req.SetBasicAuth("bob", "bob-password")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	args["approvalId"] = id
	_, out := tool(t, aliceKey, "confluence_update_page", args)
	if c := code(out); c != "VERSION_CONFLICT" && c != "APPROVAL_STALE" {
		t.Fatalf("stale approval applied: %v", out)
	}
	// Changing arguments after approval is stale too.
	v = currentVersion(t, "65540")
	args = map[string]any{"pageId": "65540", "expectedVersion": v, "mode": "append", "body": "원래 내용"}
	id = propose(t, aliceKey, "confluence_update_page", args)
	approve(t, alice, id)
	args["body"], args["approvalId"] = "바꿔치기", id
	_, out = tool(t, aliceKey, "confluence_update_page", args)
	if code(out) != "APPROVAL_STALE" {
		t.Fatalf("changed arguments accepted: %v", out)
	}
}

func TestAC10_SingleExecution(t *testing.T) {
	setup(t)
	v := currentVersion(t, "65543")
	args := map[string]any{"pageId": "65543", "expectedVersion": v, "mode": "prepend", "body": "동시성 시험"}
	id := propose(t, aliceKey, "confluence_update_page", args)
	approve(t, alice, id)
	args["approvalId"] = id
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); tool(t, aliceKey, "confluence_update_page", args) }()
	}
	wg.Wait()
	if got := currentVersion(t, "65543"); got != v+1 {
		t.Fatalf("version %d → %d: the write ran more than once", v, got)
	}
}

func TestAC12_MacrosPreserved(t *testing.T) {
	setup(t)
	v := currentVersion(t, "65538")
	args := map[string]any{"pageId": "65538", "expectedVersion": v, "mode": "replace_text", "find": "첫 주에", "body": "입사 첫 주에", "replaceAll": true}
	id := propose(t, aliceKey, "confluence_update_page", args)
	approve(t, alice, id)
	args["approvalId"] = id
	if isErr, out := tool(t, aliceKey, "confluence_update_page", args); isErr {
		t.Fatalf("update failed: %v", out)
	}
	_, out := tool(t, aliceKey, "confluence_get_page", map[string]any{"pageId": "65538", "format": "storage"})
	data, _ := out["data"].(map[string]any)
	body, _ := data["body"].(map[string]any)
	s, _ := body["content"].(string)
	for _, want := range []string{"입사 첫 주에", `ac:name="code"`, "make setup && make test]]>", "<table>"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q after edit: %s", want, s)
		}
	}
	_, out = tool(t, aliceKey, "confluence_update_page", map[string]any{"pageId": "65538", "expectedVersion": v + 1, "mode": "replace_markdown", "body": "# 덮어쓰기"})
	if code(out) != "UNSUPPORTED_CONTENT" {
		t.Fatalf("markdown overwrite of a macro page allowed: %v", out)
	}
}

func TestAC15_ContextPathKorean(t *testing.T) {
	setup(t)
	_, out := tool(t, aliceKey, "confluence_pages", map[string]any{"spaceKey": "DEV", "title": "온보딩 가이드"})
	s := dump(out)
	if !strings.Contains(s, "/confluence/pages/viewpage.action?pageId=65538") {
		t.Fatalf("context path or Korean title broken: %s", s)
	}
}

func TestAC17_CursorBoundToUser(t *testing.T) {
	setup(t)
	_, out := tool(t, aliceKey, "confluence_pages", map[string]any{"spaceKey": "DEV", "limit": 1})
	page, _ := out["pagination"].(map[string]any)
	cursor, _ := page["nextCursor"].(string)
	if cursor == "" {
		t.Fatalf("no cursor: %v", out)
	}
	_, out = tool(t, bobKey, "confluence_pages", map[string]any{"spaceKey": "DEV", "limit": 1, "cursor": cursor})
	if code(out) != "BAD_ARGUMENTS" {
		t.Fatalf("another user's cursor accepted: %v", out)
	}
}

func TestPolicyDeniesHR(t *testing.T) {
	setup(t)
	_, out := tool(t, aliceKey, "confluence_spaces", nil)
	if strings.Contains(dump(out), `"HR"`) {
		t.Fatal("denied space listed")
	}
}
