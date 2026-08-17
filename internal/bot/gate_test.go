package bot

import (
	"bufio"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ergochat/irc-go/ircmsg"

	"eggbot/internal/config"
	"eggbot/internal/flags"
	"eggbot/internal/identity"
	"eggbot/internal/llm"
	"eggbot/internal/origin"
	"eggbot/internal/script"
)

func TestNewFailsOnMissingStoreDir(t *testing.T) {
	cfg := config.Defaults()
	cfg.Nick = "eggbot"
	cfg.Server.Host = "irc.example.net"
	cfg.Owners.Handles = []string{"nate"}
	cfg.Store.Path = filepath.Join(t.TempDir(), "missing", "eggbot.db")
	if _, err := New(cfg, slog.Default()); err == nil {
		t.Fatal("expected store open error")
	}
}

func TestPartylineAuthenticateBranches(t *testing.T) {
	b := testBot(t)
	if err := b.PL.Authenticate("ghost", "x"); err == nil {
		t.Fatal("missing handle")
	}
	if err := b.Users.Add("noparty", ""); err != nil {
		t.Fatal(err)
	}
	if err := b.PL.Authenticate("noparty", "x"); err == nil {
		t.Fatal("need +p")
	}
	if err := b.Users.Add("pluser", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Users.Chattr("pluser", "+p", ""); err != nil {
		t.Fatal(err)
	}
	if err := b.PL.Authenticate("pluser", "hunter22"); err == nil {
		t.Fatal("no password")
	}
	b.Cfg.Learn.ProductionLock = true
	if err := b.PL.Authenticate("nate", "hunter22"); err == nil {
		t.Fatal("production lock")
	}
	b.Cfg.Learn.ProductionLock = false
	if err := b.PL.Authenticate("nate", ""); err == nil {
		t.Fatal("empty owner password")
	}
	if err := b.PL.Authenticate("nate", "hunter22"); err != nil {
		t.Fatal(err)
	}
	if err := b.PL.Authenticate("nate", "wrongpass"); err == nil {
		t.Fatal("bad password")
	}
	if b.PL.OnJoin != nil {
		b.PL.OnJoin(testPLSession(t, "nate"))
	}
	if b.PL.OnLeave != nil {
		b.PL.OnLeave(testPLSession(t, "nate"))
	}
}

func TestPartylineUsageSuccessAndDenied(t *testing.T) {
	b := testBot(t)
	owner := testPLSession(t, "nate")
	if err := b.Users.Add("alice", "hunter22"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Users.Chattr("alice", "+p", ""); err != nil {
		t.Fatal(err)
	}
	if err := b.Users.Add("carol", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Users.Chattr("carol", "+m", ""); err != nil {
		t.Fatal(err)
	}
	if err := b.Users.AddHost("alice", "alice!*@h"); err != nil {
		t.Fatal(err)
	}
	cs, _ := flags.ParseChanSet("+seen +ai +greet")
	b.Chans.LoadOrCreate("#cool", cs, "hi %n", "brief", "chanserv", "")
	b.Chans.AddMember("#cool", "alice", "a", "h", "", "")
	b.Chans.AddMember("#cool", "eggbot", "u", "h", "", "@")
	b.Scripts.AddBind(&script.Bind{Type: "dcc", Mask: "*", Name: "echo", File: "t.lua", Call: func(script.Event) bool { return true }})

	for _, c := range []string{
		".help help", ".whom", ".uptime", ".match", ".match missing", ".match alice",
		".+user", ".adduser bob bob!*@*", ".-user", ".-user missing",
		".chattr", ".chattr missing", ".chattr alice", ".chattr alice +f", ".chattr alice +o #cool",
		".+host", ".+host missing *!*@*", ".+host alice alice!*@*", ".-host", ".-host missing *!*@*", ".-host alice alice!*@*",
		".chpass", ".chpass alice hunter22",
		".console", ".console mjpk", ".console +t-m", ".chat", ".chat on", ".chat off",
		".op", ".op #cool", ".op #cool alice", ".deop", ".deop #cool alice",
		".voice", ".voice #cool alice", ".devoice", ".devoice #cool alice",
		".kick", ".kick #cool alice later", ".kickban", ".kickban #cool alice later",
		".+ban", ".+ban *!*@x #cool 1s spam", ".bans", ".bans #cool", ".-ban", ".-ban *!*@x #cool", ".-ban *!*@nope #cool",
		".join", ".join cool", ".part", ".part #missing", ".part #cool leftover",
		".+chan #again", ".-chan", ".-chan #again",
		".chan", ".channel", ".channel #missing", ".chanset", ".chanset #missing", ".chanset #cool", ".chanset #cool +notathing", ".chanset #cool +greet",
		".say", ".act", ".notice", ".topic",
		".note", ".note alice", ".note alice hi", ".notes", ".seen", ".seen alice",
		".quote", ".quote #cool", ".+quote", ".+quote #cool", ".+quote #cool line", ".quote #cool line",
		".ai model", ".ai hello there", ".binds", ".bind",
		".reload",
	} {
		b.onPartyline(owner, c)
	}

	alice := testPLSession(t, "alice")
	for _, c := range []string{".die", ".join #x", ".status", ".who", "hello chat", ",secret"} {
		b.onPartyline(alice, c)
	}
	carol := testPLSession(t, "carol")
	b.onPartyline(carol, ".join #x")
	b.onPartyline(carol, ".die")
	b.onPartyline(testPLSession(t, "ghost"), ".help")

	b.Cfg.Path = ""
	b.onPartyline(owner, ".rehash")
	b.Cfg.Path = filepath.Join(t.TempDir(), "missing.toml")
	b.onPartyline(owner, ".rehash")

	dir := t.TempDir()
	cold := filepath.Join(dir, "cold.toml")
	if err := os.WriteFile(cold, []byte(`
nick = "other"
[server]
host = "irc.example.net"
tls = true
[owners]
handles = ["nate"]
[llm]
enabled = false
[store]
path = "`+b.Cfg.Store.Path+`"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	b.Cfg.Path = cold
	b.onPartyline(owner, ".rehash")

	b.Cfg.Scripts.PythonEnabled = true
	b.onPartyline(owner, ".reload")
	b.onPartyline(owner, ".backup rel.db")
	b.onPartyline(owner, ".backup")

	if !canParty(nil, "-") || canParty(nil, "n") {
		t.Fatal("canParty nil")
	}
}

func TestResolveIdentityAndIRCBranches(t *testing.T) {
	b := testBot(t)
	b.Cfg.Owners.Handles = append(b.Cfg.Owners.Handles, "auto", "eve")
	b.Cfg.Owners.AutoOwnerHosts = []string{"auto!*@trusted.example"}
	cs, _ := flags.ParseChanSet("+seen +ai +greet +honorackmsg")
	b.Chans.LoadOrCreate("#cool", cs, "hi %n (%h)", "brief", "chanserv", "k")
	b.Chans.SetAutoJoin("#cool", true)
	b.Chans.AddMember("#cool", "eve", "e", "h", "", "")
	if err := b.Users.AddHost("nate", "nate!*@h"); err != nil {
		t.Fatal(err)
	}
	if err := b.Notes.Add("nate", "alice", "read me"); err != nil {
		t.Fatal(err)
	}

	b.onIRC(ircmsg.MakeMessage(map[string]string{"account": "nateacct", "certfp": "DEAD"}, "nate!n@h", "JOIN", "#cool"))
	b.onIRC(ircmsg.MakeMessage(map[string]string{"account": "autoacct", "certfp": "beef"}, "auto!a@trusted.example", "PRIVMSG", "eggbot", "whoami"))
	if err := b.Ident.Bind("nate", b.Cfg.NetworkID(), identity.MechAccount, "boundacct"); err != nil {
		t.Fatal(err)
	}
	b.onIRC(ircmsg.MakeMessage(map[string]string{"account": "boundacct"}, "other!o@z", "PRIVMSG", "eggbot", "whoami"))

	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot", "hello"))
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot", "hello"))
	b.Cfg.Learn.HelloPerHour = 1
	b.onIRC(ircmsg.MakeMessage(nil, "zoe!z@h", "PRIVMSG", "eggbot", "hello"))
	b.onIRC(ircmsg.MakeMessage(nil, "zoe!z@h", "PRIVMSG", "eggbot", "hello"))

	b.onIRC(ircmsg.MakeMessage(nil, "eggbot!u@h", "PRIVMSG", "#cool", "self"))
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "NOTICE", "eggbot", "plain notice"))
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "NOTICE", "eggbot"))
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot"))
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "KICK", "#cool"))
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "MODE", "#cool"))
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "TOPIC"))
	b.onIRC(ircmsg.MakeMessage(nil, "srv", "353", "eggbot"))
	b.onIRC(ircmsg.MakeMessage(nil, "srv", "353", "eggbot", "=", "#new", "@op!u@h + :empty"))
	b.onIRC(ircmsg.MakeMessage(nil, "srv", "332", "eggbot", "#cool", "saved topic"))
	b.onIRC(ircmsg.MakeMessage(nil, "op!o@h", "INVITE", "eggbot", "#cool"))
	b.onIRC(ircmsg.MakeMessage(nil, "op!o@h", "MODE", "#cool", "+ov-v", "alice", "bob", "alice"))
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "@#cool", "statusmsg hi"))

	b.Cfg.Learn.HelloFlags = "+f"
	b.onIRC(ircmsg.MakeMessage(nil, "fresh!f@h", "PRIVMSG", "eggbot", "hello"))
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot", "password hunter22 hunter23x"))
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot", "ident eve hunter22"))
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot", "ident eve wrongpass"))
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot", "catchup"))
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot", "catchup #cool"))
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot", "note"))
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot", "chat"))
	b.onIRC(ircmsg.MakeMessage(nil, "nate!n@h", "PRIVMSG", "eggbot", "chat"))
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot", "op"))
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot", "invite"))
	b.onIRC(ircmsg.MakeMessage(nil, "unknown!u@h", "PRIVMSG", "eggbot", "op #cool"))
	b.onIRC(ircmsg.MakeMessage(nil, "unknown!u@h", "PRIVMSG", "eggbot", "invite #cool"))
	b.onIRC(ircmsg.MakeMessage(nil, "unknown!u@h", "PRIVMSG", "eggbot", "note nate hi"))
	b.onIRC(ircmsg.MakeMessage(nil, "unknown!u@h", "PRIVMSG", "eggbot", "notes"))
	b.onIRC(ircmsg.MakeMessage(nil, "unknown!u@h", "PRIVMSG", "eggbot", "whoami"))
	b.Cfg.LLM.AllowPrivmsg = true
	b.Cfg.LLM.Enabled = true
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot", "random question"))

	b.onIRC(ircmsg.MakeMessage(nil, "nate!n@h", "PRIVMSG", "#cool", "!quote missing"))
	b.onIRC(ircmsg.MakeMessage(nil, "nate!n@h", "PRIVMSG", "#cool", "!note"))
	b.onIRC(ircmsg.MakeMessage(nil, "unknown!u@h", "PRIVMSG", "#cool", "!note nate hi"))
	b.onIRC(ircmsg.MakeMessage(nil, "nate!n@h", "PRIVMSG", "#cool", "eggbot: hello"))
	b.onIRC(ircmsg.MakeMessage(nil, "nate!n@h", "PRIVMSG", "#cool", "eggbot: what is up"))
	b.onIRC(ircmsg.MakeMessage(nil, "nate!n@h", "PRIVMSG", "#cool", "!seen"))

	b.Cfg.Partyline.DCC = true
	for i := 0; i < 2; i++ {
		select {
		case b.dccSlots <- struct{}{}:
		default:
		}
	}
	b.onIRC(ircmsg.MakeMessage(nil, "nate!n@h", "PRIVMSG", "eggbot", "\x01DCC CHAT chat 16909060 4000\x01"))
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot", "\x01DCC CHAT chat 16909060 4000\x01"))
	b.onIRC(ircmsg.MakeMessage(nil, "nate!n@h", "PRIVMSG", "eggbot", "\x01DCC SEND file 1 1\x01"))

	b.onIRC(ircmsg.MakeMessage(nil, "srv", "366", "eggbot", "#cool", "end"))
	b.Chans.SetOp("#cool", "eggbot", true)
	b.onIRC(ircmsg.MakeMessage(nil, "srv", "366", "eggbot", "#cool", "end"))
	b.onIRC(ircmsg.MakeMessage(nil, "srv", "366", "eggbot", "#nope", "end"))

	_ = b.UserOn(origin.Origin{Handle: "nate"})
	_ = b.UserOn(origin.Origin{Nick: "nate", User: "n", Host: "h"})
	_ = b.UserOn(origin.Origin{Nick: "alice"})
	_ = b.UserOn(origin.Origin{})
	_ = b.authorize("", "n", "")
	_ = b.authorize("ghost", "n", "")
	_ = b.authorize("nate", "", "")
	_ = b.authorize("nate", "n", "")
	_ = b.authorize("alice", "n", "")
	_ = b.authorize("nate", "o", "#cool")
	_ = b.MatchAttr("ghost", "n", "")
	_ = b.Getuser("ghost")
	_ = b.need(nil, "-", "")
	_ = b.Isop("ghost", "#missing")
	_ = b.Isvoice("ghost", "#missing")
	_ = b.Topic("#missing")
	_ = b.Chanlist("#missing")
	_ = b.Finduser("nope!n@h")
	_, _ = b.IsBanned("#cool", "someone!s@h")

	b.mu.Lock()
	b.dying = true
	b.mu.Unlock()
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot", "hello"))
}

func TestToolsErrorAndLookupPaths(t *testing.T) {
	b := testBot(t)
	b.Cfg.LLM.ConfirmMutations = false
	ctx := llm.ToolCtx{Handle: "nate", Channel: "", Nick: "nate"}
	_, _ = b.RunTool("nope", ctx, nil)
	_, _ = b.RunTool("kick", ctx, map[string]any{"nick": "eve"})
	_, _ = b.RunTool("channel_say", ctx, map[string]any{"text": "hi"})
	_, _ = b.RunTool("channel_act", ctx, map[string]any{"text": "hi"})
	_, _ = b.RunTool("set_topic", ctx, map[string]any{"topic": "t"})
	_, _ = b.RunTool("ban", ctx, map[string]any{"mask": "*!*@x"})
	_, _ = b.RunTool("unban", ctx, map[string]any{"mask": "*!*@x"})
	_, _ = b.RunTool("mode", ctx, map[string]any{"modes": "+v"})
	_, _ = b.RunTool("chanset", ctx, map[string]any{"spec": "+greet"})
	_, _ = b.RunTool("dm", ctx, map[string]any{"text": ""})
	_, _ = b.RunTool("dm", llm.ToolCtx{Handle: "nate", Channel: "#c", Nick: "nate"}, map[string]any{"nick": "eve", "text": "hi"})
	if err := b.Users.Add("alice", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Users.Chattr("alice", "+p", ""); err != nil {
		t.Fatal(err)
	}
	_, _ = b.RunTool("dm", llm.ToolCtx{Handle: "alice", Channel: "#c", Nick: "alice"}, map[string]any{"nick": "eve", "text": "hi"})
	_, _ = b.RunTool("note_add", llm.ToolCtx{Handle: "nate", Channel: "#c", Nick: "nate"}, map[string]any{"handle": "", "text": ""})
	_, _ = b.RunTool("quote_search", llm.ToolCtx{Handle: "nate", Channel: "#c", Nick: "nate"}, map[string]any{"search": "nope"})
	_, _ = b.RunTool("seen_lookup", llm.ToolCtx{Handle: "nate", Channel: "#c", Nick: "nate"}, map[string]any{"nick": "ghost"})
	_, _ = b.RunTool("user_lookup", llm.ToolCtx{Handle: "nate", Channel: "#c", Nick: "nate"}, map[string]any{"handle": "ghost"})
	_, _ = b.RunTool("user_lookup", llm.ToolCtx{Handle: "nate", Channel: "#c", Nick: "nate"}, map[string]any{"handle": "nate"})
	cs, _ := flags.ParseChanSet("+seen")
	b.Chans.LoadOrCreate("#c", cs, "", "", "", "")
	_, _ = b.RunTool("mode", llm.ToolCtx{Handle: "nate", Channel: "#c", Nick: "nate"}, map[string]any{"modes": "+v"})
	_, _ = b.RunTool("mode", llm.ToolCtx{Handle: "nate", Channel: "#c", Nick: "nate"}, map[string]any{"modes": "z"})
	_ = mutatingTool("dm")
	_ = allowedModes("+")
	_ = allowedModes(" +o")

	b.Cfg.LLM.ConfirmMutations = true
	res, err := b.RunTool("kick", llm.ToolCtx{Handle: "nate", Channel: "#c", Nick: "eve"}, map[string]any{"nick": "eve"})
	if err != nil {
		t.Fatal(err)
	}
	id := strings.TrimSpace(res)
	if i := strings.LastIndex(res, " "); i >= 0 {
		id = strings.TrimSpace(res[i+1:])
	}
	if _, err := b.ConfirmProposal("missing", "nate"); err == nil {
		t.Fatal("missing proposal")
	}
	if _, err := b.ConfirmProposal(id, "alice"); err == nil {
		t.Fatal("not owner of proposal with +p only")
	}
	if _, err := b.Users.Chattr("alice", "+m", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ConfirmProposal(id, "alice"); err != nil {
		t.Fatal(err)
	}
}

func TestLivePartylineWhoBroadcastAndJoin(t *testing.T) {
	b := testBot(t)
	if err := b.Users.SetPass("nate", "hunter22"); err != nil {
		t.Fatal(err)
	}
	if err := b.Notes.Add("nate", "alice", "pending"); err != nil {
		t.Fatal(err)
	}
	var addr string
	var startErr error
	for i := 0; i < 30; i++ {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr = ln.Addr().String()
		_ = ln.Close()
		b.PL.Listen = addr
		startErr = b.PL.Start()
		if startErr == nil {
			break
		}
	}
	if startErr != nil {
		t.Fatal(startErr)
	}
	defer b.PL.Stop()

	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(4 * time.Second))
	r := bufio.NewReader(c)
	readUntil(t, r, "handle:")
	if _, err := io.WriteString(c, "nate\n"); err != nil {
		t.Fatal(err)
	}
	readUntil(t, r, "password:")
	if _, err := io.WriteString(c, "hunter22\n"); err != nil {
		t.Fatal(err)
	}
	readUntil(t, r, "welcome")
	deadline := time.Now().Add(2 * time.Second)
	for len(b.PL.Sessions()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("session")
		}
		time.Sleep(10 * time.Millisecond)
	}
	s := testPLSession(t, "nate")
	b.onPartyline(s, ".who")
	b.PL.Broadcast("hi all", false, nil)
	b.PL.Broadcast("owners", true, func(h string) bool { return h == "nate" })
	b.PL.Broadcast("skip", true, func(string) bool { return false })
	b.PL.Consolef('j', "join noise")
	_ = c.Close()
	time.Sleep(50 * time.Millisecond)
}

func readUntil(t *testing.T, r *bufio.Reader, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var buf strings.Builder
	for time.Now().Before(deadline) {
		_ = time.Second
		b, err := r.ReadByte()
		if err != nil {
			t.Fatalf("read %q in %q: %v", want, buf.String(), err)
		}
		buf.WriteByte(b)
		if strings.Contains(buf.String(), want) {
			return
		}
	}
	t.Fatalf("timeout waiting for %q in %q", want, buf.String())
}
