package ircx

import (
	"strings"
	"unicode/utf8"

	"github.com/ergochat/irc-go/ircmsg"

	"eggbot/internal/queue"
)

const defaultBodyLimit = 512

// EncodeLine serializes an outbound client message, stripping CR/LF/NUL
// from parameters and truncating to the negotiated body limit.
func EncodeLine(msg ircmsg.Message, lineLen int) (string, error) {
	if lineLen <= 0 {
		lineLen = defaultBodyLimit
	}
	for i, p := range msg.Params {
		msg.Params[i] = queue.Sanitize(p)
	}
	raw, err := msg.LineBytesStrict(true, lineLen)
	if err != nil && err != ircmsg.ErrorBodyTooLong {
		return "", err
	}
	line := strings.TrimRight(string(raw), "\r\n")
	return queue.Sanitize(line), nil
}

func Make(command string, params ...string) ircmsg.Message {
	return ircmsg.MakeMessage(nil, "", command, params...)
}

func CommandLine(command string, params ...string) (string, error) {
	return EncodeLine(Make(command, params...), defaultBodyLimit)
}

// TextBudget is the number of UTF-8 bytes available for a trailing parameter
// after the command, target, and framing.
func TextBudget(command, target string, lineLen int) int {
	if lineLen <= 0 {
		lineLen = defaultBodyLimit
	}
	// "CMD target :" plus CRLF
	overhead := len(command) + 1 + len(target) + 2 + 2
	n := lineLen - overhead
	if n < 16 {
		return 16
	}
	return n
}

func SplitCTCP(tag, text string, max int) []string {
	text = queue.Sanitize(text)
	inner := "\x01" + tag
	if text != "" {
		inner += " " + text
	}
	inner += "\x01"
	if max <= 0 {
		max = 400
	}
	prefixOverhead := len("\x01"+tag+" ") + 1
	bodyMax := max - prefixOverhead
	if bodyMax < 8 {
		bodyMax = 8
	}
	if len(inner) <= max {
		return []string{inner}
	}
	parts := queue.Split(text, bodyMax)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, "\x01"+tag+" "+p+"\x01")
	}
	return out
}

func ValidUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	return strings.ToValidUTF8(s, "")
}
