# Changelog

## 0.1.2

- After `eggbot:` or `!ask`, that nick’s later lines go to the model without a keyword filter until the window idles (120s) or the model replies DROP because they are talking to someone else. SILENT stays quiet without closing. Hitting the ask rate limit pauses the last question and picks it up when a slot opens (`llm.sticky`, default on).
- Live `!search` is one web lookup, same 60s budget as chat (`llm.limits.timeout_sec`). Lookup model and reasoning effort come from `llm.search_model` / `llm.search_reasoning` (empty reasoning omits the field). Chat stays `llm.model`. `!search` does not send room scrollback, so it cannot answer an earlier conversation. A newer ask from the same nick drops the old reply instead of printing it late. Channel search replies are one or two short sentences (capped). X/Twitter search only if the question is about posts there. Empty results say so instead of going quiet.
- `!help`, `!search`, and `!history` (search this channel's saved chat by words, optional time window, one help line). Natural-language `!history` questions match keywords (not the whole sentence), scan the full time window, and answer from those saved lines only. `!note <handle> <text>` is documented there; notes are still by handle.
- Public channel lines are stored in SQLite `chanlog` (365 days by default, `store.chanlog_days`) for !history and later RAG. This is separate from the 80-line RAM buffer and from `.seen`.
- Channel scrollback for the model is snapshotted to SQLite and reloaded after a restart shorter than 5 minutes; longer downtime still starts with an empty RAM buffer.

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
