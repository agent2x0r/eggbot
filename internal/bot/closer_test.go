package bot

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ergochat/irc-go/ircmsg"

	"eggbot/internal/chanstate"
	"eggbot/internal/flags"
	"eggbot/internal/llm"
	"eggbot/internal/origin"
)

func TestGateCloser(t *testing.T) {
	b := testBot(t)
	if err := b.Users.SetPass("nate", "hunter22"); err != nil {
		t.Fatal(err)
	}
	s := testPLSession(t, "nate")
	b.onPartyline(s, ".chan")
	cs, _ := flags.ParseChanSet("+seen +ai")
	b.Chans.LoadOrCreate("#cool", cs, "", "", "", "")
	b.Chans.SetJoined("#cool", true)
	b.onPartyline(s, ".chan #cool")
	b.onPartyline(s, ".chpass short")
	b.onPartyline(s, ".backup "+b.Cfg.Store.Path)

	notdir := filepath.Join(t.TempDir(), "luafile")
	if err := os.WriteFile(notdir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	b.Cfg.Scripts.LuaDir = notdir
	b.onPartyline(s, ".reload")

	for i := 0; i < 21; i++ {
		_ = b.Notes.Add("nate", "alice", "note body here")
	}
	b.onPartyline(s, ".note nate overflow")

	if err := b.Users.AddHost("nate", "nate!*@h"); err != nil {
		t.Fatal(err)
	}
	if b.Finduser("nate!n@h") == "" {
		t.Fatal("finduser")
	}
	_ = b.Chans.AddBan(chanstate.Ban{Channel: "#cool", Mask: "*!*@banned", Reason: ""})
	if ok, reason := b.IsBanned("#cool", "x!y@banned"); !ok || reason != "banned" {
		t.Fatalf("ban %v %q", ok, reason)
	}
	if !b.Botonchan("#cool") {
		t.Fatal("joined")
	}

	if err := b.Users.Add("bob", "hunter22"); err != nil {
		t.Fatal(err)
	}
	if err := b.Users.AddHost("bob", "lookup!*@*"); err != nil {
		t.Fatal(err)
	}
	b.Cfg.LLM.ConfirmMutations = false
	_, _ = b.RunTool("mode", llm.ToolCtx{Handle: "nate", Channel: "#cool", Nick: "nate"}, map[string]any{"modes": "+v", "nick": "eve"})
	_, _ = b.RunTool("note_add", llm.ToolCtx{Handle: "nate", Channel: "#cool", Nick: "nate"}, map[string]any{"handle": "bob", "text": "hi"})
	_, _ = b.RunTool("user_lookup", llm.ToolCtx{Handle: "nate", Channel: "#cool", Nick: "nate"}, map[string]any{"handle": "lookup"})
	_, _ = b.RunTool("chanset", llm.ToolCtx{Handle: "nate", Channel: "#cool", Nick: "nate"}, map[string]any{"spec": "+notathing"})
	_, _ = b.RunTool("quote_search", llm.ToolCtx{Handle: "nate", Channel: "#cool", Nick: "nate"}, map[string]any{})
	id, err := b.Quotes.Add("#cool", "nate", "remember this")
	if err != nil {
		t.Fatal(err)
	}
	_ = id
	_, _ = b.RunTool("quote_search", llm.ToolCtx{Handle: "nate", Channel: "#cool", Nick: "nate"}, map[string]any{"search": "remember"})

	b.onIRC(ircmsg.MakeMessage(nil, "bob!b@h", "PRIVMSG", "eggbot", "ident nate hunter22"))
	for i := 0; i < 6; i++ {
		b.onIRC(ircmsg.MakeMessage(nil, "bob!b@h", "PRIVMSG", "eggbot", "ident nate wrongpass"))
	}
	b.onIRC(ircmsg.MakeMessage(nil, "nate!n@h", "PRIVMSG", "eggbot", "pass"))
	b.onIRC(ircmsg.MakeMessage(nil, "nate!n@h", "PRIVMSG", "eggbot", "pass short"))
	b.onIRC(ircmsg.MakeMessage(nil, "nate!n@h", "PRIVMSG", "eggbot", "pass wrong hunter22x"))
	b.onIRC(ircmsg.MakeMessage(nil, "eggbot!u@h", "PART", "#cool", "bye"))
	b.Chans.AddMember("#cool", "bob", "b", "h", "", "")
	b.onIRC(ircmsg.MakeMessage(nil, "bob!b@h", "QUIT", "gone"))
	_ = b.UserOn(origin.Origin{Handle: "missing"})
	_ = b.Isvoice("eve", "#cool")
}
