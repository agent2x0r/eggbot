package bot

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ergochat/irc-go/ircmsg"

	"eggbot/internal/llm"
	"eggbot/internal/origin"
	"eggbot/internal/partyline"
	"eggbot/internal/store"
	"eggbot/internal/version"
)

func testPLSession(t *testing.T, handle string) *partyline.Session {
	t.Helper()
	a, b := net.Pipe()
	t.Cleanup(func() { _ = a.Close(); _ = b.Close() })
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := b.Read(buf); err != nil {
				return
			}
		}
	}()
	return &partyline.Session{Conn: a, Handle: handle, Console: "mjpknto", ChatOn: true}
}

func TestIRCLifecycleEvents(t *testing.T) {
	b := testBot(t)
	b.Chans.LoadOrCreate("#chan", b.ChanSet("#chan"), "hi %n", "", "chanserv", "")
	if cs := b.Chans.Get("#chan"); cs != nil {
		_ = cs.Chanset.Enable("seen")
		_ = cs.Chanset.Enable("greet")
		_ = cs.Chanset.Enable("nodesynch")
		_ = cs.Chanset.Enable("honorackmsg")
		_ = b.Chans.Save(cs)
	}

	b.onIRC(ircmsg.MakeMessage(nil, "srv", "001", "eggbot", "welcome"))
	b.onIRC(ircmsg.MakeMessage(nil, "eggbot!u@h", "JOIN", "#chan"))
	b.onIRC(ircmsg.MakeMessage(nil, "alice!a@h", "JOIN", "#chan", "aliceacct", "Alice"))
	b.onIRC(ircmsg.MakeMessage(nil, "alice!a@h", "PRIVMSG", "#chan", "hello there"))
	b.onIRC(ircmsg.MakeMessage(nil, "alice!a@h", "PRIVMSG", "#chan", "\x01ACTION waves\x01"))
	b.onIRC(ircmsg.MakeMessage(nil, "op!o@h", "MODE", "#chan", "+o", "alice"))
	b.onIRC(ircmsg.MakeMessage(nil, "alice!a@h", "NICK", "ally"))
	b.onIRC(ircmsg.MakeMessage(nil, "ally!a@h", "TOPIC", "#chan", "new topic"))
	b.onIRC(ircmsg.MakeMessage(nil, "srv", "353", "eggbot", "=", "#chan", "@eggbot!u@h +bob!b@bh"))
	b.onIRC(ircmsg.MakeMessage(nil, "srv", "366", "eggbot", "#chan", "end"))
	b.onIRC(ircmsg.MakeMessage(nil, "srv", "332", "eggbot", "#chan", "topic text"))
	b.onIRC(ircmsg.MakeMessage(nil, "op!o@h", "KICK", "#chan", "bob", "out"))
	b.onIRC(ircmsg.MakeMessage(nil, "ally!a@h", "PART", "#chan", "bye"))
	b.onIRC(ircmsg.MakeMessage(nil, "carol!c@h", "QUIT", "gone"))
	b.onIRC(ircmsg.MakeMessage(nil, "op!o@h", "INVITE", "eggbot", "#chan"))
	b.onIRC(ircmsg.MakeMessage(nil, "eggbot!u@h", "PRIVMSG", "#chan", "self echo"))
	_ = b.Topic("#chan")
	_ = b.Chanlist("#chan")
	_ = b.Onchan("eggbot", "#chan")
	_ = b.Botonchan("#chan")
	_ = b.Isop("alice", "#chan")
	_ = b.Isvoice("bob", "#chan")
	_ = b.Finduser("alice!a@h")
	_ = b.Validuser("nate")
	_ = b.MatchAttr("nate", "n", "")
	_ = b.BotNick()
	b.Say("#chan", "hi")
	b.Act("#chan", "waves")
	b.Notice("alice", "hi")
	b.Putlog("log line")
	b.Putserv("PRIVMSG #chan :x")
	b.Puthelp("PRIVMSG #chan :y")
	b.Putquick("MODE #chan +v bob")
}

func TestPartylineCommands(t *testing.T) {
	b := testBot(t)
	s := testPLSession(t, "nate")
	commands := []string{
		".help", ".status", ".version", ".who", ".+user alice", ".chattr alice +p",
		".+host alice alice!*@*", ".-host alice alice!*@*", ".match alice",
		".join #room", ".chanset #room +seen", ".chan", ".channel #room",
		".say #room hello", ".act #room waves", ".notice alice hi",
		".topic #room hi", ".op #room alice", ".voice #room alice",
		".deop #room alice", ".devoice #room alice", ".kick #room alice later",
		".+ban *!*@bad #room spam", ".bans #room", ".-ban *!*@bad #room",
		".note alice hello from tests", ".notes", ".seen alice",
		".+quote #room famous line", ".quote famous", ".binds", ".ai",
		".save", ".console +mjpk", ".chat off", "'echo", ",owners only",
		".part #room", ".-chan #room", ".unknown", "",
	}
	for _, c := range commands {
		b.onPartyline(s, c)
	}
	dest := filepath.Join(t.TempDir(), "bak.db")
	b.onPartyline(s, ".backup "+dest)
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("backup: %v", err)
	}
}

