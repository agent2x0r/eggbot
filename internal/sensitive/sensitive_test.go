package sensitive

import (
	"strings"
	"testing"

	"github.com/ergochat/irc-go/ircmsg"
)

func TestCommand(t *testing.T) {
	for _, text := range []string{"pass s3cret", "IDENT nate s3cret", "auth nate s3cret", "password old new"} {
		if _, ok := Command(text); !ok {
			t.Fatalf("expected credential command: %q", text)
		}
	}
	if _, ok := Command("hello there"); ok {
		t.Fatal("hello is not a credential command")
	}
}

func TestPublic(t *testing.T) {
	for _, text := range []string{
		"eggbot: pass secret",
		"EggBot, IDENT owner secret",
		"!auth owner secret",
	} {
		if !Public("eggbot", text) {
			t.Fatalf("expected public credential command: %q", text)
		}
	}
	for _, text := range []string{
		"pass the salt",
		"eggbot: help",
		"!ask how do password managers work?",
	} {
		if Public("eggbot", text) {
			t.Fatalf("unexpected public credential command: %q", text)
		}
	}
}

func TestIRCMessage(t *testing.T) {
	pm := ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot", "pass s3cret")
	if !IRCMessage(pm, "eggbot") {
		t.Fatal("private pass should be sensitive")
	}
	ch := ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "#chan", "eggbot: ident nate s3cret")
	if !IRCMessage(ch, "eggbot") {
		t.Fatal("public ident should be sensitive")
	}
	ok := ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "#chan", "hello")
	if IRCMessage(ok, "eggbot") {
		t.Fatal("ordinary pubmsg is not sensitive")
	}
}

func TestRedact(t *testing.T) {
	if got := Redact("pass s3cret"); got != "pass *" {
		t.Fatal(got)
	}
	if got := EventText("eggbot", "ident nate s3cret"); got != "[redacted]" {
		t.Fatal(got)
	}
	if EventText("eggbot", "hello") != "hello" {
		t.Fatal(EventText("eggbot", "hello"))
	}
	if strings.Contains(Redact("pass s3cret"), "s3cret") {
		t.Fatal("secret survived redaction")
	}
}

func TestTabSeparatedCredentials(t *testing.T) {
	cases := []string{
		"pass\ts3cret",
		"pass\t\ts3cret",
		"ident\tnate\ts3cret",
		"auth\tnate s3cret",
	}
	for _, text := range cases {
		if _, ok := Command(text); !ok {
			t.Fatalf("expected credential command: %q", text)
		}
		if strings.Contains(Redact(text), "s3cret") {
			t.Fatalf("secret survived redaction: %q -> %q", text, Redact(text))
		}
	}
	if !Public("eggbot", "eggbot: pass\ts3cret") {
		t.Fatal("addressed tab pass")
	}
	if !Public("eggbot", "!ident\tnate\ts3cret") {
		t.Fatal("bang tab ident")
	}
	pm := ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "eggbot", "pass\ts3cret")
	if !IRCMessage(pm, "eggbot") {
		t.Fatal("pm tab pass")
	}
	ch := ircmsg.MakeMessage(nil, "eve!e@h", "PRIVMSG", "#chan", "eggbot: ident\tnate\ts3cret")
	if !IRCMessage(ch, "eggbot") {
		t.Fatal("channel tab ident")
	}
}
