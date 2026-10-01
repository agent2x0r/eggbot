package bot

import (
	"testing"

	"github.com/ergochat/irc-go/ircmsg"

	"eggbot/internal/script"
)

func TestBotTagSkipsDispatchButNotLogging(t *testing.T) {
	b := testBot(t)
	var got []string
	for _, typ := range []string{"pub", "pubm", "msg"} {
		typ := typ
		b.Scripts.AddBind(&script.Bind{Type: typ, Mask: "*", Call: func(ev script.Event) bool {
			got = append(got, typ+":"+ev.Text)
			return true
		}})
	}
	send := func(tags map[string]string, target, text string) {
		b.onPrivmsg(ircmsg.MakeMessage(tags, "other!b@h", "PRIVMSG", target, text))
	}

	send(map[string]string{"bot": ""}, "#chan", "!help")
	send(map[string]string{"bot": ""}, "eggbot", "!help")
	if len(got) != 0 {
		t.Fatalf("bot-tagged lines dispatched: %v", got)
	}

	send(nil, "eggbot", "hello")
	if len(got) == 0 {
		t.Fatal("untagged private message should still dispatch")
	}
}
