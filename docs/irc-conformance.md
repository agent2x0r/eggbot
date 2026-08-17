# IRC RFC

eggbot is an IRC client. It implements the client side of RFC 1459 and RFC 2812. It is not a server and does not implement server-to-server linking or IRC operator commands.

Each RFC command and numeric is classified as:

- **handled** — updates bot or session state, or is answered automatically (PING, JOIN, MODE, 001, 005, …)
- **surfaced** — parsed and passed to scripts and logs with source, command, parameters, and tags
- **ignored** — valid but unused (MOTD body, LUSERS)
- **not applicable** — server-to-server or operator-only (SERVER, SQUIT, OPER, REHASH, DIE, SUMMON, USERS)

The inventory is `internal/ircx/conformance.go` and is covered by unit tests.

## Registration and messaging

1. Dial with `net.JoinHostPort` (IPv4, IPv6, hostnames).
2. Send PASS when configured, then `CAP LS 302`, NICK, and USER.
3. Complete CAP, including an empty `CAP LS` and `421 CAP` on daemons that do not implement CAP.
4. Run SASL when configured; numerics 902–908 end registration.
5. Send `CAP END` once. Numeric `001` means registration succeeded.
6. Answer PING. Outbound traffic uses a bounded queue. CR, LF, and NUL are not injected into messages.

PRIVMSG, NOTICE, TOPIC, JOIN, PART, QUIT, KICK, MODE, NICK, INVITE, ERROR, WHO, NAMES, and their numerics update session state.

The client does not currently implement server-to-server protocol, IRC operator commands, or Eggdrop botnet linking. `server.endpoints` is failover on a single network.
