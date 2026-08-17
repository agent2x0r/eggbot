package llm

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRespondExtractsTextAndSearchTool(t *testing.T) {
	var sawSearch bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("path %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		tools, _ := req["tools"].([]any)
		var sawX bool
		for _, t0 := range tools {
			m, _ := t0.(map[string]any)
			if m["type"] == "web_search" {
				sawSearch = true
			}
			if m["type"] == "x_search" {
				sawX = true
			}
		}
		if !sawX {
			t.Error("expected x_search tool")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "resp_1",
			"output": []any{
				map[string]any{
					"type": "message",
					"role": "assistant",
					"content": []any{
						map[string]any{"type": "output_text", "text": "repo is github.com/iconidentify/chonkline"},
					},
				},
			},
		})
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "test", "grok-4.6", 0)
	text, calls, id, err := c.Respond([]Message{{Role: "user", Content: "find rustirc"}}, nil, true, "")
	if err != nil {
		t.Fatal(err)
	}
	if !sawSearch || text == "" || id != "resp_1" || len(calls) != 0 {
		t.Fatalf("search=%v text=%q id=%s calls=%d", sawSearch, text, id, len(calls))
	}
}

func TestRespondRejectsHTMLError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte("<html>nope</html>"))
	}))
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL, "test", "grok-4.6", 0)
	_, _, _, err := c.Respond([]Message{{Role: "user", Content: "hi"}}, nil, true, "")
	if err == nil {
		t.Fatal("expected error")
	}
	var pe *ProviderError
	if !errors.As(err, &pe) || pe.Status != 500 {
		t.Fatalf("want ProviderError 500, got %v", err)
	}
}
