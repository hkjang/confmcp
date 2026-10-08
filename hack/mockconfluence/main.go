// Command mockconfluence is a small Confluence Server 7.2.0 stand-in for
// development, end-to-end tests and documentation screenshots.
//
// It serves the subset of /rest/api that confmcp uses under a context path
// (/confluence), plus the confmcp permission plugin's signed endpoints. Its
// permission model follows Confluence's: space permissions per operation, a
// page's view restriction applies to all its descendants, and an edit
// restriction applies only to the page itself.
//
// It is not a Confluence emulator. Everything it checks was written to match
// the documented 7.2.0 behaviour; the real server remains the reference.
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const ctxPath = "/confluence"

type user struct {
	Username, Key, Display, Email, Password string
	Active                                bool
}

type spacePerm struct{ view, create, comment, attach, delete bool }

type space struct {
	ID          int64
	Key, Name   string
	Description string
	HomeID      string
	Perms       map[string]spacePerm // username -> grants; "*" = all licensed users
}

type content struct {
	ID, Type, Status, Title, Space, Parent, Container string
	Body                                              string
	Version                                           int
	When                                              time.Time
	By                                                string
	Created                                           time.Time
	CreatedBy                                         string
	ViewRestrict, EditRestrict                        []string
	Labels                                            []string
	// attachments
	Filename, MediaType string
	Data                []byte
}

type store struct {
	mu       sync.Mutex
	users    map[string]*user // by username
	spaces   map[string]*space
	content  map[string]*content
	order    []string
	nextID   int
	nonces   map[string]time.Time
	secret   string
	svc      string
}

func main() {
	addr := envOr("MOCK_ADDR", ":8090")
	s := seed()
	s.secret = envOr("MOCK_PLUGIN_SECRET", "mock-plugin-secret")
	mux := http.NewServeMux()
	mux.HandleFunc(ctxPath+"/", s.route)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) })
	log.Printf("mock Confluence 7.2.0 on %s%s", addr, ctxPath)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// ---------- seed data ----------

