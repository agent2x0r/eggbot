package bot

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ergochat/irc-go/ircmsg"

	"eggbot/internal/flags"
	"eggbot/internal/llm"
	"eggbot/internal/origin"
)

func TestStickyMapPolicy(t *testing.T) {
	m := newSticky()
	now := time.Unix(1_700_000_000, 0)
	o := origin.Origin{Nick: "nate"}
	m.open("#lobby", "nate", o, "nate", now)

	if _, kind := m.consider("#lobby", "eve", "what about x?", now); kind != stickyNone {
		t.Fatal("other nick has no window")
	}
	prompt, kind := m.consider("#lobby", "nate", "its for chrisk just say hi", now.Add(time.Second))
	if kind != stickyYes || prompt != "its for chrisk just say hi" {
		t.Fatalf("follow-up %q %v", prompt, kind)
	}
	if _, kind := m.consider("#lobby", "nate", "lol", now.Add(2*time.Second)); kind != stickyYes {
		t.Fatal("lol still goes to the model")
	}
	if _, kind := m.consider("#lobby", "nate", "alice: later", now.Add(3*time.Second)); kind != stickyYes {
		t.Fatal("nick: prefix still goes to the model")
	}

	m.open("#lobby", "nate", o, "nate", now)
	if _, kind := m.consider("#lobby", "nate", "still here?", now.Add(121*time.Second)); kind != stickyNone {
		t.Fatal("idle close")
	}

	m.open("#a", "nate", o, "nate", now)
	m.open("#a", "bob", origin.Origin{Nick: "bob"}, "", now)
	if _, kind := m.consider("#a", "bob", "what about b?", now.Add(time.Second)); kind != stickyYes {
		t.Fatal("bob window")
	}
	m.onNick("nate", "nathan")
	if _, kind := m.consider("#a", "nathan", "and then?", now.Add(2*time.Second)); kind != stickyYes {
		t.Fatal("nick change should rekey")
	}
	m.dropNick("bob")
	if _, kind := m.consider("#a", "bob", "what about b?", now.Add(3*time.Second)); kind != stickyNone {
		t.Fatal("drop nick")
	}

	m.open("#lobby", "nate", o, "nate", now)
	if !m.pause("#lobby", "nate", "held question?", o, "nate", now.Add(time.Minute), now) {
		t.Fatal("first pause should warn")
	}
	if m.pause("#lobby", "nate", "newer question?", o, "nate", now.Add(time.Minute), now) {
		t.Fatal("replace should be silent")
	}
	if !m.holding("#lobby", "nate") {
		t.Fatal("holding")
	}
	p, _, _, ok := m.takeHold("#lobby", "nate", 0, now.Add(time.Minute))
	if !ok || p != "newer question?" {
		t.Fatalf("takeHold %q %v", p, ok)
	}
	if m.holding("#lobby", "nate") {
		t.Fatal("hold consumed")
	}
}

func TestStickyFollowUpHitsModel(t *testing.T) {
	var asks atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		asks.Add(1)
		content := "ok"
		if strings.Contains(strings.ToLower(string(body)), "silent router") {
			content = "LOCAL"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": content}}},
		})
	}))
	t.Cleanup(srv.Close)

	b := testBot(t)
	b.Cfg.LLM.Enabled = true
	b.Cfg.LLM.Sticky = true
	b.Cfg.LLM.APIKey = "test"
	b.Cfg.LLM.Search = false
	b.Cfg.LLM.Limits.PerUserPerMin = 20
	b.Cfg.LLM.Limits.PerChannelPerMin = 20
	b.LLM.Client.BaseURL = srv.URL
	b.LLM.Client.APIKey = "test"
	cs, _ := flags.ParseChanSet("+ai")
	b.Chans.LoadOrCreate("#lobby", cs, "", "", "", "")
	b.onIRC(ircmsg.MakeMessage(nil, "eggbot!u@h", "JOIN", "#lobby"))
	b.onIRC(ircmsg.MakeMessage(nil, "nate!n@h", "JOIN", "#lobby"))
	b.onIRC(ircmsg.MakeMessage(nil, "nate!n@h", "PRIVMSG", "#lobby", "eggbot: capital of france"))
	waitAsks(t, &asks, 1)
	b.onIRC(ircmsg.MakeMessage(nil, "nate!n@h", "PRIVMSG", "#lobby", "its for chrisk just say hi"))
	waitAsks(t, &asks, 2)
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "JOIN", "#lobby"))
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "#lobby", "what about spain?"))
	time.Sleep(50 * time.Millisecond)
	if asks.Load() != 2 {
		t.Fatalf("eve should not follow up, asks=%d", asks.Load())
	}
}

