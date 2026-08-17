"""eggbot Python plugin SDK.

A plugin is a .py file in scripts/python that imports this module.
The bot launches each file as a subprocess and speaks JSON-lines
on stdin/stdout.
"""

from __future__ import annotations

import json
import sys
from typing import Any, Callable

_binds: list[tuple[str, str, str, str, Callable]] = []
_next_id = 1


def bind(typ: str, flags: str = "-", mask: str = "*"):
    def deco(fn: Callable):
        global _next_id
        _binds.append((typ, flags, mask, fn.__name__, fn))
        _next_id += 1
        return fn

    return deco


def _send(obj: dict[str, Any]) -> None:
    sys.stdout.write(json.dumps(obj) + "\n")
    sys.stdout.flush()


def _call(fn: str, *args: Any) -> Any:
    req = id(object()) % 2_000_000_000
    _send({"op": "call", "req": req, "fn": fn, "args": list(args)})
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        msg = json.loads(line)
        if msg.get("op") == "ret" and msg.get("req") == req:
            if not msg.get("ok", True):
                raise RuntimeError(msg.get("error") or "call failed")
            return msg.get("value")
        if msg.get("op") == "event":
            # nested event while blocked — ignore, host will retry? just drop
            continue
    raise RuntimeError("bot closed")


def say(target: str, text: str) -> None:
    _call("say", target, text)


def act(target: str, text: str) -> None:
    _call("act", target, text)


def notice(target: str, text: str) -> None:
    _call("notice", target, text)


def boot(channel: str, nick: str, reason: str = "boot") -> None:
    _call("boot", channel, nick, reason)


def putserv(line: str) -> None:
    _call("putserv", line)


def puthelp(line: str) -> None:
    _call("puthelp", line)


def putquick(line: str) -> None:
    _call("putquick", line)


def putlog(line: str) -> None:
    _call("putlog", line)


def matchattr(handle: str, flags: str, channel: str = "") -> bool:
    return bool(_call("matchattr", handle, flags, channel))


def finduser(nuh: str) -> str:
    return str(_call("finduser", nuh) or "")


def validuser(handle: str) -> bool:
    return bool(_call("validuser", handle))


def onchan(nick: str, channel: str) -> bool:
    return bool(_call("onchan", nick, channel))


def isop(nick: str, channel: str) -> bool:
    return bool(_call("isop", nick, channel))


def isvoice(nick: str, channel: str) -> bool:
    return bool(_call("isvoice", nick, channel))


def botonchan(channel: str) -> bool:
    return bool(_call("botonchan", channel))


def botisop(channel: str) -> bool:
    return bool(_call("botisop", channel))


def topic(channel: str) -> str:
    return str(_call("topic", channel) or "")


def chanlist(channel: str) -> list:
    return list(_call("chanlist", channel) or [])


def botnick() -> str:
    return str(_call("botnick") or "")


def run() -> None:
    _send({"op": "hello", "script": sys.argv[0]})
    by_id: dict[int, Callable] = {}
    for i, (typ, flags, mask, name, fn) in enumerate(_binds, start=1):
        by_id[i] = fn
        _send({"op": "bind", "type": typ, "flags": flags, "mask": mask, "name": name, "id": i})
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        msg = json.loads(line)
        if msg.get("op") != "event":
            continue
        fn = by_id.get(int(msg.get("id") or 0))
        args = msg.get("args") or {}
        ok = True
        if fn is not None:
            try:
                ret = fn(
                    args.get("nick", ""),
                    args.get("host", ""),
                    args.get("handle", ""),
                    args.get("channel", ""),
                    args.get("text", ""),
                )
                if ret is False:
                    ok = False
            except Exception as exc:  # noqa: BLE001
                putlog(f"python bind error: {exc}")
        _send({"op": "result", "req": msg.get("req"), "ok": True, "value": ok})
