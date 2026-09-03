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
		if sawX {
			t.Error("x_search should stay off unless asked")
		}
		if _, ok := req["reasoning"]; ok {
			t.Errorf("empty search_reasoning must omit reasoning, got %v", req["reasoning"])
		}
		if n, _ := req["max_output_tokens"].(float64); int(n) != searchMaxOutTokens {
			t.Errorf("max_output_tokens=%v want %d", req["max_output_tokens"], searchMaxOutTokens)
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
	text, calls, id, err := c.Respond([]Message{{Role: "user", Content: "find rustirc"}}, nil, true, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if !sawSearch || text == "" || id != "resp_1" || len(calls) != 0 {
		t.Fatalf("search=%v text=%q id=%s calls=%d", sawSearch, text, id, len(calls))
	}
}

func TestRespondUsesSearchModelAndReasoningFromConfig(t *testing.T) {
	var model string
	var effort any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		model, _ = req["model"].(string)
		if rsn, ok := req["reasoning"].(map[string]any); ok {
			effort = rsn["effort"]
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "s", "output": []any{}})
	}))
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL, "test", "chat-model", 0)
	c.SearchModel = "search-model"
	c.SearchReasoning = "low"
	if _, _, _, err := c.Respond([]Message{{Role: "user", Content: "q"}}, nil, true, false, ""); err != nil {
		t.Fatal(err)
	}
	if model != "search-model" || effort != "low" {
		t.Fatalf("model=%s effort=%v", model, effort)
	}
}

func TestRespondAddsXSearchWhenAsked(t *testing.T) {
	var sawX bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		for _, t0 := range req["tools"].([]any) {
			m, _ := t0.(map[string]any)
			if m["type"] == "x_search" {
				sawX = true
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "x", "output": []any{}})
	}))
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL, "test", "grok-4.6", 0)
	if _, _, _, err := c.Respond([]Message{{Role: "user", Content: "tweet"}}, nil, true, true, ""); err != nil {
		t.Fatal(err)
	}
	if !sawX {
		t.Fatal("expected x_search")
	}
}

func TestWantsXSearch(t *testing.T) {
	if wantsXSearch("chonkstep") || !wantsXSearch("latest tweet about chonkstep") {
		t.Fatal("x search gate")
	}
}

func TestRespondRejectsHTMLError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte("<html>nope</html>"))
	}))
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL, "test", "grok-4.6", 0)
	_, _, _, err := c.Respond([]Message{{Role: "user", Content: "hi"}}, nil, true, false, "")
	if err == nil {
		t.Fatal("expected error")
	}
	var pe *ProviderError
	if !errors.As(err, &pe) || pe.Status != 500 {
		t.Fatalf("want ProviderError 500, got %v", err)
	}
}
