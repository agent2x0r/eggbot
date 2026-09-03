// Package origin parses IRC prefixes, bang-commands, and addressing.
// Numerics accept both RFC2812 and short 353/332/366 forms (chonkline / RFC1459).
package origin

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ergochat/irc-go/ircmsg"

	"eggbot/internal/hostmask"
)

// Origin is who said or did something on IRC.
type Origin struct {
	Nick    string
	User    string
	Host    string
	Account string
	CertFP  string
	Handle  string
}

func (o Origin) NUH() string {
	return hostmask.Normalize(o.Nick, o.User, o.Host)
}

func (o Origin) String() string { return o.NUH() }

func FromMessage(msg ircmsg.Message) Origin {
	o := Origin{Nick: msg.Nick()}
	if nuh, err := msg.NUH(); err == nil {
		o.Nick = nuh.Name
		o.User = nuh.User
		o.Host = nuh.Host
	}
	if ok, acc := msg.GetTag("account"); ok {
		o.Account = acc
	}
	if ok, fp := msg.GetTag("certfp"); ok {
		o.CertFP = strings.ToLower(fp)
	}
	return o
}

func Param(msg ircmsg.Message, i int) string {
	if i < 0 || i >= len(msg.Params) {
		return ""
	}
	return msg.Params[i]
}

func Last(msg ircmsg.Message) string {
	if len(msg.Params) == 0 {
		return ""
	}
	return msg.Params[len(msg.Params)-1]
}

func IsChannel(target string) bool {
	return IsChannelTypes(target, "#&+!")
}

func IsChannelTypes(target, types string) bool {
	if target == "" {
		return false
	}
	if types == "" {
		types = "#&+!"
	}
	return strings.ContainsRune(types, rune(target[0]))
}

func IsCTCP(s string) (cmd, arg string, ok bool) {
	if len(s) < 2 || s[0] != 1 {
		return "", "", false
	}
	body := s[1:]
	if body[len(body)-1] == 1 {
		body = body[:len(body)-1]
	}
	cmd, arg, _ = strings.Cut(body, " ")
	return strings.ToUpper(cmd), arg, cmd != ""
}

func Addressed(botNick, text string) (body string, ok bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", false
	}
	// case-insensitive nick prefix followed by : , or space
	if len(text) <= len(botNick) {
		return "", false
	}
	prefix := text[:len(botNick)]
	if !strings.EqualFold(prefix, botNick) {
		return "", false
	}
	rest := text[len(botNick):]
	if rest == "" {
		return "", false
	}
	switch rest[0] {
	case ':', ',', ' ':
		return strings.TrimSpace(rest[1:]), true
	}
	return "", false
}

// AddressedTo reports a nick: / nick, prefix. Space after a nick is not
// treated as addressing (too many false positives: "so true", "what about").
func AddressedTo(text string) (nick, body string, ok bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", "", false
	}
	i := strings.IndexAny(text, ":,")
	if i <= 0 {
		return "", "", false
	}
	nick = text[:i]
	if nick == "" || strings.ContainsAny(nick, " \t!@") {
		return "", "", false
	}
	return nick, strings.TrimSpace(text[i+1:]), true
}

func Bang(text string) (cmd, args string, ok bool) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "!") {
		return "", "", false
	}
	cmd, args = SplitToken(strings.TrimSpace(text[1:]))
	cmd = strings.ToLower(cmd)
	return cmd, args, cmd != ""
}

// SplitToken returns the first Unicode-whitespace-separated token and the rest.
func SplitToken(text string) (first, rest string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", ""
	}
	i := 0
	for _, r := range text {
		if unicode.IsSpace(r) {
			break
		}
		i += utf8.RuneLen(r)
	}
	first = text[:i]
	if i >= len(text) {
		return first, ""
	}
	return first, strings.TrimSpace(text[i:])
}
