# Scripting

Scripts use Eggdrop-style binds. Lua runs in the bot process. Python runs as a subprocess over JSON lines (`scripts/python/eggbot.py`).

Lua is enabled by default. Python is disabled until `scripts.python = true` and the script path (or a matching glob) is listed in `scripts.trusted`. Python has filesystem and network access.

## Lua

```lua
bind("pub", "-", "!ping", "ping_pub")
function ping_pub(nick, host, handle, chan, text)
  say(chan, nick .. ": pong")
end
```

The Lua environment does not include file, OS, or dynamic-code libraries. `chattr` and raw password or connection commands require `scripts.trusted`. If a reload fails to parse, the previous Lua state is kept.

## Python

```python
from eggbot import bind, say, run

@bind("pub", flags="-", mask="!py")
def ping_pub(nick, host, handle, chan, text):
    say(chan, f"{nick}: pong from python")

if __name__ == "__main__":
    run()
```

A successful start replaces the previous Python process. A failed start leaves the previous process running.

## Host API

`say` `act` `notice` `boot` `putserv` `puthelp` `putquick` `putlog` `matchattr` `finduser` `validuser` `onchan` `isop` `isvoice` `botonchan` `botisop` `topic` `chanlist` `botnick`
