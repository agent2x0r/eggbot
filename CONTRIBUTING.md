# Contributing

Contributions are welcome. That includes features that go beyond the current Eggdrop-like core.

## Development

Install [Go](https://go.dev/dl/) 1.23 or later.

```bash
go test ./...
```

`make test` runs the same suite. The first run downloads the toolchain and SQLite.

Source is under `internal/`. `cmd/eggbot` is the entry point. `eggbot.example.toml` is the sample config. Tests are `*_test.go` files in the same directory as the code they cover. Longer session tests are in `test/integration`.

To run a bot while developing, copy `eggbot.example.toml` to `eggbot.toml`. That file, `.env`, and `*.db` are gitignored.

The release version is `internal/version/version.go`. See [docs/RELEASING.md](docs/RELEASING.md). Commands: [docs/commands.md](docs/commands.md). Package layout: [docs/architecture.md](docs/architecture.md).
