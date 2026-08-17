package integration

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ergochat/irc-go/ircmsg"

	"eggbot/internal/bot"
	"eggbot/internal/config"
	"eggbot/internal/flags"
	"eggbot/internal/github"
	"eggbot/internal/llm"
	"eggbot/internal/queue"
)

func TestIRCdMatrix(t *testing.T) {
	if os.Getenv("EGGBOT_IRCD_MATRIX") == "" {
		t.Skip("set EGGBOT_IRCD_MATRIX=1 and start test/integration/compose.yml")
	}
	t.Log("matrix job is defined in compose.yml; wire live daemons in CI schedule")
}

func TestFakeAskTranscript(t *testing.T) {
	llmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		content := "Chonkbase latest is [notes](https://github.com/chonkbase/chonkbase) **v1.2.3**. [[1]](https://github.com/chonkbase/chonkbase) <eos>\x01"
		if msgs, ok := req["messages"].([]any); ok {
			for _, m := range msgs {
				mm, _ := m.(map[string]any)
				sys, _ := mm["content"].(string)
				if mm["role"] == "system" && strings.Contains(sys, "silent router") {
					content = "SEARCH"
				}
			}
		}
		if r.URL.Path == "/responses" {
			http.Error(w, "search should not run when GitHub lookup succeeds", 500)
			return
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
		case strings.Contains(r.URL.Path, "/releases/latest"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v1.2.3", "published_at": "2026-08-04T00:00:00Z",
				"html_url": "https://github.com/chonkbase/chonkbase/releases/tag/v1.2.3",
			})
		case strings.Contains(r.URL.Path, "/contents/CHANGELOG.md"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"html_url": "https://github.com/chonkbase/chonkbase/blob/master/CHANGELOG.md",
				"encoding": "base64", "content": changelog, "size": 20,
			})
		case strings.HasSuffix(r.URL.Path, "/repos/chonkbase/chonkbase"):
			_ = json.NewEncoder(w).Encode(map[string]any{"html_url": "https://github.com/chonkbase/chonkbase"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ghSrv.Close)

	cfg := config.Defaults()
	cfg.Nick = "eggbot"
	cfg.Server.Host = "irc.example.net"
	cfg.Owners.Handles = []string{"nate"}
	cfg.Store.Path = filepath.Join(t.TempDir(), "t.db")
	cfg.Scripts.LuaDir = t.TempDir()
	cfg.Scripts.PythonDir = t.TempDir()
	cfg.LLM.Enabled = true
	cfg.LLM.Search = true
	cfg.LLM.APIKey = "test"
	cfg.LLM.BaseURL = llmSrv.URL
	b, err := bot.New(cfg, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Close)
	b.LLM.Client.BaseURL = llmSrv.URL
	b.LLM.Client.APIKey = "test"
	b.GitHub = github.NewClient(ghSrv.URL, "", 0)

	cs, _ := flags.ParseChanSet("+ai")
	b.Chans.LoadOrCreate("#lobby", cs, "", "", "", "")
	b.Chans.AddMember("#lobby", "nate", "n", "h", "", "@")
	b.Net.Apply(ircmsg.MakeMessage(nil, "eggbot!u@h", "JOIN", "#lobby"))
	b.Net.Apply(ircmsg.MakeMessage(nil, "srv", "332", "eggbot", "#lobby", "https://github.com/chonkbase/chonkbase"))
	c := b.Chans.Get("#lobby")
	c.Topic = "https://github.com/chonkbase/chonkbase"
	if err := b.Chans.Save(c); err != nil {
		t.Fatal(err)
	}

	u := b.Getuser("nate")
	reply, err := b.LLM.AskReq(llm.AskReq{
		Channel: "#lobby", Nick: "nate", Handle: "nate", User: u,
		Prompt: "latest changelog for this IRC server on GitHub",
		Topic:  b.Topic("#lobby"),
		Dest:   llm.DestChannel, Helpful: true, Search: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	b.IRC.PrivmsgHelp("#lobby", "nate: "+reply)

	got := llm.NormalizeIRC("see [Project](https://example.test) \x02<eos>")
	if strings.Contains(got, "](") || strings.Contains(got, "<eos>") || strings.ContainsAny(got, "\x02") {
		t.Fatalf("normalize %q", got)
	}

	var out strings.Builder
	for _, line := range b.IRC.Queue().Pending() {
		if !utf8.ValidString(line) {
			t.Fatalf("utf8 %q", line)
		}
		if strings.ContainsAny(line, "\x00\x01\x02\x03\r\n") {
			t.Fatalf("controls %q", line)
		}
		if len(line) > queue.DefaultConfig().MaxLine+80 {
			t.Fatalf("oversize %q", line)
		}
		out.WriteString(line)
	}
	text := out.String()
	if strings.Contains(text, "](") || strings.Contains(text, "<eos>") || strings.Contains(text, "**") {
		t.Fatalf("markdown survived: %s", text)
	}
	if !strings.Contains(text, "chonkbase") {
		t.Fatalf("expected topic-aware answer: %s", text)
	}
}
