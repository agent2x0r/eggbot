package llm

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"eggbot/internal/config"
	"eggbot/internal/flags"
	"eggbot/internal/userfile"
)

type fakeActor struct {
	denied bool
	ran    []string
}

func (f *fakeActor) MatchAttr(handle, expr, channel string) bool {
	if f.denied {
		return expr == "-" || expr == ""
	}
	return true
}

func (f *fakeActor) RunTool(name string, ctx ToolCtx, args map[string]any) (string, error) {
	f.ran = append(f.ran, name)
	return "ok", nil
}

func TestChatAndTools(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		if calls == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []any{map[string]any{
					"message": map[string]any{
						"role": "assistant",
						"tool_calls": []any{map[string]any{
							"id":   "1",
							"type": "function",
							"function": map[string]any{
								"name":      "seen_lookup",
								"arguments": `{"nick":"nate"}`,
							},
						}},
					},
				}},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]any{"role": "assistant", "content": "nate was here"},
			}},
		})
	}))
	defer srv.Close()

	cfg := config.Defaults()
	cfg.LLM.BaseURL = srv.URL
	cfg.LLM.APIKey = "test"
	cfg.LLM.Enabled = true
	actor := &fakeActor{}
	tools := []ToolDef{{
		Spec:     FuncTool("seen_lookup", "seen", Schema(map[string]any{"nick": Prop("string", "n")}, []string{"nick"})),
		MinFlags: "-",
	}}
	br := NewBrain(cfg, nil, actor, tools)
	reply, err := br.Ask("#c", "bob", "bob", &userfile.User{Handle: "bob"}, "", "where's nate?", true)
	if err != nil {
		t.Fatal(err)
	}
	if reply != "nate was here" {
		t.Fatalf("reply %q", reply)
	}
	if len(actor.ran) != 1 || actor.ran[0] != "seen_lookup" {
		t.Fatalf("tools %v", actor.ran)
	}
	_, _ = br.Client.Chat([]Message{{Role: "user", Content: "hi"}}, nil)
	br.Remember("#c", "bob", "hello")
	_ = br.Tail("#c", 2)
	_ = br.Tail("#c", 0)
}

func TestToolDenied(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]any{"role": "assistant", "content": "ok"},
			}},
		})
	}))
	defer srv.Close()
	cfg := config.Defaults()
	cfg.LLM.BaseURL = srv.URL
	cfg.LLM.APIKey = "test"
	actor := &fakeActor{denied: true}
	tools := []ToolDef{{
		Spec:     FuncTool("kick", "kick", Schema(map[string]any{"nick": Prop("string", "n")}, []string{"nick"})),
		MinFlags: "o",
	}}
	br := NewBrain(cfg, nil, actor, tools)
	// kick should not even be offered; just a normal ask
	if _, err := br.Ask("#c", "bob", "bob", nil, "", "hi", true); err != nil {
		t.Fatal(err)
	}
	if len(actor.ran) != 0 {
		t.Fatalf("should not run kick: %v", actor.ran)
	}
}

func TestRateLimit(t *testing.T) {
	cfg := config.Defaults()
	cfg.LLM.APIKey = "x"
	cfg.LLM.Limits.PerUserPerMin = 1
	cfg.LLM.Limits.PerChannelPerMin = 10
	cfg.LLM.Limits.OwnerUnlimited = true
	br := NewBrain(cfg, nil, &fakeActor{}, nil)
	req := AskReq{Handle: "a", Nick: "a", Channel: "#c"}
	if err := br.rateOK(req); err != nil {
		t.Fatal("first allowed", err)
	}
	if err := br.rateOK(req); err == nil {
		t.Fatal("second should deny")
	}
	owner := AskReq{Handle: "nate", Nick: "nate", Channel: "#c", User: &userfile.User{Handle: "nate", Global: flags.Parse("n")}}
	if err := br.rateOK(owner); err != nil {
		t.Fatal("owner should skip limit", err)
	}
	if err := br.rateOK(owner); err != nil {
		t.Fatal("owner still unlimited", err)
	}
	time.Sleep(0)
}