func TestStickyDisabledIgnoresFollowUp(t *testing.T) {
	var asks atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asks.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "ok"}}},
		})
	}))
	t.Cleanup(srv.Close)

	b := testBot(t)
	b.Cfg.LLM.Enabled = true
	b.Cfg.LLM.Sticky = false
	b.Cfg.LLM.APIKey = "test"
	b.Cfg.LLM.Search = false
	b.Cfg.LLM.Limits.PerUserPerMin = 20
	b.Cfg.LLM.Limits.PerChannelPerMin = 20
	b.LLM.Client.BaseURL = srv.URL
	b.LLM.Client.APIKey = "test"
	cs, _ := flags.ParseChanSet("+ai")
	b.Chans.LoadOrCreate("#lobby", cs, "", "", "", "")
	b.onIRC(ircmsg.MakeMessage(nil, "eggbot!u@h", "JOIN", "#lobby"))
	b.onIRC(ircmsg.MakeMessage(nil, "nate!n@h", "JOIN", "#lobby"))
	b.onIRC(ircmsg.MakeMessage(nil, "nate!n@h", "PRIVMSG", "#lobby", "eggbot: capital of france"))
	waitAsks(t, &asks, 1)
	b.onIRC(ircmsg.MakeMessage(nil, "nate!n@h", "PRIVMSG", "#lobby", "what about italy?"))
	time.Sleep(80 * time.Millisecond)
	if asks.Load() != 1 {
		t.Fatalf("sticky false should ignore follow-up, asks=%d", asks.Load())
	}
}

func TestStickyRateLimitPausesHold(t *testing.T) {
	var asks atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asks.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "ok"}}},
		})
	}))
	t.Cleanup(srv.Close)

	b := testBot(t)
	b.Cfg.LLM.Enabled = true
	b.Cfg.LLM.Sticky = true
	b.Cfg.LLM.APIKey = "test"
	b.Cfg.LLM.Search = false
	b.Cfg.LLM.Limits.PerUserPerMin = 1
	b.Cfg.LLM.Limits.PerChannelPerMin = 20
	b.Cfg.LLM.Limits.OwnerUnlimited = false
	b.LLM.Client.BaseURL = srv.URL
	b.LLM.Client.APIKey = "test"
	cs, _ := flags.ParseChanSet("+ai")
	b.Chans.LoadOrCreate("#lobby", cs, "", "", "", "")
	b.onIRC(ircmsg.MakeMessage(nil, "eggbot!u@h", "JOIN", "#lobby"))
	b.onIRC(ircmsg.MakeMessage(nil, "nate!n@h", "JOIN", "#lobby"))
	b.onIRC(ircmsg.MakeMessage(nil, "nate!n@h", "PRIVMSG", "#lobby", "eggbot: one"))
	waitAsks(t, &asks, 1)
	before := asks.Load()
	b.onIRC(ircmsg.MakeMessage(nil, "nate!n@h", "PRIVMSG", "#lobby", "and the sequel?"))
	time.Sleep(80 * time.Millisecond)
	if asks.Load() != before {
		t.Fatal("rate-limited follow-up should not call the model")
	}
	if !b.sticky.holding("#lobby", "nate") {
		t.Fatal("expected paused hold")
	}
	p, o, _, ok := b.sticky.takeHold("#lobby", "nate", 0, b.now())
	if !ok || p != "and the sequel?" {
		t.Fatalf("held %q %v", p, ok)
	}
	b.Cfg.LLM.Limits.PerUserPerMin = 20
	b.askLLM("#lobby", o, nil, p, llm.DestChannel)
	waitAsks(t, &asks, before+1)
}

func waitAsks(t *testing.T, n *atomic.Int32, want int32) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if n.Load() >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("asks=%d want %d", n.Load(), want)
}

func TestStickyBangDoesNotFollowUp(t *testing.T) {
	b := testBot(t)
	b.Cfg.LLM.Enabled = true
	b.Cfg.LLM.Sticky = true
	cs, _ := flags.ParseChanSet("+ai +seen")
	b.Chans.LoadOrCreate("#lobby", cs, "", "", "", "")
	b.onIRC(ircmsg.MakeMessage(nil, "eggbot!u@h", "JOIN", "#lobby"))
	b.onIRC(ircmsg.MakeMessage(nil, "nate!n@h", "JOIN", "#lobby"))
	b.sticky.open("#lobby", "nate", origin.Origin{Nick: "nate"}, "nate", b.now())
	b.onIRC(ircmsg.MakeMessage(nil, "nate!n@h", "PRIVMSG", "#lobby", "!seen eve"))
	b.sticky.mu.Lock()
	s := b.sticky.sess[stickyKey("#lobby", "nate")]
	b.sticky.mu.Unlock()
	if s == nil {
		t.Fatal("!seen should not close the window")
	}
}
