package bot

import (
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/ergochat/irc-go/ircmsg"

	"eggbot/internal/chanstate"
	"eggbot/internal/config"
	"eggbot/internal/origin"
)

func TestNewLoadsConfiguredChannels(t *testing.T) {
	cfg := config.Defaults()
	cfg.Nick = "eggbot"
	cfg.Server.Host = "irc.example.net"
	cfg.Server.TLS = true
	cfg.LLM.Enabled = false
	cfg.Owners.Handles = []string{"nate"}
	cfg.Store.Path = filepath.Join(t.TempDir(), "t.db")
	cfg.Scripts.LuaDir = t.TempDir()
	cfg.Scripts.PythonDir = t.TempDir()
	cfg.Channels.Join = []string{"#cool"}
	cfg.Channel = map[string]config.Channel{
		"#cool": {Chanset: "+seen +ai +greet", Greet: "hi %n", LLMPersona: "brief", NeedOp: "chanserv"},
	}
	b, err := New(cfg, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Close)
	b.onIRC(ircmsg.MakeMessage(nil, "eggbot!u@h", "JOIN", "#cool"))
	b.onIRC(ircmsg.MakeMessage(nil, "srv", "366", "eggbot", "#cool", "end"))
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "JOIN", "#cool"))
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot", "\x01TIME\x01"))
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot", "\x01CLIENTINFO\x01"))
	b.Cfg.Partyline.DCC = true
	b.onIRC(ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot", "\x01DCC CHAT chat 1 1\x01"))
	b.onIRC(ircmsg.MakeMessage(nil, "nate!n@h", "PRIVMSG", "#cool", "!ask hello"))
	b.onIRC(ircmsg.MakeMessage(nil, "nate!n@h", "PRIVMSG", "#cool", "!catchup"))
	b.onIRC(ircmsg.MakeMessage(nil, "nate!n@h", "PRIVMSG", "#cool", "eggbot: help"))
	s := testPLSession(t, "nate")
	b.onPartyline(s, ".ai enable")
	b.onPartyline(s, ".ai disable")
	b.onPartyline(s, ".ai model grok-test")
	b.onPartyline(s, ".ai")
	b.onPartyline(s, ".who")
	b.onPartyline(s, ".match")
	b.onPartyline(s, ".chattr nate")
	b.onPartyline(s, ".chattr nate +f")
	b.onPartyline(s, ".+host nate nate!*@*")
	b.onPartyline(s, ".-host nate nate!*@*")
	b.onPartyline(s, ".chanset #cool +greet")
	b.onPartyline(s, ".reload")
	_, _ = b.IsBanned("#cool", "eve!e@h")
	_ = origin.Origin{Nick: "eve", User: "e", Host: "h"}.String()
	_ = b.MemberOp("#cool", "eggbot")
	_ = b.ChannelsWithNick("eve")
	_ = b.Ident.Bind("nate", b.Cfg.NetworkID(), "account", "nateacct")
	b.onIRC(ircmsg.MakeMessage(map[string]string{"account": "nateacct"}, "nate!n@h", "JOIN", "#cool"))
	_ = b.Chans.AddBan(chanstate.Ban{Channel: "#cool", Mask: "*!*@h", Reason: "no"})
	if ok, _ := b.IsBanned("#cool", "eve!e@h"); !ok {
		t.Fatal("ban")
	}
}

func TestCountOwnersViaChattr(t *testing.T) {
	b := testBot(t)
	if err := b.Users.Add("bob", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Users.Chattr("bob", "+n", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Users.Chattr("bob", "-n", ""); err != nil {
		t.Fatal(err)
	}
}
