# eggbot

eggbot is an IRC bot inspired by Eggdrop. It is a single Go binary with a telnet partyline, user flags, seen/notes/quotes, Lua scripts, optional Python scripts, and an optional LLM.

It is not a fork of Eggdrop and does not include Tcl.

## Build

Install [Go](https://go.dev/dl/) 1.23 or later. The first build downloads the Go 1.25.13 toolchain (from `go.mod`) and a pure-Go SQLite library. That takes about a minute; later builds are much faster. A C compiler is not required.

```bash
go build -o eggbot ./cmd/eggbot
```

`make` produces the same binary.

## Configure

```bash
cp eggbot.example.toml eggbot.toml
```

Edit at least:

- `server.host`
- `owners.handles` (the example uses `alice`)
- `channels.join`

If the server does not offer TLS, set `port = 6667` and `tls = false`.

Put API keys and passwords in the environment, not in the toml file. `eggbot.toml`, `.env`, and `*.db` are listed in `.gitignore`.

## Run

```bash
./eggbot -c eggbot.toml
```

Stop with Ctrl-C. Paths in the toml file are relative to that file.

The bot can join IRC without an API key. `!ask` reports that the key is missing until one is configured.

## Owner access

Handles listed in `owners.handles` are created at startup with owner flags. Set a password locally (it is not sent over IRC):

```bash
EGGBOT_BOOTSTRAP_PASSWORD='pick-a-password' ./eggbot -c eggbot.toml -bootstrap-owner alice
telnet 127.0.0.1 3333
```

Then, from IRC:

```
/msg eggbot ident alice yourpassword
```

Use the handle you put in `owners.handles`.

## Channels

From the partyline:

```
.join #lobby
.chanset #lobby +seen +ai
```

The bot answers `eggbot:` and `!ask` only on channels with `+ai`.

`.` begins a partyline command. `,` sends to owners. `'` echoes to the current session. `.help` lists commands. The full list is in [docs/commands.md](docs/commands.md).

## LLM

The LLM is optional.

```bash
cp .env.example .env
chmod 600 .env
```

Set `XAI_API_KEY` in `.env`, then:

```bash
set -a && . ./.env && set +a
./eggbot -c eggbot.toml
```

On a channel with `+ai`:

- `eggbot: capital of France` — answered by the model without web search
- `eggbot: is chonkline on GitHub?` — may use web search
- `!ask` / `!ai` — same as addressing the nick
- `!catchup` — recap of channel text this process has seen since it started

Partyline `.ai` is limited to owners. It reports whether a key is configured, not the key itself.

## Scripts

Lua is enabled by default. Python is disabled until `scripts.python = true` and the script is listed in `scripts.trusted`. See [docs/scripting.md](docs/scripting.md).

## Documentation

- [Commands and flags](docs/commands.md)
- [Operations](docs/operations.md)
- [Contributing](CONTRIBUTING.md)
- [Security](SECURITY.md)

## License

[MIT](LICENSE). Third-party Go modules: [THIRD_PARTY_NOTICES](THIRD_PARTY_NOTICES).
