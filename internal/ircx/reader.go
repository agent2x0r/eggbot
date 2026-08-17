package ircx

import "github.com/ergochat/irc-go/ircmsg"

// ParseLine is the session reader seam around irc-go's line parser.
func ParseLine(line string) (ircmsg.Message, error) {
	return ircmsg.ParseLine(line)
}
