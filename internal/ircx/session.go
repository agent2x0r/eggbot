package ircx

import (
	"github.com/ergochat/irc-go/ircmsg"

	"eggbot/internal/queue"
)

// Session is the injectable IRC client seam used by the bot and tests.
type Session interface {
	Connect() error
	Loop()
	Quit()
	Ready() bool
	Nick() string
	State() State
	Queue() *queue.Queue
	ISupport() *ISupport
	Caps() *Caps
	On(fn func(ircmsg.Message))
	OnReady(fn func())
	OnDisconnect(fn func())
	Send(pri queue.Pri, line string)
	SendMsg(pri queue.Pri, msg ircmsg.Message)
}

var _ Session = (*Client)(nil)
