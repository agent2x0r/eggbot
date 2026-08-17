# Architecture

```
TCP/TLS → CAP/SASL → session state → bot
                         ↓
                      identity
                         ↓
           scripts     LLM     partyline
                         ↓
                       SQLite
```

- `internal/ircx` — connection, capabilities, SASL, reconnect, outbound messages
- `internal/ircstate` — current nicks and channels
- `internal/chanstate` — persisted channel settings
- `internal/identity` — account, certificate, or hostmask → handle
- `internal/app` — process start and shutdown

One IRC network per process. Additional addresses for that network go in `server.endpoints`.
