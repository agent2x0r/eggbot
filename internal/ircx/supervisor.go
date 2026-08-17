package ircx

// Supervisor owns reconnect, backoff, and endpoint rotation.
// Implementation lives on Client in client.go:
//
//	disconnected → dialing → negotiating → authenticating → registering → ready → draining
