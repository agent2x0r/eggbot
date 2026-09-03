package llm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"eggbot/internal/config"
)

func TestNormalizeIRC(t *testing.T) {
	in := "Ergo latest [[1]](https://github.com/ergochat/ergo/blob/master/CHANGELOG.md) <eos>"
	got := normalizeIRC(in)
	if strings.Contains(got, "[[") || strings.Contains(got, "<eos>") || strings.Contains(got, "](") {
		t.Fatalf("markdown survived: %q", got)
	}
	if strings.Contains(got, "https://") {
		t.Fatalf("citation url leaked: %q", got)
	}
	if got != "Ergo latest" {
		t.Fatalf("cite %q", got)
	}
	glued := normalizeIRC("hey[1](https://irc.chonkbase.net/)")
	if glued != "hey" {
		t.Fatalf("glued cite %q", glued)
	}
	thanks := normalizeIRC("thanks ([1](https://irc.chonkbase.net/))")
	if thanks != "thanks" {
		t.Fatalf("paren cite %q", thanks)
	}
	bare := normalizeIRC("hey [[1]](https://irc.chonkbase.net/)")
	if bare != "hey" {
		t.Fatalf("double-bracket cite %q", bare)
	}

	link := normalizeIRC("see [Project](https://example.test/repo) and **bold**")
	if link != "see Project — https://example.test/repo and bold" {
		t.Fatalf("link %q", link)
	}

	multi := normalizeIRC("[A](https://a.test) then [B](https://b.test).")
	if !strings.Contains(multi, "A — https://a.test") || !strings.Contains(multi, "B — https://b.test") {
		t.Fatalf("multi %q", multi)
	}

	fence := normalizeIRC("```go\nfmt.Println(1)\n```")
	if strings.Contains(fence, "```") || !strings.Contains(fence, "fmt.Println(1)") {
		t.Fatalf("fence %q", fence)
	}

	ctrl := normalizeIRC("hi\x01\x02there\x03" + "12,01 colored\x0f" + " \x04AABBCC end")
	if strings.ContainsAny(ctrl, "\x01\x02\x03\x04\x0f") {
		t.Fatalf("controls survived: %q", ctrl)
	}
	if !utf8.ValidString(ctrl) {
		t.Fatal("utf8")
	}

	uni := normalizeIRC("café [ok](https://ex.test/ä)")
	if !strings.Contains(uni, "café") || !utf8.ValidString(uni) {
		t.Fatalf("unicode %q", uni)
	}

	clipped := clip("see [X](https://example.test/very/long/path/here) trailing", 24)
	if strings.Contains(clipped, "](") || strings.Contains(clipped, "\x01") {
		t.Fatalf("clipped markdown %q", clipped)
	}
}

func TestPromptPutsQuestionLastWithTopic(t *testing.T) {
	cfg := config.Defaults()
	cfg.LLM.APIKey = "x"
	br := NewBrain(cfg, nil, &fakeActor{}, nil)
	br.Remember("#lobby", "nate", "hello")
	br.Remember("#lobby", "nate", "!ask what's the topic?")
	req := AskReq{
		Channel: "#lobby", Nick: "nate", Prompt: "what's the topic?",
		Topic: "chonkbase https://github.com/chonkbase/chonkbase",
		Dest:  DestChannel,
	}
	hist := br.historyFor(req)
	for _, h := range hist {
		if h.Text == "!ask what's the topic?" {
			t.Fatal("trigger line should be dropped")
		}
	}
	msgs := br.buildMessages(req, hist, "", false)
	if len(msgs) < 3 {
		t.Fatalf("msgs %d", len(msgs))
	}
	if msgs[0].Role != "system" {
		t.Fatal("system first")
	}
	ctx := msgs[len(msgs)-2].Content
	if !strings.Contains(ctx, "Untrusted IRC context") {
		t.Fatalf("untrusted label missing: %q", ctx)
	}
	if !strings.Contains(ctx, "github.com/chonkbase/chonkbase") {
		t.Fatalf("topic missing: %q", ctx)
	}
	if !strings.Contains(ctx, "<nate> hello") {
		t.Fatalf("history missing: %q", ctx)
	}
	last := msgs[len(msgs)-1]
	if last.Role != "user" || last.Content != "nate: what's the topic?" {
		t.Fatalf("question last: %+v", last)
	}
}

