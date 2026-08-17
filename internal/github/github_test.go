package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseAndFind(t *testing.T) {
	owner, repo, ok := Parse("https://github.com/chonkbase/chonkbase/blob/master/CHANGELOG.md")
	if !ok || owner != "chonkbase" || repo != "chonkbase" {
		t.Fatalf("%s %s %v", owner, repo, ok)
	}
	owner, repo, ok = Parse("github.com/ergochat/ergo.git")
	if !ok || owner != "ergochat" || repo != "ergo" {
		t.Fatalf("%s %s %v", owner, repo, ok)
	}
	owner, repo, ok = Find("see the topic: https://github.com/chonkbase/chonkbase and also https://github.com/ergochat/ergo")
	if !ok || owner != "chonkbase" || repo != "chonkbase" {
		t.Fatalf("first %s/%s", owner, repo)
	}
	if _, _, ok := Find("no links here"); ok {
		t.Fatal("expected miss")
	}
	if _, _, ok := Parse("https://evil.example/github.com/foo/bar"); ok {
		t.Fatal("wrong host")
	}
}

func TestLookup(t *testing.T) {
	changelog := base64.StdEncoding.EncodeToString([]byte("## 1.2.3\nsecurity patches\n"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/repos/chonkbase/chonkbase") && !strings.Contains(r.URL.Path, "/contents/") && !strings.Contains(r.URL.Path, "/releases/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"html_url": "https://github.com/chonkbase/chonkbase", "default_branch": "master",
			})
		case strings.Contains(r.URL.Path, "/releases/latest"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v1.2.3", "name": "1.2.3", "published_at": "2026-08-04T00:00:00Z",
				"html_url": "https://github.com/chonkbase/chonkbase/releases/tag/v1.2.3",
				"body":     "security patches",
			})
		case strings.Contains(r.URL.Path, "/contents/CHANGELOG.md"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"html_url": "https://github.com/chonkbase/chonkbase/blob/master/CHANGELOG.md",
				"encoding": "base64", "content": changelog, "size": 32,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL, "", 0)
	info, err := c.Lookup(context.Background(), "https://github.com/chonkbase/chonkbase")
	if err != nil {
		t.Fatal(err)
	}
	out := info.Format()
	if !strings.Contains(out, "v1.2.3") || !strings.Contains(out, "2026-08-04") {
		t.Fatalf("%s", out)
	}
	if !strings.Contains(out, "https://github.com/chonkbase/chonkbase") {
		t.Fatalf("url %s", out)
	}
	if !strings.Contains(out, "changelog_url:") {
		t.Fatalf("changelog %s", out)
	}
}

func TestLookupRejectsBadSpec(t *testing.T) {
	c := NewClient("http://127.0.0.1:1", "", 0)
	if _, err := c.Lookup(context.Background(), "not a repo"); err == nil {
		t.Fatal("expected error")
	}
}
