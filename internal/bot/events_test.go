package bot

import (
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ergochat/irc-go/ircmsg"

	"eggbot/internal/config"
	"eggbot/internal/flags"
	"eggbot/internal/llm"
	"eggbot/internal/script"
	"eggbot/internal/version"
)

func testBot(t *testing.T) *Bot {
	t.Helper()
	cfg := config.Defaults()
	cfg.Nick = "eggbot"
	cfg.Server.Host = "irc.example.net"
	cfg.Server.TLS = true
	cfg.Learn.Hello = true
	cfg.LLM.Enabled = false
	cfg.LLM.ConfirmMutations = true
	cfg.Owners.Handles = []string{"nate"}
	cfg.Store.Path = filepath.Join(t.TempDir(), "t.db")
	cfg.Scripts.LuaDir = t.TempDir()
	cfg.Scripts.PythonDir = t.TempDir()
	b, err := New(cfg, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Close)
	return b
}

func TestCredentialPMNeverReachesScripts(t *testing.T) {
	b := testBot(t)
	var leaked []string
	b.Scripts.AddBind(&script.Bind{Type: "msg", Mask: "*", Call: func(ev script.Event) bool {
		leaked = append(leaked, ev.Text)
		return true
	}})
	b.Scripts.AddBind(&script.Bind{Type: "raw", Mask: "*", Call: func(ev script.Event) bool {
		leaked = append(leaked, ev.Text, ev.Raw)
		return true
	}})
	msg := ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot", "pass s3cret-password")
	b.onIRC(msg)
	for _, s := range leaked {
		if strings.Contains(s, "s3cret") {
			t.Fatalf("secret leaked to script: %q", leaked)
		}
	}
}

func TestCredentialChannelNeverReachesHistory(t *testing.T) {
	b := testBot(t)
	var leaked []string
	b.Scripts.AddBind(&script.Bind{Type: "pub", Mask: "*", Call: func(ev script.Event) bool {
		leaked = append(leaked, ev.Text)
		return true
	}})
	b.Scripts.AddBind(&script.Bind{Type: "raw", Mask: "*", Call: func(ev script.Event) bool {
		leaked = append(leaked, ev.Text)
		return true
	}})
	msg := ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "#chan", "eggbot: ident nate s3cret-password")
	b.onIRC(msg)
	for _, s := range leaked {
		if strings.Contains(s, "s3cret") {
			t.Fatalf("secret leaked: %q", leaked)
		}
	}
}

func TestActionIsNotDropped(t *testing.T) {
	b := testBot(t)
	var saw bool
	b.Scripts.AddBind(&script.Bind{Type: "pubm", Mask: "*", Call: func(ev script.Event) bool {
		if strings.Contains(ev.Text, "dances") {
			saw = true
		}
		return true
	}})
	msg := ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "#chan", "\x01ACTION dances\x01")
	b.onIRC(msg)
	if !saw {
		t.Fatal("ACTION should surface as pubm")
	}
}

func TestCredentialNeverReachesLLM(t *testing.T) {
	b := testBot(t)
	msg := ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "#chan", "eggbot: pass s3cret-password")
	b.onIRC(msg)
	if strings.Contains(b.LLM.Tail("#chan", 80), "s3cret") {
		t.Fatal("secret leaked into LLM memory")
	}
}

func TestTabCredentialNeverReachesHistoryOrLLM(t *testing.T) {
	b := testBot(t)
	cs, _ := flags.ParseChanSet("+ai")
	b.Chans.LoadOrCreate("#chan", cs, "", "", "", "")
	var leaked []string
	b.Scripts.AddBind(&script.Bind{Type: "pub", Mask: "*", Call: func(ev script.Event) bool {
		leaked = append(leaked, ev.Text)
		return true
	}})
	b.Scripts.AddBind(&script.Bind{Type: "msg", Mask: "*", Call: func(ev script.Event) bool {
		leaked = append(leaked, ev.Text)
		return true
	}})
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "#chan", "eggbot: pass\ts3cret-tab"))
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot", "pass\ts3cret-tab"))
	for _, s := range leaked {
		if strings.Contains(s, "s3cret") {
			t.Fatalf("secret leaked to scripts: %q", leaked)
		}
	}
	if strings.Contains(b.LLM.Tail("#chan", 80), "s3cret") {
		t.Fatal("secret leaked into LLM memory")
	}
}

func TestNOTICECTCPDoesNotReply(t *testing.T) {
	b := testBot(t)
	before := b.IRC.Queue().Len()
	msg := ircmsg.MakeMessage(nil, "eve!e@h", "NOTICE", "eggbot", "\x01VERSION\x01")
	b.onIRC(msg)
	if b.IRC.Queue().Len() != before {
		t.Fatal("NOTICE CTCP must not generate a reply")
	}
}

func TestSelfCTCPDoesNotReply(t *testing.T) {
	b := testBot(t)
	before := b.IRC.Queue().Len()
	msg := ircmsg.MakeMessage(nil, "eggbot!u@h", "PRIVMSG", "eggbot", "\x01VERSION\x01")
	b.onIRC(msg)
	if b.IRC.Queue().Len() != before {
		t.Fatal("self CTCP must not loop")
	}
}

func TestCTCPVersionQueuesNotice(t *testing.T) {
	b := testBot(t)
	msg := ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot", "\x01VERSION\x01")
	b.onIRC(msg)
	var saw string
	for _, line := range b.IRC.Queue().Pending() {
		if strings.Contains(line, "PRIVMSG eve") {
			t.Fatalf("CTCP VERSION sent as PRIVMSG: %q", line)
		}
		if strings.Contains(line, "NOTICE eve") && strings.Contains(line, version.UserAgent()) {
			saw = line
		}
	}
	if saw == "" {
		t.Fatalf("expected NOTICE with %s, got %v", version.UserAgent(), b.IRC.Queue().Pending())
	}
}

func TestProductionDisablesHello(t *testing.T) {
	b := testBot(t)
	b.Cfg.Learn.Hello = true
	b.Cfg.Learn.ProductionLock = true
	msg := ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot", "hello")
	b.onIRC(msg)
	u := b.Users.FindByHost("eve!e@h")
	if u != nil {
		t.Fatal("production profile must not auto-register")
	}
}

func TestMutatingToolRequiresConfirm(t *testing.T) {
	b := testBot(t)
	res, err := b.RunTool("kick", llm.ToolCtx{Handle: "nate", Channel: "#c", Nick: "eve"}, map[string]any{"nick": "eve"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res, "confirm") {
		t.Fatalf("expected proposal, got %q", res)
	}
}
