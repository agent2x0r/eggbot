# Sticky context (attention window)

After someone addresses eggbot (`eggbot:` / `eggbot,` / `eggbot ` or `!ask` / `!ai`) on a `+ai` channel, a few of *that nick’s* later lines can be follow-ups without the prefix. The bot then goes quiet again. Several people can talk to the bot at once. Channel scrollback is still the shared in-RAM `LLM.Remember` buffer (lost on restart).

Set `llm.sticky = false` to require a prefix every time.

## Policy

Key sessions by `(channel, nick)` with RFC1459 folding. Not one global floor.

- **Open** when that nick addresses eggbot or `!ask`/`!ai` and we dispatch a public AI turn (`DestChannel`). Catchup, query commands, DMs, and `/me` do not open a window. Open on dispatch, not after the model returns.
- **While open**, every line from that nick is an ask. The harness does not keyword-filter and does not close on `alice:`. The model decides: speak, `SILENT` (no reply, window stays), or `DROP` (no reply, window closes because they are clearly talking to someone else).
- **Close** on the first of:
  - 120s idle with no line from that nick, no in-flight ask, and no paused hold
  - the model replies `DROP`
  - they leave (part / quit / kick)
- A successful channel reply refreshes the idle deadline.

## Rate-limit pause

`llm.limits` still apply after a line is accepted. If Admit hits a per-user or per-channel cap:

1. The window stays open.
2. One held prompt (latest wins).
3. `NOTICE` the nick once: `too fast — I'll get to that when I can`.
4. When the minute slot opens (checked on the 30s expire loop), the held prompt is asked.
5. Idle does not expire the window while a prompt is held.

Owners with `owner_unlimited` skip this path. Other Admit errors still speak in channel.

## “Is this for me?”

Bang-commands (`!seen`, `!note`, `!help`, …) are handled as commands and do not go to the model. Empty lines are ignored. Addressing another current member closes the window. Everything else in the window is the model’s.

IRCv3 reply-to-bot-msgid is not used: `echo-message` is disabled, so eggbot does not learn its outbound msgid.

## Where it lives

- Gate: `internal/bot/msg.go` `handlePublic`
- State: in-memory `stickyMap` on the bot
- Rate errors: `llm.RateLimitError` from `rateOK`

No SQLite. No change to the hourly room-reader timer.
