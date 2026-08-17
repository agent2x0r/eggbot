# Commands

Partyline is telnet to `127.0.0.1:3333` by default (`partyline.listen`). `.` begins a command. `,` sends to owners. `'` echoes to the current session. Other text is partyline chat when `.chat` is on.

## Partyline

| Command | Need | What it does |
|---------|------|----------------|
| `.help` | — | list commands |
| `.who` / `.whom` | +p | who is on the partyline |
| `.status` / `.uptime` | +p | nick, server, uptime, LLM on/off |
| `.version` | +p | release number |
| `.match <nick\|mask>` | +m | find users |
| `.+user` / `.adduser` | +m | create a handle |
| `.-user` | +m | delete a handle |
| `.chattr <handle> <flags> [channel]` | +m | `+np`, `-o`, channel flags |
| `.+host` / `.-host` | +m | hostmasks |
| `.chpass [handle] <new>` | +p | change password (own, or another if you may) |
| `.console [+jpk…]` | +p | mirror joins/parts/kicks |
| `.chat [on\|off]` | +p | partyline chat |
| `.join` `.part` `.+chan` `.-chan` | +m | IRC membership; `.+chan` autojoins |
| `.chan` / `.chans` / `.channel` | +m | list / inspect |
| `.chanset #chan [+flag\|-flag]` | +m | channel settings |
| `.op` `.deop` `.voice` `.devoice` | +o | modes |
| `.kick` `.kickban` `.+ban` `.-ban` `.bans` | +o | moderation |
| `.say` `.msg` `.act` `.notice` `.topic` | +o | speak |
| `.note` `.notes` `.seen` `.quote` | +p | modules |
| `.+quote` `.-quote` | +m | manage quotes |
| `.ai` / `.llm` | +n | prompt, or enable/disable/model |
| `.binds` | +n | script binds |
| `.jump` `.save` `.rehash` `.backup` `.confirm` `.reject` `.die` | +n | reconnect / flush / reload / backup / LLM proposals / quit |
| `.quit` | +p | leave the partyline |

Incoming DCC CHAT is off by default. When enabled it is for `+p` users only, two concurrent dials, and it will not dial loopback/private/link-local/CGNAT targets.

## IRC

Query is `/msg eggbot …`. Some commands also work in channel with `!`.

| Command | Where | What |
|---------|--------|------|
| `hello` | query | create a handle |
| `pass <password>` | query | set an initial password (8+ characters) |
| `pass <current> <new>` | query | change an existing password |
| `ident <handle> <password>` | query | add this host to a handle |
| `whoami` | query | handle and flags |
| `help` | query | short list |
| `op #channel` | query | request +o if you have +o |
| `invite #channel` | query | invite yourself |
| `chat` | query | remind telnet address |
| `seen <nick>` | query or `!seen` | last seen |
| `note <handle> <text>` | query or `!note` | leave a note |
| `notes` | query | read your notes |
| `catchup` / `recap` | query or `!catchup` | recap (channel if used there) |
| `!ask` / `!ai` | channel | LLM (needs `+ai`) |
| `!quote` | channel | random or search |
| `eggbot: …` | channel | LLM (needs `+ai`) |

`hello` creates a user with no flags unless `learn.hello_flags` is set. Open registration is off (`learn.hello = false`).

Password, ident, and hello belong in a private message. In channel they are rejected and never reach logs, scripts, seen, or the LLM.

## Flags

| Flag | Meaning |
|------|---------|
| n | owner |
| m | master |
| o | op |
| v | voice |
| f | friend (flood-exempt) |
| p | partyline |
| a | auto-op |
| g | auto-voice |
| d | deop (never allowed ops) |
| k | auto-kick |
| q | quiet (never autovoice) |

Owner (`+n`) can grant `+m` and `+n`. Masters cannot modify peers or owners. Nobody can reset an equal-or-higher user's password.

To add someone:

```
.+user alice
.+host alice alice!ident@host
.chattr alice +p
```

Alice then `/msg eggbot pass a-long-password` and telnets in. Give `+o` to op.

`owners.auto_owner_hosts` is optional. A mask must name a configured owner and include an ident or host. Masks that match only a nickname, such as `alice!*@*`, are rejected.

## Channel settings

`.chanset #chan +flag`

Implemented: `enforcebans` `dynamicbans` `bitch` `protectops` `protectfriends` `dontkickops` `autoop` `autovoice` `greet` `seen` `nodesynch` `honorackmsg` `ai` `aitools`.

Line, join, and nick floods kick and ban when `enforcebans` or `dynamicbans` is on. `+f` / `+n` / `+m` / `+o` are exempt. `need_op` messages a configured nick when the bot is not opped. Timed `.+ban` durations expire.

`+aitools` lets the model call kick/topic/etc. **as the asking user**, and only if that user already has the flags. It cannot `chattr`, die, rehash, or read secrets.