func TestRejectedChannelDoesNotConsumeUserRate(t *testing.T) {
	cfg := config.Defaults()
	cfg.LLM.Limits.PerUserPerMin = 1
	cfg.LLM.Limits.PerChannelPerMin = 1
	br := NewBrain(cfg, nil, &fakeActor{}, nil)

	if err := br.rateOK(AskReq{Nick: "alice", Channel: "#busy"}); err != nil {
		t.Fatal(err)
	}
	if err := br.rateOK(AskReq{Nick: "bob", Channel: "#busy"}); err == nil {
		t.Fatal("expected busy channel rejection")
	}
	if err := br.rateOK(AskReq{Nick: "bob", Channel: "#other"}); err != nil {
		t.Fatalf("rejected request consumed bob's user quota: %v", err)
	}
}

func TestAdmitHasGlobalCapacityLimit(t *testing.T) {
	cfg := config.Defaults()
	cfg.LLM.Enabled = true
	cfg.LLM.APIKey = "test"
	cfg.LLM.Limits.OwnerUnlimited = true
	br := NewBrain(cfg, nil, &fakeActor{}, nil)
	owner := &userfile.User{Handle: "owner", Global: flags.Parse("n")}

	for i := 0; i < maxAskJobs; i++ {
		req := AskReq{Nick: fmt.Sprintf("owner%d", i), User: owner}
		if err := br.Admit(&req); err != nil {
			t.Fatalf("reservation %d failed: %v", i+1, err)
		}
	}
	req := AskReq{Nick: "overflow"}
	if err := br.Admit(&req); err == nil {
		t.Fatal("expected global capacity rejection")
	}
	br.finishFlight("unused")
	if err := br.Admit(&req); err != nil {
		t.Fatalf("capacity rejection consumed the user's rate quota: %v", err)
	}
}

func TestIsCatchup(t *testing.T) {
	if !IsCatchup("catch me up") || !IsCatchup("What did I miss?") || !IsCatchup("summary") {
		t.Fatal("expected catchup")
	}
	if !IsCatchup("can you summarize what we've been doing for chrisk?") {
		t.Fatal("room recap for a nick")
	}
	if !IsCatchup("catch chrisk up") || !IsCatchup("bring chrisk up to speed") {
		t.Fatal("catch nick up")
	}
	if IsCatchup("what is rust") {
		t.Fatal("false positive")
	}
	if IsCatchup("tell us just a brief summary of this rust IRC project") {
		t.Fatal("project summary is not a room recap")
	}
}

func TestParseRoute(t *testing.T) {
	if parseRoute("SEARCH") != RouteSearch || parseRoute("search.") != RouteSearch {
		t.Fatal("search")
	}
	if parseRoute("LOCAL") != RouteLocal || parseRoute("local") != RouteLocal {
		t.Fatal("local")
	}
	if parseRoute("RECAP") != RouteLocal || parseRoute("catchup") != RouteLocal {
		t.Fatal("recap is local")
	}
}

func TestNeedsSearch(t *testing.T) {
	if NeedsSearch("capital of france") || NeedsSearch("square root of 16") || NeedsSearch("what is a mutex") {
		t.Fatal("trivia should be local")
	}
	if !NeedsSearch("how new is this rust irc server") || !NeedsSearch("is it public now") {
		t.Fatal("live questions should search")
	}
}

func TestObviousLocal(t *testing.T) {
	if !ObviousLocal("capital of france") || !ObviousLocal("square root of 16") || !ObviousLocal("2+2") {
		t.Fatal("trivia should skip the router")
	}
	if ObviousLocal("what is chonkline") || ObviousLocal("is it on github") || ObviousLocal("who is chrisk") {
		t.Fatal("live lookups must still go through the router")
	}
}

