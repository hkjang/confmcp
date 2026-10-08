package confluence

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hkjang/confmcp/internal/settings"
)

var cred = Credential{Mode: "service", Username: "svc", Password: "pw"}

func client(t *testing.T, h http.HandlerFunc, timeout int) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := NewClient(settings.Confluence{BaseURL: srv.URL + "/confluence", TimeoutSec: timeout})
	if err != nil {
		t.Fatal(err)
	}
	return c, srv
}

func TestContextPathAndKoreanTitle(t *testing.T) {
	var gotPath, gotQuery string
	c, _ := client(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"id":"1","type":"page","status":"current","title":"한글 제목","_links":{"webui":"/pages/viewpage.action?pageId=1"}}],"start":0,"limit":25,"size":1,"_links":{}}`))
	}, 5)
	list, _, err := c.ContentList(context.Background(), cred, ContentQuery{SpaceKey: "DEV", Title: "한글 제목"})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/confluence/rest/api/content" || !strings.Contains(gotQuery, "title=%ED%95%9C%EA%B8%80") {
		t.Fatalf("path=%s query=%s", gotPath, gotQuery)
	}
	if u := c.WebURL(list[0].WebPath); !strings.HasSuffix(u, "/confluence/pages/viewpage.action?pageId=1") {
		t.Fatalf("web url %s", u)
	}
}

func TestLoginHTMLIsNotASuccess(t *testing.T) {
	c, _ := client(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><title>Log In - Confluence</title></html>"))
	}, 5)
	if _, err := c.Content(context.Background(), cred, "1"); !errors.Is(err, ErrAuthPage) {
		t.Fatalf("want ErrAuthPage, got %v", err)
	}
}

func TestSSORedirectIsRefused(t *testing.T) {
	c, _ := client(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://sso.example.com/login", http.StatusFound)
	}, 5)
	if _, err := c.Content(context.Background(), cred, "1"); !errors.Is(err, ErrAuthPage) {
		t.Fatalf("want ErrAuthPage, got %v", err)
	}
}

func TestWriteTimeoutIsOutcomeUnknown(t *testing.T) {
	c, _ := client(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1500 * time.Millisecond)
	}, 1)
	_, err := c.CreateContent(context.Background(), cred, NewContent{Type: "page", Title: "x", SpaceKey: "DEV", Storage: "<p/>"})
	if !errors.Is(err, ErrOutcomeUnknown) {
		t.Fatalf("want ErrOutcomeUnknown, got %v", err)
	}
}

func TestConflictSurfacesAs409(t *testing.T) {
	c, _ := client(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(409)
		_, _ = w.Write([]byte(`{"statusCode":409,"message":"Version must be incremented"}`))
	}, 5)
	_, err := c.UpdateContent(context.Background(), cred, "1", ContentUpdate{Type: "page", Title: "t", NewVersion: 2})
	if !IsStatus(err, 409) {
		t.Fatalf("want 409, got %v", err)
	}
}

func TestDownloadRefusesForeignLinks(t *testing.T) {
	c, _ := client(t, func(w http.ResponseWriter, r *http.Request) {}, 5)
	att := &Content{DownloadPath: "https://evil.example.com/download/attachments/1/x.pdf"}
	if _, _, err := c.Download(context.Background(), cred, att, 1024); err == nil {
		t.Fatal("download link to another host was followed")
	}
}

func TestBodyViewIsNeverRequested(t *testing.T) {
	c, _ := client(t, func(w http.ResponseWriter, r *http.Request) {}, 5)
	if _, err := c.Content(context.Background(), cred, "1", "body.view"); err == nil {
		t.Fatal("body.view was requested")
	}
}

func TestCQLString(t *testing.T) {
	if got := CQLString(`a "b" \c` + "\n"); got != `"a \"b\" \\c "` {
		t.Fatalf("%s", got)
	}
}
