package ircx

import (
	"github.com/ergochat/irc-go/ircmsg"

	"eggbot/internal/queue"
)

func (c *Client) SendMsg(pri queue.Pri, msg ircmsg.Message) {
	line, err := EncodeLine(msg, c.textLimit())
	if err != nil {
		return
	}
	c.q.Push(pri, line)
}