func seed() *store {
	s := &store{users: map[string]*user{}, spaces: map[string]*space{}, content: map[string]*content{},
		nextID: 98300, nonces: map[string]time.Time{}, svc: "confmcp-svc"}
	for _, u := range []*user{
		{"confmcp-svc", "ff8080817a0001", "confmcp 서비스 계정", "svc@example.com", "svc-password", true},
		{"admin", "ff8080817a0002", "관리자", "admin@example.com", "admin-password", true},
		{"alice", "ff8080817a0010", "김앨리스", "alice@example.com", "alice-password", true},
		{"bob", "ff8080817a0011", "박밥", "bob@example.com", "bob-password", true},
		{"carol", "ff8080817a0012", "이캐롤", "carol@example.com", "carol-password", true},
		{"dave", "ff8080817a0013", "정데이브", "dave@example.com", "dave-password", false},
		{"kim.sso", "ff8080817a0020", "김에스에스오", "kim.sso@example.com", "sso-password-1", true},
		{"lee.sso", "ff8080817a0021", "이에스에스오", "lee.sso@example.com", "sso-password-1", true},
	} {
		s.users[u.Username] = u
	}
	all := spacePerm{true, true, true, true, true}
	readOnly := spacePerm{view: true}
	writer := spacePerm{true, true, true, true, false}
	s.spaces["DEV"] = &space{ID: 1001, Key: "DEV", Name: "개발팀", Description: "개발팀 공용 문서 공간",
		Perms: map[string]spacePerm{"confmcp-svc": all, "alice": writer, "bob": all, "carol": readOnly, "admin": all, "kim.sso": writer, "lee.sso": all}}
	s.spaces["OPS"] = &space{ID: 1002, Key: "OPS", Name: "운영", Description: "운영 절차와 장애 대응",
		Perms: map[string]spacePerm{"confmcp-svc": all, "alice": readOnly, "bob": writer, "admin": all}}
	s.spaces["HR"] = &space{ID: 1003, Key: "HR", Name: "인사", Description: "인사 규정 (제한 공간)",
		Perms: map[string]spacePerm{"confmcp-svc": all, "carol": writer, "admin": all}}
	s.spaces["ARCH"] = &space{ID: 1004, Key: "ARCH", Name: "아키텍처", Description: "아키텍처 결정 기록",
		Perms: map[string]spacePerm{"confmcp-svc": all, "alice": writer, "bob": all, "admin": all}}

	day := func(d int) time.Time { return time.Date(2026, 9, 1, 9, 0, 0, 0, time.FixedZone("KST", 9*3600)).AddDate(0, 0, d) }
	add := func(c *content) *content {
		if c.Status == "" {
			c.Status = "current"
		}
		if c.Version == 0 {
			c.Version = 1
		}
		if c.By == "" {
			c.By = "alice"
		}
		if c.CreatedBy == "" {
			c.CreatedBy = c.By
		}
		if c.Created.IsZero() {
			c.Created = c.When
		}
		s.content[c.ID] = c
		s.order = append(s.order, c.ID)
		return c
	}
	add(&content{ID: "65537", Type: "page", Title: "개발팀 홈", Space: "DEV", When: day(0), By: "bob",
		Body: `<h1>개발팀 공간</h1><p>개발팀의 설계·운영 문서를 모읍니다. 새 팀원은 <strong>온보딩 가이드</strong>부터 읽으십시오.</p>` +
			`<ac:structured-macro ac:name="info"><ac:rich-text-body><p>문서 수정은 confmcp 승인 흐름을 거쳐 반영됩니다.</p></ac:rich-text-body></ac:structured-macro>` +
			`<ac:structured-macro ac:name="children" />`, Labels: []string{"home"}})
	s.spaces["DEV"].HomeID = "65537"
	add(&content{ID: "65538", Type: "page", Title: "온보딩 가이드", Space: "DEV", Parent: "65537", When: day(3), Version: 4,
		Body: `<h1>온보딩 가이드</h1><p>첫 주에 해야 할 일을 정리했습니다.</p><h2>계정 준비</h2><ol><li>Keycloak 계정 발급</li><li>confmcp 콘솔 로그인<ul><li>내 Confluence 연결 확인</li></ul></li><li>API 키 발급</li></ol>` +
			`<h2>개발 환경</h2><ac:structured-macro ac:name="code"><ac:parameter ac:name="language">bash</ac:parameter><ac:plain-text-body><![CDATA[git clone https://git.example.internal/dev/app.git
make setup && make test]]></ac:plain-text-body></ac:structured-macro>` +
			`<table><tbody><tr><th>항목</th><th>담당</th></tr><tr><td>장비</td><td>운영팀</td></tr><tr><td>권한</td><td>팀장</td></tr></tbody></table>` +
			`<h2>참고</h2><p>질문은 #dev-help 채널에 남겨 주세요.</p>`, Labels: []string{"onboarding", "guide"}})
	add(&content{ID: "65539", Type: "page", Title: "API 설계 원칙", Space: "DEV", Parent: "65537", When: day(5), Version: 7, By: "bob",
		Body: `<h1>API 설계 원칙</h1><p>모든 API 는 다음 원칙을 따릅니다.</p><ul><li>리소스 중심 URL</li><li>명시적 버전</li><li>멱등성 키 지원</li></ul>` +
			`<ac:structured-macro ac:name="warning"><ac:rich-text-body><p>인증 토큰을 로그에 남기지 마십시오.</p></ac:rich-text-body></ac:structured-macro>` +
			`<ac:structured-macro ac:name="include"><ac:parameter ac:name=""><ac:link><ri:page ri:content-title="보안 점검 결과" /></ac:link></ac:parameter></ac:structured-macro>`,
		Labels: []string{"api", "standard"}})
	add(&content{ID: "65540", Type: "page", Title: "인증 API", Space: "DEV", Parent: "65539", When: day(8), Version: 2,
		Body: `<h1>인증 API</h1><p>OAuth 2.1 기반 토큰 발급 흐름입니다.</p><ac:structured-macro ac:name="code"><ac:parameter ac:name="language">http</ac:parameter><ac:plain-text-body><![CDATA[POST /oauth/token
grant_type=authorization_code&code=...&code_verifier=...]]></ac:plain-text-body></ac:structured-macro>`})
	add(&content{ID: "65541", Type: "page", Title: "보안 점검 결과", Space: "DEV", Parent: "65537", When: day(10), By: "bob",
		Body: `<h1>보안 점검 결과</h1><p>2026년 3분기 점검에서 발견된 항목입니다. <strong>대외비</strong></p>`, ViewRestrict: []string{"bob", "confmcp-svc"}})
	add(&content{ID: "65542", Type: "page", Title: "취약점 목록", Space: "DEV", Parent: "65541", When: day(10), By: "bob",
		Body: `<p>CVE 대응 현황 (상위 페이지 열람 제한이 상속됩니다)</p>`})
	add(&content{ID: "65543", Type: "page", Title: "릴리스 노트 2026", Space: "DEV", Parent: "65537", When: day(20), Version: 12,
		Body: `<h1>릴리스 노트 2026</h1><h2>v2.4.0</h2><ul><li>검색 성능 개선</li><li>배포 자동화</li></ul><h2>v2.3.0</h2><ul><li>권한 점검 강화</li></ul>`,
		Labels: []string{"release"}})
	add(&content{ID: "65544", Type: "page", Title: "아키텍처 결정 기록", Space: "ARCH", When: day(2), By: "bob",
		Body: `<h1>아키텍처 결정 기록</h1><p>이 페이지는 편집이 제한되어 있지만 하위 ADR 은 각자 편집할 수 있습니다.</p>` +
			`<ac:layout><ac:layout-section ac:type="two_equal"><ac:layout-cell><p>왼쪽: 원칙</p></ac:layout-cell><ac:layout-cell><p>오른쪽: 결정 목록</p></ac:layout-cell></ac:layout-section></ac:layout>`,
		EditRestrict: []string{"bob", "confmcp-svc"}})
	s.spaces["ARCH"].HomeID = "65544"
	add(&content{ID: "65545", Type: "page", Title: "ADR-001 게이트웨이 도입", Space: "ARCH", Parent: "65544", When: day(4),
		Body: `<h1>ADR-001 게이트웨이 도입</h1><h2>상태</h2><p>승인됨</p><h2>결정</h2><p>MCP 요청은 confmcp 게이트웨이를 거친다.</p>`})
	add(&content{ID: "65546", Type: "page", Title: "장애 대응 절차", Space: "OPS", When: day(6), By: "bob", Version: 3,
		Body: `<h1>장애 대응 절차</h1><ol><li>알림 확인</li><li>영향 범위 파악</li><li>공지</li></ol><p>담당: <ac:link><ri:user ri:userkey="ff8080817a0011" /></ac:link></p>`,
		Labels: []string{"runbook"}})
	s.spaces["OPS"].HomeID = "65546"
	add(&content{ID: "65547", Type: "page", Title: "인사 규정", Space: "HR", When: day(1), By: "carol",
		Body: `<h1>인사 규정</h1><p>인사팀 전용 문서입니다.</p>`})
	s.spaces["HR"].HomeID = "65547"
	add(&content{ID: "65560", Type: "blogpost", Title: "9월 개발팀 소식", Space: "DEV", When: day(25), By: "bob",
		Body: `<p>이번 달에는 confmcp 를 도입했습니다. AI 도구가 이제 권한에 맞는 문서만 읽습니다.</p>`})
	add(&content{ID: "65570", Type: "comment", Space: "DEV", Container: "65538", When: day(4), By: "bob",
		Body: `<p>장비 신청 링크도 추가해 주세요.</p>`})
	add(&content{ID: "65571", Type: "comment", Space: "DEV", Container: "65538", Parent: "65570", When: day(4), By: "alice",
		Body: `<p>네, 다음 버전에 반영하겠습니다.</p>`})
	add(&content{ID: "att65580", Type: "attachment", Space: "DEV", Container: "65538", When: day(3), Title: "onboarding-checklist.txt",
		Filename: "onboarding-checklist.txt", MediaType: "text/plain", Data: []byte("1. 계정\n2. 장비\n3. 교육\n")})
	add(&content{ID: "att65581", Type: "attachment", Space: "DEV", Container: "65541", When: day(10), By: "bob", Title: "scan-report.pdf",
		Filename: "scan-report.pdf", MediaType: "application/pdf", Data: []byte("%PDF-1.4 mock")})
	return s
}

