package oauthserver

import "testing"

func TestValidRedirect(t *testing.T) {
	for _, ok := range []string{"http://127.0.0.1:33418/callback", "http://localhost/callback", "https://app.example.com/cb", "cursor://anysphere.cursor-retrieval/oauth", "com.example.app:/cb"} {
		if err := ValidRedirect(ok); err != nil {
			t.Errorf("%s rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"http://evil.example.com/cb", "javascript:alert(1)", "http://127.0.0.1/cb#frag", "", "file:///etc/passwd"} {
		if err := ValidRedirect(bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

func TestMatchRedirectLoopbackAnyPort(t *testing.T) {
	c := &Client{RedirectURIs: []string{"http://localhost:33418/callback"}}
	if !c.CheckRedirect("http://localhost:51234/callback") {
		t.Fatal("loopback with another port must match (RFC 8252)")
	}
	if c.CheckRedirect("http://localhost:51234/other") {
		t.Fatal("different path matched")
	}
	if c.CheckRedirect("http://127.0.0.1:51234/callback") {
		t.Fatal("different loopback host matched")
	}
	h := &Client{RedirectURIs: []string{"https://app.example.com/cb"}}
	if h.CheckRedirect("https://app.example.com/cb2") {
		t.Fatal("https must match exactly")
	}
}

func TestSameResource(t *testing.T) {
	if !sameResource("https://g.example.com/mcp", "https://g.example.com") || sameResource("https://g.example.com/mcp", "https://other.example.com/mcp") {
		t.Fatal("resource comparison")
	}
}