func TestPublicHelpLines(t *testing.T) {
	lines := publicHelpLines("eggbot")
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "0.1.") || strings.Contains(strings.ToLower(joined), "commit=") {
		t.Fatal("help should not dump version")
	}
	if strings.Contains(joined, "weeks dogs") {
		t.Fatal("help should not mash history example")
	}
	if !strings.Contains(joined, "!history") || !strings.Contains(joined, "!note") {
		t.Fatal(joined)
	}
	histLines := 0
	for _, line := range lines {
		if strings.Contains(line, "!history") {
			histLines++
			if !strings.Contains(line, "last 2 days") {
				t.Fatalf("history should be one line with the time hint: %q", line)
			}
		}
	}
	if histLines != 1 {
		t.Fatalf("history should be one help line, got %d\n%s", histLines, joined)
	}
	if !strings.Contains(joined, "I'll remember you") {
		t.Fatal(joined)
	}
}

func TestRecordSeenAndTouchOnClosedStore(t *testing.T) {
	b := testBot(t)
	_ = b.Store.Close()
	b.recordSeen("nate", "nate", "", "n!u@h", "#lobby", "saying", "x")
	b.touchUser("nate")
	if b.Store.WriteErrors() == 0 {
		t.Fatal("expected sqlite write errors after closed store")
	}
}

func TestSaveCheckpointsSeenToMainFile(t *testing.T) {
	b := testBot(t)
	if err := b.Seen.Record("nate", "nate", "", "n!u@h", "#lobby", "saying", "hello"); err != nil {
		t.Fatal(err)
	}
	s := testPLSession(t, "nate")
	b.cmdSave(s, nil, "")
	copyPath := filepath.Join(t.TempDir(), "copy.db")
	raw, err := os.ReadFile(b.Cfg.Store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(copyPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	st2, err := store.Open(copyPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st2.Close() })
	var n int
	if err := st2.DB.QueryRow(`SELECT COUNT(*) FROM seen WHERE nick='nate' AND last_text='hello'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("main file missing seen after .save, count=%d", n)
	}
}

func TestPartylineVersion(t *testing.T) {
	b := testBot(t)
	a, c := net.Pipe()
	t.Cleanup(func() { _ = a.Close(); _ = c.Close() })
	s := &partyline.Session{Conn: a, Handle: "nate", Console: "mjpknto"}
	got := make(chan string, 1)
	go func() {
		buf := make([]byte, 4096)
		n, err := c.Read(buf)
		if err != nil {
			got <- err.Error()
			return
		}
		got <- string(buf[:n])
	}()
	b.onPartyline(s, ".version")
	select {
	case line := <-got:
		if !strings.Contains(line, version.Version) {
			t.Fatalf("got %q", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestRehashHotConfig(t *testing.T) {
	b := testBot(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "eggbot.toml")
	body := `
nick = "eggbot"
[server]
host = "irc.example.net"
tls = true
[owners]
handles = ["nate"]
[llm]
enabled = false
model = "grok-test"
[store]
path = "` + b.Cfg.Store.Path + `"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	b.Cfg.Path = path
	s := testPLSession(t, "nate")
	b.onPartyline(s, ".rehash")
	if b.Cfg.LLM.Model != "grok-test" {
		t.Fatalf("model %s", b.Cfg.LLM.Model)
	}
}

func TestConfirmAndRejectProposal(t *testing.T) {
	b := testBot(t)
	ctx := llm.ToolCtx{Handle: "nate", Channel: "#c", Nick: "eve"}
	res, err := b.RunTool("kick", ctx, map[string]any{"nick": "eve"})
	if err != nil {
		t.Fatal(err)
	}
	id := strings.TrimSpace(res[strings.LastIndex(res, " ")+1:])
	if err := b.RejectProposal(id, "nate"); err != nil {
		t.Fatal(err)
	}
	res, err = b.RunTool("kick", ctx, map[string]any{"nick": "eve"})
	if err != nil {
		t.Fatal(err)
	}
	id = strings.TrimSpace(res[strings.LastIndex(res, " ")+1:])
	if _, err := b.ConfirmProposal(id, "nate"); err != nil {
		t.Fatal(err)
	}
}

func TestMsgCommands(t *testing.T) {
	b := testBot(t)
	b.Cfg.Learn.Hello = true
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot", "hello"))
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot", "whoami"))
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot", "help"))
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot", "chat"))
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot", "seen nate"))
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot", "notes"))
}

func TestNeedOpAndCTCPPing(t *testing.T) {
	b := testBot(t)
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot", "\x01PING 123\x01"))
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot", "\x01FINGER\x01"))
	if b.IRC.Queue().Len() == 0 {
		t.Fatal("expected ctcp replies")
	}
}

func TestScriptAPIHelpers(t *testing.T) {
	b := testBot(t)
	if b.Getuser("nate") == nil {
		t.Fatal("owner")
	}
	if err := b.Chattr("nate", "+f", ""); err != nil {
		t.Fatal(err)
	}
	_ = b.ChannelsWithNick("nate")
	_ = b.UserOn(origin.Origin{Nick: "nate"})
	_ = b.MemberOp("#nope", "nate")
	_ = b.AutoJoin("#nope")
	_, _ = b.IsBanned("#nope", "x!y@z")
	_ = time.Now()
}