// ---------- permission model ----------

func (s *store) spacePerm(username, key string) spacePerm {
	sp := s.spaces[key]
	if sp == nil {
		return spacePerm{}
	}
	u := s.users[username]
	if u == nil || !u.Active {
		return spacePerm{}
	}
	if p, ok := sp.Perms[username]; ok {
		return p
	}
	return sp.Perms["*"]
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// chain returns the page and its ancestors, nearest first.
func (s *store) chain(c *content) []*content {
	out := []*content{}
	cur := c
	if c.Type == "comment" || c.Type == "attachment" {
		cur = s.content[c.Container]
	}
	for cur != nil {
		out = append(out, cur)
		if cur.Parent == "" || cur.Type != "page" {
			break
		}
		cur = s.content[cur.Parent]
	}
	return out
}

// canView: space view and every view restriction on the page and its
// ancestors (view restrictions are inherited).
func (s *store) canView(username string, c *content) bool {
	if c == nil || c.Status != "current" || !s.spacePerm(username, c.Space).view {
		return false
	}
	for _, p := range s.chain(c) {
		if len(p.ViewRestrict) > 0 && !contains(p.ViewRestrict, username) {
			return false
		}
	}
	return true
}

// canEdit: view, the space's add-page permission and the page's own edit
// restriction (edit restrictions are not inherited).
func (s *store) canEdit(username string, c *content) bool {
	if !s.canView(username, c) || !s.spacePerm(username, c.Space).create {
		return false
	}
	return len(c.EditRestrict) == 0 || contains(c.EditRestrict, username)
}

func (s *store) decide(username, op string, c *content, sp *space) (string, string) {
	if c != nil {
		if !s.canView(username, c) {
			return "deny", "NOT_VISIBLE"
		}
		perm := s.spacePerm(username, c.Space)
		switch op {
		case "read":
			return "allow", ""
		case "edit", "move":
			if s.canEdit(username, c) {
				return "allow", ""
			}
			return "deny", "PAGE_EDIT_RESTRICTED"
		case "create":
			if perm.create {
				return "allow", ""
			}
			return "deny", "NO_SPACE_CREATE"
		case "comment":
			if perm.comment {
				return "allow", ""
			}
			return "deny", "NO_SPACE_COMMENT"
		case "attach":
			if perm.attach && s.canEdit(username, c) {
				return "allow", ""
			}
			return "deny", "NO_ATTACH"
		case "delete":
			if perm.delete && s.canEdit(username, c) {
				return "allow", ""
			}
			return "deny", "NO_SPACE_DELETE"
		}
		return "unknown", "UNKNOWN_OPERATION"
	}
	if sp == nil {
		return "deny", "NOT_VISIBLE"
	}
	perm := s.spacePerm(username, sp.Key)
	if !perm.view {
		return "deny", "NOT_VISIBLE"
	}
	m := map[string]bool{"read": true, "create": perm.create, "comment": perm.comment, "attach": perm.attach, "delete": perm.delete, "edit": perm.create, "move": perm.create}
	if v, ok := m[op]; ok {
		if v {
			return "allow", ""
		}
		return "deny", "NO_SPACE_PERMISSION"
	}
	return "unknown", "UNKNOWN_OPERATION"
}

// ---------- HTTP ----------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json;charset=UTF-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func apiErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"statusCode": status, "message": msg, "data": map[string]any{}})
}

func (s *store) auth(r *http.Request) *user {
	u, p, ok := r.BasicAuth()
	if !ok {
		return nil
	}
	usr := s.users[u]
	if usr == nil || usr.Password != p || !usr.Active {
		return nil
	}
	return usr
}

