package bot

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ergochat/irc-go/ircmsg"

	"eggbot/internal/flags"
	"eggbot/internal/github"
	"eggbot/internal/llm"
	"eggbot/internal/origin"
	"eggbot/internal/queue"
)

func TestTopicClearedOn331(t *testing.T) {
	b := testBot(t)
	b.Chans.LoadOrCreate("#lobby", b.ChanSet("#lobby"), "", "", "", "")
	b.onIRC(ircmsg.MakeMessage(nil, "eggbot!u@h", "JOIN", "#lobby"))
	b.onIRC(ircmsg.MakeMessage(nil, "srv", "332", "eggbot", "#lobby", "https://github.com/chonkbase/chonkbase"))
	if b.channelTopic("#lobby") == "" {
		t.Fatal("expected topic")
	}
	b.onIRC(ircmsg.MakeMessage(nil, "srv", "331", "eggbot", "#lobby", "No topic is set"))
	if got := b.channelTopic("#lobby"); got != "" {
		t.Fatalf("stale topic %q", got)
	}
	if c := b.Chans.Get("#lobby"); c != nil && c.Topic != "" {
		t.Fatalf("persisted topic %q", c.Topic)
	}
}

func TestLobbyTranscriptUsesTopicAndPlainIRC(t *testing.T) {
	var lastPrompt string
	llmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		if r.URL.Path == "/responses" {
			t.Errorf("github lookup should skip web search, got /responses")
			http.Error(w, "no search", 500)
			return
		}
		sys, lastUser := "", ""
		if msgs, ok := req["messages"].([]any); ok {
			for _, m := range msgs {
				mm, _ := m.(map[string]any)
				c, _ := mm["content"].(string)
				if mm["role"] == "system" {
					sys = c
				}
				if mm["role"] == "user" {
					lastUser = c
				}
				if c != "" {
					lastPrompt += c + "\n"
				}
			}
		}
		q := lastUser
		if strings.HasPrefix(q, "Message:") {
			q = strings.TrimPrefix(q, "Message:")
			if i := strings.Index(q, "\n\n"); i > 0 {
				q = q[:i]
			}
		} else if i := strings.LastIndex(q, "nate:"); i >= 0 {
			q = q[i:]
		}
		content := "Chonkbase latest is [notes](https://github.com/chonkbase/chonkbase) **v1.2.3** (2026-08-04). [[1]](https://github.com/chonkbase/chonkbase/blob/master/CHANGELOG.md) <eos>\x02"
		if strings.Contains(sys, "silent router") {
			content = "SEARCH"
			if strings.Contains(strings.ToLower(q), "capital of france") {
				content = "LOCAL"
			}
		} else if strings.Contains(strings.ToLower(q), "capital of france") {
			content = "Paris."
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]any{"role": "assistant", "content": content},
			}},
		})
	}))
	t.Cleanup(llmSrv.Close)

	changelog := base64.StdEncoding.EncodeToString([]byte("## v1.2.3\nsecurity patches\n"))
	ghSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/repos/chonkbase/chonkbase") && !strings.Contains(r.URL.Path, "/contents/") && !strings.Contains(r.URL.Path, "/releases/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"html_url": "https://github.com/chonkbase/chonkbase", "default_branch": "master"})
		case strings.Contains(r.URL.Path, "/releases/latest"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v1.2.3", "published_at": "2026-08-04T00:00:00Z",
				"html_url": "https://github.com/chonkbase/chonkbase/releases/tag/v1.2.3",
				"body":     "security patches",
			})
		case strings.Contains(r.URL.Path, "/contents/CHANGELOG.md"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"html_url": "https://github.com/chonkbase/chonkbase/blob/master/CHANGELOG.md",
				"encoding": "base64", "content": changelog, "size": 20,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ghSrv.Close)

	b := testBot(t)
	b.Cfg.LLM.Enabled = true
	b.Cfg.LLM.Search = true
	b.Cfg.LLM.APIKey = "test"
	b.LLM.Client.BaseURL = llmSrv.URL
	b.LLM.Client.APIKey = "test"
	b.GitHub = github.NewClient(ghSrv.URL, "", 0)
	cs, _ := flags.ParseChanSet("+ai +aitools")
	b.Chans.LoadOrCreate("#lobby", cs, "", "", "", "")
	b.onIRC(ircmsg.MakeMessage(nil, "eggbot!u@h", "JOIN", "#lobby"))
	b.onIRC(ircmsg.MakeMessage(nil, "nate!n@h", "JOIN", "#lobby"))
	topic := "chonkbase irc — https://github.com/chonkbase/chonkbase"
	b.onIRC(ircmsg.MakeMessage(nil, "srv", "332", "eggbot", "#lobby", topic))
	if b.channelTopic("#lobby") != topic {
		t.Fatalf("topic %q", b.channelTopic("#lobby"))
	}

	u := b.Getuser("nate")
	o := origin.Origin{Nick: "nate", User: "n", Host: "h", Handle: "nate"}
	b.LLM.Remember("#lobby", "nate", "!ask what's the capital of france?")
	b.askLLM("#lobby", o, u, "what's the capital of france?", llm.DestChannel)

	lastPrompt = ""
	b.LLM.Remember("#lobby", "nate", "!ask latest changelog for this IRC server on GitHub")
	b.askLLM("#lobby", o, u, "latest changelog for this IRC server on GitHub", llm.DestChannel)
	if !strings.Contains(lastPrompt, "github.com/chonkbase/chonkbase") {
		t.Fatalf("topic/repo missing from prompt: %s", lastPrompt)
	}
	if !strings.Contains(lastPrompt, "Untrusted IRC context") {
		t.Fatal("context not labeled untrusted")
	}
	if i := strings.LastIndex(lastPrompt, "nate: latest changelog"); i < 0 || strings.LastIndex(lastPrompt, "Channel topic:") > i {
		t.Fatal("question should be after topic context")
	}

	lastPrompt = ""
	b.LLM.Remember("#lobby", "nate", "eggbot: oof, its chonkbase, not ergo")
	b.askLLM("#lobby", o, u, "oof, its chonkbase, not ergo", llm.DestChannel)

	lastPrompt = ""
	b.LLM.Remember("#lobby", "nate", "eggbot: just look at the channel topic")
	b.askLLM("#lobby", o, u, "just look at the channel topic, that should help", llm.DestChannel)
	if !strings.Contains(lastPrompt, topic) && !strings.Contains(lastPrompt, "github.com/chonkbase/chonkbase") {
		t.Fatalf("topic follow-up missing repo: %s", lastPrompt)
	}

	var joined strings.Builder
	for _, line := range b.IRC.Queue().Pending() {
		joined.WriteString(line)
		joined.WriteByte('\n')
		if len(line) > queue.DefaultConfig().MaxLine+80 {
			t.Fatalf("oversize %q", line)
		}
		if !utf8.ValidString(line) {
			t.Fatalf("utf8 %q", line)
		}
		if strings.ContainsAny(line, "\x00\x01\x02\x03\r\n") {
			t.Fatalf("controls %q", line)
		}
	}
	out := joined.String()
	if strings.Contains(out, "](") || strings.Contains(out, "<eos>") || strings.Contains(out, "**") {
		t.Fatalf("markdown survived: %s", out)
	}
	if strings.Contains(strings.ToLower(out), "ergo") && !strings.Contains(strings.ToLower(out), "chonkbase") {
		t.Fatalf("wrong project: %s", out)
	}
	if !strings.Contains(out, "chonkbase") {
		t.Fatalf("expected chonkbase answer: %s", out)
	}
}
