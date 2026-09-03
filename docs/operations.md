# Operations

```bash
make
./eggbot -c eggbot.toml
./eggbot -check-config -c eggbot.toml
./eggbot -version
```

SIGINT and SIGTERM wait for IRC, scripts, and SQLite to finish before exit.

## Health

If `observe.listen` is set (default `127.0.0.1:0`):

- `GET /live` — process is running
- `GET /ready` — registered on IRC and the database responds
- `GET /metrics` — queue depth, drops, IRC ready state, sqlite write errors

## Backup

From the partyline:

```
.save
.backup
.backup /var/lib/eggbot/eggbot-manual.db
```

`.save` writes channel rows and runs `PRAGMA wal_checkpoint(TRUNCATE)` so `eggbot.db` on disk matches the live connection. `.backup` uses `VACUUM INTO` (mode `0600`) and is the check for “is RAM ahead of disk?”.

To restore: stop the bot, replace `store.path` with the backup file, start, then run `PRAGMA quick_check`. Do not copy a live database that has a WAL file.

One sqlite writer: the Go bot. Sidecars (the hourly room reader included) open `eggbot.db` with `mode=ro` after the bot has checkpointed, or they read a `.backup` copy. Never a second writer. Never copy a live `eggbot.db` + `-wal` + `-shm` trio.

If the on-disk file is behind the live bot (room-reader last-active frozen, `.seen` on partyline newer than `sqlite3 eggbot.db`):

1. Stop the sidecar timer so nothing else opens the live file.
2. `.backup /var/lib/eggbot/eggbot-flush.db` while the bot is still up.
3. Stop eggbot. Move aside `eggbot.db`, `eggbot.db-wal`, `eggbot.db-shm`. Install the flush file as `eggbot.db` (mode `0600`).
4. Start eggbot. `.save`. Confirm `.seen` and on-disk `seen_at` agree.
5. Start the timer.

## Upgrade

1. `.backup`
2. Replace the binary
3. Start (migrations run on boot)

To roll back: stop, restore the backup, replace the binary with the previous one.

`.rehash` reloads LLM, scripts, log level, and channel personas from the toml file. Changing the database path or the IRC host requires a restart.

LLM tools that kick, ban, change mode, set topic, or chanset create a proposal. Confirm with `.confirm <id>` or discard with `.reject <id>`.

## Long-running hosts

Set `learn.production = true` and `learn.hello = false`. Keep the partyline on loopback, or use TLS if it must be reachable remotely. Use TLS and SASL if passwords are sent on IRC. Leave Python disabled unless the script is listed in `scripts.trusted`. Keep backups and test a restore.

## Incidents

Stop the process (`.die` or SIGTERM). Rotate SASL, partyline, and LLM credentials. Review `ai_audit`. Restore the last known-good backup if the database is suspect. Userfile passwords are argon2id; change them with `.chpass`.