func (s *store) route(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := strings.TrimPrefix(r.URL.Path, ctxPath)
	if strings.HasPrefix(path, "/rest/confmcp/1.0/") {
		s.plugin(w, r, path)
		return
	}
	if path == "/rest/applinks/1.0/manifest" {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<manifest><id>mock</id><name>Confluence</name><typeId>confluence</typeId><version>7.2.0</version><buildNumber>8402</buildNumber></manifest>`))
		return
	}
	if strings.HasPrefix(path, "/login.action") || path == "/" {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body>Log in - Confluence</body></html>`))
		return
	}
	u := s.auth(r)
	if u == nil {
		w.Header().Set("WWW-Authenticate", `Basic realm="Confluence"`)
		apiErr(w, 401, "This request requires authentication")
		return
	}
	switch {
	case strings.HasPrefix(path, "/download/attachments/"):
		s.download(w, r, u, path)
	case path == "/rest/api/user/current":
		writeJSON(w, 200, userJSON(u))
	case path == "/rest/api/user":
		s.getUser(w, r)
	case path == "/rest/api/space":
		s.listSpaces(w, r, u)
	case strings.HasPrefix(path, "/rest/api/space/"):
		s.getSpace(w, r, u, strings.TrimPrefix(path, "/rest/api/space/"))
	case path == "/rest/api/content/search":
		s.search(w, r, u)
	case path == "/rest/api/content":
		if r.Method == http.MethodPost {
			s.create(w, r, u)
		} else {
			s.listContent(w, r, u)
		}
	case strings.HasPrefix(path, "/rest/api/content/"):
		s.contentRoute(w, r, u, strings.Split(strings.TrimPrefix(path, "/rest/api/content/"), "/"))
	default:
		apiErr(w, 404, "Not found: "+path)
	}
}

func userJSON(u *user) map[string]any {
	return map[string]any{"type": "known", "username": u.Username, "userKey": u.Key, "displayName": u.Display,
		"profilePicture": map[string]any{"path": "/images/icons/profilepics/default.svg"}}
}

func (s *store) getUser(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	for _, u := range s.users {
		if (q.Get("username") != "" && u.Username == q.Get("username")) || (q.Get("key") != "" && u.Key == q.Get("key")) {
			writeJSON(w, 200, userJSON(u))
			return
		}
	}
	apiErr(w, 404, "No user found")
}

