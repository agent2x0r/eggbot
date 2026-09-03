# Changelog

## 0.1.1

- Documented `llm.persona` as the system-prompt voice in the example config; hot-reloads with `.rehash` instead of rebuilding to tune bot behavior per deployment or channel (per-channel note stays `llm_persona`)
- `[llm]` documented as any OpenAI-compatible chat/completions endpoint; web+X search stays xAI `/responses`-only - set `search = false` on other providers so live questions degrade to knowledge or an honest refusal
- Dropped the unused `llm.api` config key and provider-specific error text that assumed xAI
- Citation sanitizer drops numbered citations together with their URLs; replies are told to include links only when asked or when a lookup made one directly useful
- SQLite WAL is checkpointed on a 30s timer, on `.save`, and on shutdown so `eggbot.db` on disk matches what the bot heard; `seen` / `last_seen` write errors are logged instead of discarded
- Room-reader opens `eggbot.db` read-only so it cannot freeze or corrupt the live WAL

## 0.1.0

- IRC client with CAP, SASL, reconnect, and NOTICE
- Partyline, user flags, seen, notes, quotes
- Lua scripts; Python optional
- Optional LLM with channel context and GitHub lookup
- Credentials excluded from logs, scripts, seen, and model history
- SQLite with migrations and partyline `.backup`
- Version available from `eggbot -version`, `.version`, and CTCP VERSION
