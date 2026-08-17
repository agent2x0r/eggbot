package bot

import (
	"bufio"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ergochat/irc-go/ircmsg"

	"eggbot/internal/chanstate"
	"eggbot/internal/config"
	"eggbot/internal/flags"
	"eggbot/internal/llm"
	"eggbot/internal/origin"
)

func TestPartylineRemainingCommands(t *testing.T) {
	b := testBot(t)
	s := testPLSession(t, "nate")
	if err := b.Users.Add("alice", ""); err != nil {
		t.Fatal(err)
	}
	_ = b.Users.SetPass("alice", "hunter22")
	id, err := b.Quotes.Add("#room", "nate", "famous line")
	if err != nil {
		t.Fatal(err)
	}
	b.Chans.LoadOrCreate("#room", b.ChanSet("#room"), "", "", "", "")
	b.Chans.AddMember("#room", "alice", "a", "h", "", "")
	res, err := b.RunTool("kick", llm.ToolCtx{Handle: "nate", Channel: "#room", Nick: "eve"}, map[string]any{"nick": "eve"})
	if err != nil {
		t.Fatal(err)
	}
	pid := strings.TrimSpace(res[strings.LastIndex(res, " ")+1:])

	for _, c := range []string{
		".+user", ".-user", ".-user missing", ".-user alice",
		".chpass hunter22", ".chpass alice hunter22", ".chpass",
		".kickban", ".kickban #room alice later",
		".+chan #extra", ".jump",
		".confirm", ".confirm missing", ".confirm " + pid,
		".reject", ".reject missing",
		".-quote", ".-quote 99999", ".-quote " + strconv.FormatInt(id, 10),
	} {
		b.onPartyline(s, c)
	}

	s2 := testPLSession(t, "nate")
	b.onPartyline(s2, ".quit")
	b.Kick("#room", "eve", "bye")
	_ = b.Ready()
	b.expireBans()
	b.expireProposals()
	_ = allowedModes("+o")
	_ = allowedModes("o")
	_ = allowedModes("+z")
	b.onPartyline(s, ".die")
}

func TestMsgIdentPassOpInviteNote(t *testing.T) {
	b := testBot(t)
	b.Cfg.Learn.Hello = true
	if err := b.Users.Add("eve", "hunter22"); err != nil {
		t.Fatal(err)
	}
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot", "ident"))
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot", "ident eve hunter22"))
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot", "pass"))
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot", "pass hunter22 hunter23x"))
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot", "op"))
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot", "op #chan"))
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot", "invite #chan"))
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot", "note nate hello there"))
	b.onIRC(ircmsg.MakeMessage(nil, "nate!n@h", "PRIVMSG", "eggbot", "notes"))
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot", "op #chan"))

	cs, _ := flags.ParseChanSet("+seen +ai")
	b.Chans.LoadOrCreate("#chan", cs, "", "", "", "")
	b.Chans.AddMember("#chan", "eve", "e", "h", "", "")
	id, _ := b.Quotes.Add("#chan", "eve", "hello quote")
	_ = id
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "#chan", "!seen"))
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "#chan", "!seen nate"))
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "#chan", "!quote"))
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "#chan", "!note nate hi"))
	b.say("#chan", "hello from bot")
	b.clearAuthAttempts("eve!e@h")
}

func TestExpireBansAndProposals(t *testing.T) {
	b := testBot(t)
	_ = b.Chans.AddBan(chanstate.Ban{
		Channel:   "#c",
		Mask:      "*!*@old",
		ExpiresAt: time.Now().Add(-time.Minute),
	})
	b.mu.Lock()
	b.proposals["dead"] = &Proposal{ID: "dead", Expires: time.Now().Add(-time.Minute)}
	b.mu.Unlock()
	b.expireBans()
	b.expireProposals()
	if len(b.Chans.AllBans()) != 0 {
		t.Fatal("ban")
	}
	b.mu.Lock()
	_, ok := b.proposals["dead"]
	b.mu.Unlock()
	if ok {
		t.Fatal("proposal")
	}
}