func pageParams(r *http.Request) (int, int) {
	start, _ := strconv.Atoi(r.URL.Query().Get("start"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 25
	}
	return start, limit
}

func paged(r *http.Request, items []any) map[string]any {
	start, limit := pageParams(r)
	end := start + limit
	if start > len(items) {
		start = len(items)
	}
	if end > len(items) {
		end = len(items)
	}
	out := map[string]any{"results": items[start:end], "start": start, "limit": limit, "size": end - start,
		"_links": map[string]any{"base": "http://" + r.Host + ctxPath, "context": ctxPath}}
	if end < len(items) {
		q := r.URL.Query()
		q.Set("start", strconv.Itoa(end))
		out["_links"].(map[string]any)["next"] = r.URL.Path + "?" + q.Encode()
	}
	return out
}

func (s *store) spaceJSON(sp *space, viewer string) map[string]any {
	out := map[string]any{"id": sp.ID, "key": sp.Key, "name": sp.Name, "type": "global", "status": "current",
		"description": map[string]any{"plain": map[string]any{"value": sp.Description, "representation": "plain"}},
		"_links": map[string]any{"webui": "/display/" + sp.Key}}
	if h := s.content[sp.HomeID]; h != nil {
		out["homepage"] = map[string]any{"id": h.ID, "type": "page", "title": h.Title}
	}
	return out
}

func (s *store) listSpaces(w http.ResponseWriter, r *http.Request, u *user) {
	keys := []string{}
	for k := range s.spaces {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	items := []any{}
	for _, k := range keys {
		if s.spacePerm(u.Username, k).view {
			items = append(items, s.spaceJSON(s.spaces[k], u.Username))
		}
	}
	writeJSON(w, 200, paged(r, items))
}

func (s *store) getSpace(w http.ResponseWriter, r *http.Request, u *user, key string) {
	sp := s.spaces[strings.ToUpper(key)]
	if sp == nil || !s.spacePerm(u.Username, sp.Key).view {
		apiErr(w, 404, "No space with key : "+key)
		return
	}
	out := s.spaceJSON(sp, u.Username)
	if strings.Contains(r.URL.Query().Get("expand"), "operations") {
		perm := s.spacePerm(u.Username, sp.Key)
		ops := []map[string]string{{"operation": "read", "targetType": "space"}}
		if perm.create {
			ops = append(ops, map[string]string{"operation": "create", "targetType": "page"}, map[string]string{"operation": "create", "targetType": "blogpost"})
		}
		if perm.comment {
			ops = append(ops, map[string]string{"operation": "create", "targetType": "comment"})
		}
		if perm.attach {
			ops = append(ops, map[string]string{"operation": "create", "targetType": "attachment"})
		}
		out["operations"] = ops
	}
	writeJSON(w, 200, out)
}

func (s *store) ancestors(c *content) []*content {
	out := []*content{}
	p := s.content[c.Parent]
	if c.Type == "comment" {
		// A reply's ancestors are the comments it answers.
		for p != nil && p.Type == "comment" {
			out = append([]*content{p}, out...)
			p = s.content[p.Parent]
		}
		return out
	}
	for p != nil {
		out = append([]*content{p}, out...)
		p = s.content[p.Parent]
	}
	return out
}

func (s *store) contentJSON(c *content, expand string, viewer string) map[string]any {
	sp := s.spaces[c.Space]
	out := map[string]any{"id": c.ID, "type": c.Type, "status": c.Status, "title": c.Title}
	webui := "/pages/viewpage.action?pageId=" + c.ID
	if c.Type == "blogpost" {
		webui = "/pages/viewpage.action?pageId=" + c.ID
	}
	links := map[string]any{"webui": webui, "self": ctxPath + "/rest/api/content/" + c.ID}
	if c.Type == "attachment" {
		links["download"] = "/download/attachments/" + c.Container + "/" + c.Filename + "?version=" + strconv.Itoa(c.Version) + "&api=v2"
		out["metadata"] = map[string]any{"mediaType": c.MediaType, "comment": ""}
		out["extensions"] = map[string]any{"mediaType": c.MediaType, "fileSize": len(c.Data)}
	}
	out["_links"] = links
	has := func(k string) bool { return strings.Contains(","+expand+",", ","+k+",") || strings.Contains(expand, k+".") }
	if has("space") && sp != nil {
		out["space"] = map[string]any{"key": sp.Key, "name": sp.Name}
	}
	if has("version") {
		by := s.users[c.By]
		v := map[string]any{"number": c.Version, "when": c.When.Format(time.RFC3339), "minorEdit": false}
		if by != nil {
			v["by"] = userJSON(by)
		}
		out["version"] = v
	}
	if has("history") {
		cb := s.users[c.CreatedBy]
		h := map[string]any{"createdDate": c.Created.Format(time.RFC3339), "latest": true}
		if cb != nil {
			h["createdBy"] = userJSON(cb)
		}
		out["history"] = h
	}
	if has("ancestors") {
		anc := []any{}
		for _, a := range s.ancestors(c) {
			anc = append(anc, map[string]any{"id": a.ID, "type": a.Type, "title": a.Title})
		}
		out["ancestors"] = anc
	}
	if has("container") && c.Container != "" {
		if ct := s.content[c.Container]; ct != nil {
			out["container"] = map[string]any{"id": ct.ID, "type": ct.Type, "title": ct.Title}
		}
	}
	if strings.Contains(expand, "body.storage") {
		out["body"] = map[string]any{"storage": map[string]any{"value": c.Body, "representation": "storage"}}
	}
	if strings.Contains(expand, "body.view") {
		// The trap confmcp must avoid: rendered view expands include macros
		// with the caller's authority (here, the service account's).
		out["body"] = map[string]any{"view": map[string]any{"value": "<p>RENDERED (includes restricted content)</p>"}}
	}
	if strings.Contains(expand, "metadata.labels") {
		ls := []any{}
		for _, l := range c.Labels {
			ls = append(ls, map[string]any{"prefix": "global", "name": l, "id": l})
		}
		md, _ := out["metadata"].(map[string]any)
		if md == nil {
			md = map[string]any{}
		}
		md["labels"] = map[string]any{"results": ls, "size": len(ls)}
		out["metadata"] = md
	}
	if strings.Contains(expand, "operations") {
		ops := []map[string]string{{"operation": "read", "targetType": c.Type}}
		if s.canEdit(viewer, c) {
			ops = append(ops, map[string]string{"operation": "update", "targetType": c.Type})
			if s.spacePerm(viewer, c.Space).delete {
				ops = append(ops, map[string]string{"operation": "delete", "targetType": c.Type})
			}
			if s.spacePerm(viewer, c.Space).attach {
				ops = append(ops, map[string]string{"operation": "create", "targetType": "attachment"})
			}
		}
		if s.spacePerm(viewer, c.Space).create {
			ops = append(ops, map[string]string{"operation": "create", "targetType": "page"})
		}
		if s.spacePerm(viewer, c.Space).comment {
			ops = append(ops, map[string]string{"operation": "create", "targetType": "comment"})
		}
		out["operations"] = ops
	}
	return out
}

func (s *store) visibleList(u *user, pred func(*content) bool) []*content {
	out := []*content{}
	for _, id := range s.order {
		c := s.content[id]
		if pred(c) && s.canView(u.Username, c) {
			out = append(out, c)
		}
	}
	return out
}

func (s *store) toItems(list []*content, expand, viewer string) []any {
	items := []any{}
	for _, c := range list {
		items = append(items, s.contentJSON(c, expand, viewer))
	}
	return items
}

func (s *store) listContent(w http.ResponseWriter, r *http.Request, u *user) {
	q := r.URL.Query()
	typ := q.Get("type")
	if typ == "" {
		typ = "page"
	}
	list := s.visibleList(u, func(c *content) bool {
		return c.Type == typ && (q.Get("spaceKey") == "" || strings.EqualFold(c.Space, q.Get("spaceKey"))) &&
			(q.Get("title") == "" || c.Title == q.Get("title"))
	})
	writeJSON(w, 200, paged(r, s.toItems(list, q.Get("expand"), u.Username)))
}

var (
	clauseRe = regexp.MustCompile(`(?i)^\s*(text|title|space|label|type|lastmodified)\s*(~|=|in|>=)\s*(.+?)\s*$`)
	quotedRe = regexp.MustCompile(`"((?:[^"\\]|\\.)*)"|([A-Za-z0-9_\-]+)`)
)

func splitAnd(cql string) []string {
	var out []string
	depth, quote, start := 0, false, 0
	up := strings.ToUpper(cql)
	for i := 0; i < len(cql); i++ {
		switch {
		case cql[i] == '\\' && quote:
			i++
		case cql[i] == '"':
			quote = !quote
		case !quote && cql[i] == '(':
			depth++
		case !quote && cql[i] == ')':
			depth--
		case !quote && depth == 0 && strings.HasPrefix(up[i:], " AND "):
			out = append(out, cql[start:i])
			start = i + 5
			i += 4
		}
	}
	return append(out, cql[start:])
}

func values(v string) []string {
	out := []string{}
	for _, m := range quotedRe.FindAllStringSubmatch(v, -1) {
		if m[1] != "" {
			out = append(out, strings.ReplaceAll(strings.ReplaceAll(m[1], `\"`, `"`), `\\`, `\`))
		} else if m[2] != "" {
			out = append(out, m[2])
		}
	}
	return out
}

func (s *store) search(w http.ResponseWriter, r *http.Request, u *user) {
	cql := r.URL.Query().Get("cql")
	if i := strings.Index(strings.ToUpper(cql), " ORDER BY"); i >= 0 {
		cql = cql[:i]
	}
	preds := []func(*content) bool{}
	for _, part := range splitAnd(cql) {
		part = strings.TrimSpace(strings.Trim(strings.TrimSpace(part), "()"))
		m := clauseRe.FindStringSubmatch(part)
		if m == nil {
			apiErr(w, 400, "Could not parse cql : "+part)
			return
		}
		field, vals := strings.ToLower(m[1]), values(m[3])
		switch field {
		case "text":
			preds = append(preds, func(c *content) bool {
				return strings.Contains(strings.ToLower(c.Title+" "+c.Body), strings.ToLower(strings.Join(vals, " ")))
			})
		case "title":
			preds = append(preds, func(c *content) bool { return strings.Contains(strings.ToLower(c.Title), strings.ToLower(strings.Join(vals, " "))) })
		case "space":
			preds = append(preds, func(c *content) bool {
				for _, v := range vals {
					if strings.EqualFold(c.Space, v) {
						return true
					}
				}
				return false
			})
		case "label":
			preds = append(preds, func(c *content) bool {
				for _, v := range vals {
					if contains(c.Labels, strings.ToLower(v)) {
						return true
					}
				}
				return false
			})
		case "type":
			preds = append(preds, func(c *content) bool { return contains(vals, c.Type) })
		case "lastmodified":
			t, _ := time.Parse("2006-01-02", strings.Join(vals, ""))
			preds = append(preds, func(c *content) bool { return !c.When.Before(t) })
		}
	}
	list := s.visibleList(u, func(c *content) bool {
		for _, p := range preds {
			if !p(c) {
				return false
			}
		}
		return true
	})
	if strings.Contains(strings.ToUpper(r.URL.Query().Get("cql")), "ORDER BY LASTMODIFIED DESC") {
		sort.SliceStable(list, func(i, j int) bool { return list[i].When.After(list[j].When) })
	}
	writeJSON(w, 200, paged(r, s.toItems(list, r.URL.Query().Get("expand"), u.Username)))
}

func (s *store) contentRoute(w http.ResponseWriter, r *http.Request, u *user, parts []string) {
	c := s.content[parts[0]]
	if c == nil || (!s.canView(u.Username, c) && !(r.Method == http.MethodGet && c.Status == "trashed" && u.Username == s.svc)) {
		apiErr(w, 404, "No content found with id : "+parts[0])
		return
	}
	expand := r.URL.Query().Get("expand")
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, 200, s.contentJSON(c, expand, u.Username))
		case http.MethodPut:
			s.update(w, r, u, c)
		case http.MethodDelete:
			if !s.canEdit(u.Username, c) || !s.spacePerm(u.Username, c.Space).delete {
				apiErr(w, 403, "Not permitted to delete")
				return
			}
			c.Status = "trashed"
			w.WriteHeader(204)
		default:
			apiErr(w, 405, "method")
		}
		return
	}
	switch parts[1] {
	case "child":
		if len(parts) < 3 {
			apiErr(w, 404, "child type")
			return
		}
		kind := parts[2]
		if kind == "attachment" && r.Method == http.MethodPost {
			s.upload(w, r, u, c)
			return
		}
		list := s.visibleList(u, func(x *content) bool {
			switch kind {
			case "page":
				return x.Type == "page" && x.Parent == c.ID
			case "comment":
				return x.Type == "comment" && x.Container == c.ID
			case "attachment":
				return x.Type == "attachment" && x.Container == c.ID
			}
			return false
		})
		writeJSON(w, 200, paged(r, s.toItems(list, expand, u.Username)))
	case "label":
		switch r.Method {
		case http.MethodGet:
			items := []any{}
			for _, l := range c.Labels {
				items = append(items, map[string]any{"prefix": "global", "name": l, "id": l, "label": l})
			}
			writeJSON(w, 200, paged(r, items))
		case http.MethodPost:
			if !s.canEdit(u.Username, c) {
				apiErr(w, 403, "Not permitted to add labels")
				return
			}
			var in []map[string]string
			_ = json.NewDecoder(r.Body).Decode(&in)
			for _, l := range in {
				if !contains(c.Labels, l["name"]) {
					c.Labels = append(c.Labels, l["name"])
				}
			}
			items := []any{}
			for _, l := range c.Labels {
				items = append(items, map[string]any{"prefix": "global", "name": l, "id": l})
			}
			writeJSON(w, 200, map[string]any{"results": items, "size": len(items)})
		case http.MethodDelete:
			if !s.canEdit(u.Username, c) {
				apiErr(w, 403, "Not permitted to remove labels")
				return
			}
			name := r.URL.Query().Get("name")
			kept := []string{}
			for _, l := range c.Labels {
				if l != name {
					kept = append(kept, l)
				}
			}
			c.Labels = kept
			w.WriteHeader(204)
		}
	default:
		apiErr(w, 404, "unknown")
	}
}

func (s *store) newID() string {
	s.nextID++
	return strconv.Itoa(s.nextID)
}

func (s *store) create(w http.ResponseWriter, r *http.Request, u *user) {
	var in struct {
		Type      string `json:"type"`
		Title     string `json:"title"`
		Space     struct{ Key string } `json:"space"`
		Ancestors []struct{ ID string } `json:"ancestors"`
		Container struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"container"`
		Body struct {
			Storage struct{ Value string } `json:"storage"`
		} `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		apiErr(w, 400, "bad json")
		return
	}
	c := &content{ID: s.newID(), Type: in.Type, Status: "current", Title: in.Title, Space: strings.ToUpper(in.Space.Key),
		Body: in.Body.Storage.Value, Version: 1, When: time.Now(), By: u.Username, Created: time.Now(), CreatedBy: u.Username}
	switch in.Type {
	case "comment":
		page := s.content[in.Container.ID]
		if page == nil || !s.canView(u.Username, page) || !s.spacePerm(u.Username, page.Space).comment {
			apiErr(w, 403, "Not permitted to comment")
			return
		}
		c.Container, c.Space = page.ID, page.Space
		if len(in.Ancestors) > 0 {
			c.Parent = in.Ancestors[0].ID
		}
	case "page", "blogpost":
		if !s.spacePerm(u.Username, c.Space).create {
			apiErr(w, 403, "Not permitted to create content in space")
			return
		}
		for _, x := range s.content {
			if x.Type == c.Type && x.Space == c.Space && x.Title == c.Title && x.Status == "current" {
				apiErr(w, 400, "A page with this title already exists")
				return
			}
		}
		if len(in.Ancestors) > 0 {
			parent := s.content[in.Ancestors[0].ID]
			if parent == nil || !s.canView(u.Username, parent) {
				apiErr(w, 403, "Cannot create under parent")
				return
			}
			c.Parent = parent.ID
		}
	default:
		apiErr(w, 400, "unsupported type")
		return
	}
	s.content[c.ID] = c
	s.order = append(s.order, c.ID)
	writeJSON(w, 200, s.contentJSON(c, "space,version,ancestors", u.Username))
}

func (s *store) update(w http.ResponseWriter, r *http.Request, u *user, c *content) {
	var in struct {
		Title     string               `json:"title"`
		Version   struct{ Number int } `json:"version"`
		Ancestors []struct{ ID string } `json:"ancestors"`
		Body      struct {
			Storage struct{ Value string } `json:"storage"`
		} `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		apiErr(w, 400, "bad json")
		return
	}
	if !s.canEdit(u.Username, c) {
		apiErr(w, 403, "Not permitted to update this content")
		return
	}
	if in.Version.Number != c.Version+1 {
		apiErr(w, 409, fmt.Sprintf("Version must be incremented on update. Current version is: %d", c.Version))
		return
	}
	if len(in.Ancestors) > 0 && in.Ancestors[0].ID != c.Parent {
		np := s.content[in.Ancestors[0].ID]
		if np == nil || np.Space != c.Space || !s.canView(u.Username, np) {
			apiErr(w, 400, "Invalid parent")
			return
		}
		c.Parent = np.ID
	}
	c.Title, c.Body, c.Version, c.When, c.By = in.Title, in.Body.Storage.Value, in.Version.Number, time.Now(), u.Username
	writeJSON(w, 200, s.contentJSON(c, "space,version,ancestors", u.Username))
}

func (s *store) upload(w http.ResponseWriter, r *http.Request, u *user, page *content) {
	if r.Header.Get("X-Atlassian-Token") != "nocheck" && r.Header.Get("X-Atlassian-Token") != "no-check" {
		apiErr(w, 403, "XSRF check failed")
		return
	}
	if !s.canEdit(u.Username, page) || !s.spacePerm(u.Username, page.Space).attach {
		apiErr(w, 403, "Not permitted to attach")
		return
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		apiErr(w, 400, "bad multipart")
		return
	}
	f, h, err := r.FormFile("file")
	if err != nil {
		apiErr(w, 400, "file required")
		return
	}
	data, _ := io.ReadAll(f)
	c := &content{ID: "att" + s.newID(), Type: "attachment", Status: "current", Title: h.Filename, Filename: h.Filename,
		MediaType: h.Header.Get("Content-Type"), Data: data, Container: page.ID, Space: page.Space, Version: 1,
		When: time.Now(), By: u.Username}
	s.content[c.ID] = c
	s.order = append(s.order, c.ID)
	writeJSON(w, 200, map[string]any{"results": []any{s.contentJSON(c, "version,container", u.Username)}, "size": 1})
}

func (s *store) download(w http.ResponseWriter, r *http.Request, u *user, path string) {
	parts := strings.Split(strings.TrimPrefix(path, "/download/attachments/"), "/")
	if len(parts) < 2 {
		apiErr(w, 404, "no")
		return
	}
	for _, c := range s.content {
		if c.Type == "attachment" && c.Container == parts[0] && c.Filename == parts[1] && s.canView(u.Username, c) {
			w.Header().Set("Content-Type", c.MediaType)
			_, _ = w.Write(c.Data)
			return
		}
	}
	apiErr(w, 404, "Attachment not found")
}

// ---------- permission plugin ----------

func (s *store) verifySignature(r *http.Request, body []byte) bool {
	ts, nonce, sig := r.Header.Get("X-Confmcp-Timestamp"), r.Header.Get("X-Confmcp-Nonce"), r.Header.Get("X-Confmcp-Signature")
	t, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || nonce == "" || sig == "" {
		return false
	}
	if d := time.Since(time.Unix(t, 0)); d > 5*time.Minute || d < -5*time.Minute {
		return false
	}
	for n, at := range s.nonces {
		if time.Since(at) > 10*time.Minute {
			delete(s.nonces, n)
		}
	}
	if _, used := s.nonces[nonce]; used {
		return false
	}
	sum := sha256.Sum256(body)
	mac := hmac.New(sha256.New, []byte(s.secret))
	mac.Write([]byte(r.Method + "\n" + r.URL.EscapedPath() + "\n" + hex.EncodeToString(sum[:]) + "\n" + ts + "\n" + nonce))
	if !hmac.Equal([]byte(sig), []byte(hex.EncodeToString(mac.Sum(nil)))) {
		return false
	}
	s.nonces[nonce] = time.Now()
	return true
}

type pluginTarget struct {
	Kind     string `json:"kind"`
	SpaceKey string `json:"spaceKey,omitempty"`
	ID       string `json:"id,omitempty"`
}

func (s *store) userByKey(key string) *user {
	for _, u := range s.users {
		if u.Key == key {
			return u
		}
	}
	return nil
}

func (s *store) checkOne(u *user, t pluginTarget, ops []string) map[string]any {
	res := map[string]string{}
	reasons := map[string]string{}
	out := map[string]any{"target": t}
	var c *content
	var sp *space
	if t.Kind == "content" {
		c = s.content[t.ID]
		if c != nil && s.canView(u.Username, c) {
			out["content"] = map[string]any{"id": c.ID, "type": c.Type, "status": c.Status, "spaceKey": c.Space}
		} else {
			c = &content{Status: "missing"} // not visible: answer deny without revealing existence
		}
	} else {
		sp = s.spaces[strings.ToUpper(t.SpaceKey)]
		if sp != nil && s.spacePerm(u.Username, sp.Key).view {
			out["space"] = map[string]any{"key": sp.Key}
		}
	}
	for _, op := range ops {
		v, reason := s.decide(u.Username, op, c, sp)
		res[op] = v
		if reason != "" {
			reasons[op] = reason
		}
	}
	out["results"], out["reasons"] = res, reasons
	return out
}

func (s *store) plugin(w http.ResponseWriter, r *http.Request, path string) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if !s.verifySignature(r, body) {
		apiErr(w, 401, "invalid gateway signature")
		return
	}
	switch path {
	case "/rest/confmcp/1.0/health":
		writeJSON(w, 200, map[string]any{"status": "ok", "pluginVersion": "0.1.0-mock", "confluenceVersion": "7.2.0", "buildNumber": "8402"})
	case "/rest/confmcp/1.0/users":
		var in struct{ Username, UserKey string }
		_ = json.Unmarshal(body, &in)
		for _, u := range s.users {
			if (in.Username != "" && u.Username == in.Username) || (in.UserKey != "" && u.Key == in.UserKey) {
				status := "active"
				if !u.Active {
					status = "deactivated"
				}
				writeJSON(w, 200, map[string]any{"userKey": u.Key, "username": u.Username, "displayName": u.Display,
					"email": u.Email, "status": status, "directory": "Confluence Internal Directory", "duplicates": 1})
				return
			}
		}
		apiErr(w, 404, "user not found")
	case "/rest/confmcp/1.0/permissions/check", "/rest/confmcp/1.0/permissions/batch":
		var in struct {
			UserKey    string `json:"userKey"`
			Target     pluginTarget `json:"target"`
			Operations []string `json:"operations"`
			Checks     []struct {
				Target     pluginTarget `json:"target"`
				Operations []string     `json:"operations"`
			} `json:"checks"`
		}
		if err := json.Unmarshal(body, &in); err != nil {
			apiErr(w, 400, "bad json")
			return
		}
		u := s.userByKey(in.UserKey)
		if u == nil {
			apiErr(w, 404, "unknown userKey")
			return
		}
		status := "active"
		if !u.Active {
			status = "deactivated"
		}
		now := time.Now().UTC().Format(time.RFC3339)
		if path == "/rest/confmcp/1.0/permissions/check" {
			out := s.checkOne(u, in.Target, in.Operations)
			out["userKey"], out["userStatus"], out["evaluatedAt"] = u.Key, status, now
			writeJSON(w, 200, out)
			return
		}
		results := []any{}
		for _, ch := range in.Checks {
			results = append(results, s.checkOne(u, ch.Target, ch.Operations))
		}
		writeJSON(w, 200, map[string]any{"userKey": u.Key, "userStatus": status, "evaluatedAt": now, "results": results})
	default:
		apiErr(w, 404, "unknown plugin path")
	}
}