func TestSearchLookupOmitsRoomScrollback(t *testing.T) {
	cfg := config.Defaults()
	cfg.LLM.APIKey = "x"
	br := NewBrain(cfg, nil, &fakeActor{}, nil)
	br.Remember("#lobby", "nate", "ATX 3.0 PSU transients")
	br.Remember("#lobby", "chrisk", "memtest+")
	req := AskReq{
		Channel: "#lobby", Nick: "nate", Prompt: "chonkstep",
		Topic:  "https://github.com/chonkbase/chonkbase",
		Dest:   DestSearch,
		Search: true,
	}
	hist := br.historyFor(req)
	if len(hist) != 0 {
		t.Fatalf("search hist %d", len(hist))
	}
	msgs := br.buildMessages(req, hist, "", true)
	if len(msgs) != 2 || msgs[1].Content != "chonkstep" {
		t.Fatalf("search msgs %+v", msgs)
	}
	sys := msgs[0].Content
	if strings.Contains(sys, "ATX") || strings.Contains(sys, "chonkbase") {
		t.Fatalf("room leaked into search: %q", sys)
	}
	joined := msgs[0].Content + msgs[1].Content
	if strings.Contains(joined, "nate: chonkstep") {
		t.Fatal("search must not prefix nick:")
	}
	if !strings.Contains(sys, "One or two short IRC sentences") {
		t.Fatal("search brevity missing")
	}
	for _, leak := range []string{"Your build is", "commit=", "!help", "!history", "scrollback", "SILENT", "DROP"} {
		if strings.Contains(sys, leak) {
			t.Fatalf("search prompt still has %q: %s", leak, sys)
		}
	}
}

func TestFollowupRouting(t *testing.T) {
	if !NeedsSearch("latest changelog for this IRC server on GitHub") {
		t.Fatal("changelog")
	}
	if !followupNeedsSearch("oof, its chonkbase, not ergo", "Channel topic: https://github.com/chonkbase/chonkbase changelog") {
		t.Fatal("correction")
	}
	if followupNeedsSearch("thanks", "idle chatter") {
		t.Fatal("false followup")
	}
	if !wantsRepoLookup("just look at the channel topic", nil) {
		t.Fatal("topic")
	}
	hist := []HistoryLine{{Nick: "nate", Text: "latest changelog on GitHub"}}
	if !wantsRepoLookup("oof, its chonkbase, not ergo", hist) {
		t.Fatal("correction wants repo")
	}
}

func TestSearchFailureDoesNotFallback(t *testing.T) {
	var chat bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/responses" {
			http.Error(w, "<html>nope</html>", 500)
			return
		}
		chat = true
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]any{"role": "assistant", "content": "from memory"},
			}},
		})
	}))
	t.Cleanup(srv.Close)
	cfg := config.Defaults()
	cfg.LLM.BaseURL = srv.URL
	cfg.LLM.APIKey = "test"
	cfg.LLM.Enabled = true
	cfg.LLM.Limits.OwnerUnlimited = true
	br := NewBrain(cfg, nil, &fakeActor{}, nil)
	_, err := br.AskReq(AskReq{Channel: "#c", Nick: "nate", Prompt: "latest news", Search: true, Helpful: true})
	if err == nil || !strings.Contains(err.Error(), "couldn't look that up") {
		t.Fatalf("err=%v", err)
	}
	if chat {
		t.Fatal("must not fall back to memory chat")
	}
}
