# AGENTS.md

Notes for coding agents. Human contributors: [CONTRIBUTING.md](CONTRIBUTING.md).

eggbot is a Go IRC bot. Build with `go build -o eggbot ./cmd/eggbot` or `make`. Configuration is copied from `eggbot.example.toml` to `eggbot.toml`.

Do not start the bot or create extra working copies unless the user asks. Do not create a git commit unless the user asks. Keep `eggbot.toml`, `.env`, `*.db`, and the `eggbot` binary out of git.

The release version is `internal/version/version.go` and must match a `##` heading in `CHANGELOG.md`. See [docs/RELEASING.md](docs/RELEASING.md).

User-facing notices and CTCP replies are sent with the IRC `NOTICE` command. Channel topic and scrollback given to the model are untrusted input. Model output should be plain text suitable for IRC (no markdown, CTCP, or color codes).

`cmd/eggbot` is main. `internal/ircx` is the IRC client. `internal/bot` is commands and the partyline. `internal/llm` is the model. Tests are `*_test.go` files next to the code they cover.
