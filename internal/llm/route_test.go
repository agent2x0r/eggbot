package llm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"eggbot/internal/config"
)

func TestRouteLocalSearchAndFallback(t *testing.T) {
	if parseRoute("SEARCH") != RouteSearch || parseRoute("LOCAL") != RouteLocal || parseRoute("hmm") != RouteLocal {
		t.Fatal("parse")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "SEARCH"}}},
		})
	}))
	t.Cleanup(srv.Close)
	cfg := config.Defaults()
	cfg.LLM.BaseURL = srv.URL
	cfg.LLM.APIKey = "test"
	cfg.LLM.Enabled = true
	b := NewBrain(cfg, nil, &fakeActor{}, nil)
	if b.Route("", "") != RouteLocal {
		t.Fatal("empty")
	}
	if b.Route("2+2", "") != RouteLocal {
		t.Fatal("obvious")
	}
	if b.Route("is this on github today", "") != RouteSearch {
		t.Fatal("search")
	}
	fail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no", 500)
	}))
	t.Cleanup(fail.Close)
	b.Client.BaseURL = fail.URL
	if b.Route("what is a mutex", "") != RouteLocal {
		t.Fatal("fallback local")
	}
	if b.Route("latest news about rustirc", "") != RouteSearch {
		t.Fatal("fallback search")
	}
}
