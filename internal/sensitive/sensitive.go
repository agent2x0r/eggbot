// Package sensitive classifies IRC authentication input so secrets never
// reach logs, scripts, history, or LLM memory.
package sensitive

import (
	"strings"

	"github.com/ergochat/irc-go/ircmsg"

	"eggbot/internal/origin"
)

// Commands that carry passwords or other secrets in their arguments.
var commands = map[string]struct{}{
	"pass":     {},
	"password": {},
	"ident":    {},
	"auth":     {},
}

// Command reports whether text is a credential command (leading token only).
func Command(text string) (cmd string, ok bool) {
	cmd, _ = origin.SplitToken(text)
	cmd = strings.ToLower(cmd)
	_, ok = commands[cmd]
	return cmd, ok
}

// Public reports whether a channel message is an addressed or bang
// credential command aimed at this bot.
func Public(botNick, text string) bool {
	body := ""
	if addressed, ok := origin.Addressed(botNick, text); ok {
		body = addressed
	} else if cmd, args, ok := origin.Bang(text); ok {
		body = cmd + " " + args
	} else {
		return false
	}
	_, ok := Command(body)
	return ok
}

// IRCMessage reports whether an inbound IRC message carries a credential
// command in its last parameter.
func IRCMessage(msg ircmsg.Message, botNick string) bool {
	if !strings.EqualFold(msg.Command, "PRIVMSG") && !strings.EqualFold(msg.Command, "NOTICE") {
		return false
	}
	if len(msg.Params) < 2 {
		return false
	}
	target, text := msg.Params[0], msg.Params[1]
	if origin.IsChannel(target) {
		return Public(botNick, text)
	}
	_, ok := Command(text)
	return ok
}

// Redact replaces credential arguments with a placeholder.
func Redact(text string) string {
	cmd, ok := Command(text)
	if !ok {
		return text
	}
	return cmd + " *"
}

// EventText is the only string that may be placed on a script event,
// log line, or history record for a credential-bearing message.
func EventText(botNick, text string) string {
	if Public(botNick, text) {
		return "[redacted]"
	}
	if _, ok := Command(text); ok {
		return "[redacted]"
	}
	return text
}
