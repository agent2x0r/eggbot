# Security Policy

Report vulnerabilities to the repository owner. Do not file public issues for credential leaks, remote code execution, or authentication bypasses.

## Supported versions

The current tagged release and the main branch receive security fixes.

## Production

- TLS and SASL on networks that carry passwords
- Partyline on loopback, or TLS with a certificate for remote access
- `learn.production = true` and `learn.hello = false`
- Python plugins disabled unless listed in `scripts.trusted`
- LLM mutating tools require `.confirm`
- SQLite file mode `0600` and regular `.backup`

See [docs/security.md](docs/security.md) and [docs/operations.md](docs/operations.md).