func TestStripCitations(t *testing.T) {
	in := "It's hours old, written overnight with a local LLM. Yes: https://github.com/iconidentify/chonkline display render_inline_citation with citation_id is 65 display render_inline_citation with citation_id is 66"
	got := normalizeIRC(in)
	want := "It's hours old, written overnight with a local LLM. Yes: https://github.com/iconidentify/chonkline"
	if got != want {
		t.Fatalf("got %q", got)
	}
	tagged := "see it " + "\x3cgrok:render type=\"render_inline_citation\" citation_id=\"65\"\x3e" + "junk" + "\x3c/grok:render\x3e" + " now"
	if g := normalizeIRC(tagged); g != "see it now" {
		t.Fatalf("tagged %q", g)
	}
}

func TestAskQueueFIFO(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var n int
	var answers = []string{"one", "two", "three"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n == 1 {
			close(started)
			<-release
		}
		msg := answers[n-1]
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]any{"role": "assistant", "content": msg},
			}},
		})
	}))
	defer srv.Close()
	cfg := config.Defaults()
	cfg.LLM.BaseURL = srv.URL
	cfg.LLM.APIKey = "test"
	cfg.LLM.Limits.OwnerUnlimited = true
	br := NewBrain(cfg, nil, &fakeActor{}, nil)
	owner := &userfile.User{Handle: "nate", Global: flags.Parse("n")}

	type res struct {
		text string
		err  error
	}
	out := make([]chan res, 3)
	for i := range out {
		out[i] = make(chan res, 1)
	}
	go func() {
		t, e := br.AskReq(AskReq{Channel: "#c", Nick: "nate", Handle: "nate", User: owner, Prompt: "q1"})
		out[0] <- res{t, e}
	}()
	<-started
	go func() {
		t, e := br.AskReq(AskReq{Channel: "#c", Nick: "nate", Handle: "nate", User: owner, Prompt: "q2"})
		out[1] <- res{t, e}
	}()
	waitQueued(t, br, "nate", 1)
	go func() {
		t, e := br.AskReq(AskReq{Channel: "#c", Nick: "nate", Handle: "nate", User: owner, Prompt: "q3"})
		out[2] <- res{t, e}
	}()
	waitQueued(t, br, "nate", 2)
	close(release)

	got := make([]string, 3)
	for i := 0; i < 3; i++ {
		r := <-out[i]
		if r.err != nil {
			t.Fatalf("ask %d: %v", i+1, r.err)
		}
		got[i] = r.text
	}
	if got[0] != "one" || got[1] != "two" || got[2] != "three" {
		t.Fatalf("fifo order %v", got)
	}
}

func TestClaimSearchAckOncePerSession(t *testing.T) {
	cfg := config.Defaults()
	cfg.LLM.APIKey = "x"
	br := NewBrain(cfg, nil, &fakeActor{}, nil)
	key := flightKey(AskReq{Handle: "nate"})
	if br.ClaimSearchAck("nate", "nate") {
		t.Fatal("not inflight")
	}
	br.mu.Lock()
	br.inflight[key] = true
	br.mu.Unlock()
	if !br.ClaimSearchAck("nate", "nate") {
		t.Fatal("first should claim")
	}
	if br.ClaimSearchAck("nate", "nate") {
		t.Fatal("second must not claim")
	}
	br.finishFlight(key)
	br.mu.Lock()
	br.inflight[key] = true
	br.mu.Unlock()
	if !br.ClaimSearchAck("nate", "nate") {
		t.Fatal("new session should claim again")
	}
}

func waitQueued(t *testing.T, br *Brain, handle string, n int) {
	t.Helper()
	deadline := time.After(200 * time.Millisecond)
	key := flightKey(AskReq{Handle: handle})
	for {
		br.mu.Lock()
		queued := len(br.queue[key])
		br.mu.Unlock()
		if queued >= n {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("queued=%d want >= %d", queued, n)
		case <-time.After(5 * time.Millisecond):
		}
	}
}