func TestAskLLMAndTools(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"pong"}}]}`)
	}))
	t.Cleanup(srv.Close)
	b := testBot(t)
	b.Cfg.LLM.Enabled = true
	b.Cfg.LLM.Search = false
	b.Cfg.LLM.AllowPrivmsg = true
	b.Cfg.LLM.APIKey = "test"
	b.LLM.Client.BaseURL = srv.URL
	b.LLM.Client.APIKey = "test"
	cs, _ := flags.ParseChanSet("+ai +aitools +seen")
	b.Chans.LoadOrCreate("#chan", cs, "", "be brief", "", "")
	b.Chans.AddMember("#chan", "nate", "n", "h", "", "@")
	b.onIRC(ircmsg.MakeMessage(nil, "nate!n@h", "JOIN", "#chan"))
	u := b.Getuser("nate")
	b.askLLM("#chan", origin.Origin{Nick: "nate", User: "n", Host: "h", Handle: "nate"}, u, "hello brain", llm.DestChannel)
	b.askLLM("", origin.Origin{Nick: "nate"}, u, "dm hi", llm.DestDM)
	b.askLLM("#chan", origin.Origin{Nick: "nate"}, u, "", llm.DestChannel)
	b.askLLM("#chan", origin.Origin{Nick: "nate"}, u, "catch me up", llm.DestCatchup)
	b.Cfg.LLM.ConfirmMutations = false
	if _, err := b.RunTool("channel_say", llm.ToolCtx{Handle: "nate", Channel: "#chan", Nick: "nate"}, map[string]any{"text": "hi"}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.RunTool("channel_act", llm.ToolCtx{Handle: "nate", Channel: "#chan", Nick: "nate"}, map[string]any{"text": "waves"}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.RunTool("dm", llm.ToolCtx{Handle: "nate", Channel: "#chan", Nick: "nate"}, map[string]any{"text": "hi", "nick": "nate"}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.RunTool("mode", llm.ToolCtx{Handle: "nate", Channel: "#chan", Nick: "nate"}, map[string]any{"modes": "+v", "args": "eve"}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.RunTool("set_topic", llm.ToolCtx{Handle: "nate", Channel: "#chan", Nick: "nate"}, map[string]any{"topic": "t"}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.RunTool("ban", llm.ToolCtx{Handle: "nate", Channel: "#chan", Nick: "nate"}, map[string]any{"mask": "*!*@x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.RunTool("unban", llm.ToolCtx{Handle: "nate", Channel: "#chan", Nick: "nate"}, map[string]any{"mask": "*!*@x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.RunTool("chanset", llm.ToolCtx{Handle: "nate", Channel: "#chan", Nick: "nate"}, map[string]any{"spec": "+greet"}); err != nil {
		t.Fatal(err)
	}
}

func TestBotRunAndClose(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(15 * time.Second))
		r := bufio.NewReader(c)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(line, "CAP LS"):
				_, _ = io.WriteString(c, ":test. CAP * LS :account-tag\r\n")
			case strings.HasPrefix(line, "CAP REQ"):
				_, _ = io.WriteString(c, ":test. CAP * ACK :account-tag\r\n")
			case strings.HasPrefix(line, "CAP END"):
				_, _ = io.WriteString(c, ":test. 001 eggbot :welcome\r\n")
			case strings.HasPrefix(line, "PING"):
				_, _ = io.WriteString(c, ":test. PONG :test.\r\n")
			}
		}
	}()

	cfg := config.Defaults()
	cfg.Nick = "eggbot"
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = ln.Addr().(*net.TCPAddr).Port
	cfg.Server.TLS = false
	cfg.LLM.Enabled = false
	cfg.Partyline.Listen = "127.0.0.1:0"
	cfg.Observe.Listen = "127.0.0.1:0"
	cfg.Store.Path = filepath.Join(t.TempDir(), "t.db")
	cfg.Scripts.LuaDir = t.TempDir()
	cfg.Scripts.PythonDir = t.TempDir()
	cfg.Scripts.PythonEnabled = true
	cfg.Owners.Handles = []string{"nate"}
	b, err := New(cfg, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	errc := make(chan error, 1)
	go func() { errc <- b.Run() }()
	deadline := time.Now().Add(8 * time.Second)
	for !b.Ready() {
		select {
		case err := <-errc:
			t.Fatalf("run: %v", err)
		default:
			if time.Now().After(deadline) {
				b.Close()
				t.Fatal("not ready")
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	b.goWork(func() {})
	b.Close()
	select {
	case err := <-errc:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("Run did not return")
	}
}
