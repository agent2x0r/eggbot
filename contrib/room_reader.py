#!/usr/bin/env python3
"""Hourly room reader for eggbot #lobby. No Go changes.

State machine (checked every 5 minutes by systemd):
  - Last #lobby chat older than 90 min -> HOLD
  - First chat after that silence      -> start a 60-min clock
  - Clock fires while room still alive -> ask xAI, .say #lobby via partyline
  - After speaking, the 60-min clock starts again (hourly while the room is live)

Partyline login uses EGGBOT_PL_HANDLE (default owners-nick-here) and
EGGBOT_PL_PASSWORD. The password is never logged.
"""
from __future__ import annotations

import json
import os
import re
import socket
import sqlite3
import sys
import time
import urllib.error
import urllib.request
from datetime import datetime, timezone

DB = "/var/lib/eggbot/eggbot.db"
ENV_FILE = "/etc/eggbot/eggbot.env"
CHANNEL = "#lobby"
PL_HANDLE = os.environ.get("EGGBOT_PL_HANDLE", "owners-nick-here")
PL_HOST = "127.0.0.1"
PL_PORT = 3333
STATE_FILE = "/var/lib/eggbot/room-reader-state.json"
LOG_FILE = "/var/lib/eggbot/room-reader.log"

SILENCE_THRESHOLD = 90 * 60   # hold if no chats in this window
ENGAGE_EVERY = 60 * 60        # speak this often while the room is alive
CONTEXT_WINDOW = 3 * 3600
MAX_CONTEXT_CHARS = 4000
MAX_SAY = 200


def log(msg: str) -> None:
    ts = datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M:%S")
    line = f"{ts} {msg}"
    try:
        with open(LOG_FILE, "a") as f:
            f.write(line + "\n")
    except OSError:
        pass
    print(line, flush=True)


def load_env_file(path: str) -> dict:
    env = {}
    try:
        with open(path) as f:
            for line in f:
                line = line.strip()
                if not line or line.startswith("#") or "=" not in line:
                    continue
                k, _, v = line.partition("=")
                env[k.strip()] = v.strip().strip('"').strip("'")
    except OSError:
        pass
    return env


def db():
    # Read-only: the Go bot is the only sqlite writer. A second read-write
    # connection can freeze WAL so eggbot.db never sees today's seen.
    con = sqlite3.connect("file:" + DB + "?mode=ro", uri=True, timeout=8)
    con.row_factory = sqlite3.Row
    return con


def get_last_activity():
    con = db()
    try:
        row = con.execute(
            "SELECT MAX(seen_at) AS ts FROM seen "
            "WHERE channel=? AND event=?",
            (CHANNEL, "saying"),
        ).fetchone()
        return int(row["ts"]) if row and row["ts"] else None
    finally:
        con.close()


def get_recent_snippets(window_sec: int):
    con = db()
    try:
        cutoff = int(time.time()) - window_sec
        rows = con.execute(
            "SELECT nick, last_text, seen_at FROM seen "
            "WHERE channel=? AND event=? AND seen_at>=? AND last_text<>'' "
            "ORDER BY seen_at ASC",
            (CHANNEL, "saying", cutoff),
        ).fetchall()
        out = []
        for r in rows:
            nick = (r["nick"] or "").strip()
            if nick.lower() in ("eggbot", "eggbot_", "eggbot2", PL_HANDLE):
                continue
            out.append(dict(r))
        return out
    finally:
        con.close()


def load_state():
    try:
        with open(STATE_FILE) as f:
            return json.load(f)
    except (OSError, json.JSONDecodeError):
        return {}


def save_state(state: dict) -> None:
    tmp = STATE_FILE + ".tmp"
    with open(tmp, "w") as f:
        json.dump(state, f)
        f.write("\n")
    os.replace(tmp, STATE_FILE)


def build_context(messages) -> str:
    lines = []
    for m in messages:
        ts = datetime.fromtimestamp(int(m["seen_at"]), tz=timezone.utc).strftime("%H:%M")
        text = (m["last_text"] or "").replace("\n", " ").strip()
        lines.append(f"{ts} {m['nick']}: {text}")
    txt = "\n".join(lines)
    if len(txt) > MAX_CONTEXT_CHARS:
        txt = txt[-MAX_CONTEXT_CHARS:]
    return txt


def ask_xai(context: str, env: dict):
    key = os.environ.get("XAI_API_KEY") or env.get("XAI_API_KEY") or ""
    base = os.environ.get("XAI_BASE_URL") or env.get("XAI_BASE_URL") or "https://api.x.ai/v1"
    model = os.environ.get("XAI_MODEL") or env.get("XAI_MODEL") or "grok-4.6"
    if not key:
        log("WARN: XAI_API_KEY not set")
        return None
    prompt = (
        "You are eggbot in IRC #lobby. Below are the most recent last-lines "
        "from people in the room (not a full transcript). Write ONE short "
        "casual statement or question that fits the room, under 80 characters. "
        "No citations, no preamble, no quotes around it, no bot name prefix. "
        "Just the message.\n\nRecent last-lines:\n" + (context or "(none)")
    )
    body = json.dumps({
        "model": model,
        "messages": [{"role": "user", "content": prompt}],
        "max_tokens": 80,
        "temperature": 0.8,
    }).encode()
    req = urllib.request.Request(
        base.rstrip("/") + "/chat/completions",
        data=body,
        headers={
            "Content-Type": "application/json",
            "Authorization": "Bearer " + key,
        },
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            data = json.loads(resp.read())
        text = (data["choices"][0]["message"]["content"] or "").strip()
        text = re.sub(r"\s+", " ", text).strip().strip('"').strip("'")
        if len(text) > MAX_SAY:
            text = text[:MAX_SAY].rsplit(" ", 1)[0]
        return text or None
    except (urllib.error.URLError, TimeoutError, KeyError, IndexError, json.JSONDecodeError) as e:
        log(f"ERROR: xAI call failed: {e}")
        return None


def recv_until(sock: socket.socket, marker: str, limit: int = 8192) -> str:
    buf = b""
    needle = marker.encode()
    while len(buf) < limit:
        chunk = sock.recv(1024)
        if not chunk:
            break
        buf += chunk
        if needle in buf:
            break
    return buf.decode(errors="replace")


def send_partyline(text: str) -> bool:
    password = os.environ.get("EGGBOT_PL_PASSWORD", "")
    if not password:
        log("WARN: EGGBOT_PL_PASSWORD not set")
        return False
    text = text.replace("\r", " ").replace("\n", " ").strip()
    if not text:
        return False
    try:
        s = socket.create_connection((PL_HOST, PL_PORT), timeout=10)
        s.settimeout(10)
        recv_until(s, "handle: ")
        s.sendall((PL_HANDLE + "\r\n").encode())
        recv_until(s, "password: ")
        s.sendall((password + "\r\n").encode())
        resp = recv_until(s, "type .help")
        low = resp.lower()
        if "login failed" in low or "too many failed" in low:
            log("ERROR: partyline auth failed")
            s.close()
            return False
        s.sendall((".say %s %s\r\n" % (CHANNEL, text)).encode())
        time.sleep(0.4)
        try:
            s.sendall(b".quit\r\n")
        except OSError:
            pass
        s.close()
        return True
    except OSError as e:
        log(f"ERROR: partyline send failed: {e}")
        return False


def ping_partyline() -> bool:
    password = os.environ.get("EGGBOT_PL_PASSWORD", "")
    if not password:
        log("WARN: EGGBOT_PL_PASSWORD not set")
        return False
    try:
        s = socket.create_connection((PL_HOST, PL_PORT), timeout=10)
        s.settimeout(10)
        recv_until(s, "handle: ")
        s.sendall((PL_HANDLE + "\r\n").encode())
        recv_until(s, "password: ")
        s.sendall((password + "\r\n").encode())
        resp = recv_until(s, "type .help")
        ok = "welcome" in resp.lower() and "login failed" not in resp.lower()
        try:
            s.sendall(b".quit\r\n")
        except OSError:
            pass
        s.close()
        log("PL ping: " + ("ok" if ok else "failed: " + resp.replace("\n", " ")[:180]))
        return ok
    except OSError as e:
        log(f"ERROR: partyline ping failed: {e}")
        return False


def status_line(now: int, last_active, state: dict) -> str:
    if last_active is None:
        return "no #lobby saying events in seen table"
    silence = now - last_active
    bits = [
        f"last_active={silence}s ago",
        f"dead={bool(state.get('dead', True))}",
    ]
    nd = state.get("next_due")
    if nd:
        bits.append(f"next_due_in={int(nd) - now}s")
    le = state.get("last_engage")
    if le:
        bits.append(f"last_engage={now - int(le)}s ago")
    return " ".join(bits)


def tick(force: bool = False) -> None:
    now = int(time.time())
    last_active = get_last_activity()
    state = load_state()

    if last_active is None:
        log("NO DATA: no #lobby saying events yet")
        return

    silence = now - last_active
    state["last_active"] = last_active

    if not force and silence >= SILENCE_THRESHOLD:
        if not state.get("dead"):
            log(f"HOLD: no chats for {silence}s (>=90min)")
        state["dead"] = True
        save_state(state)
        return

    if not force and (state.get("dead", True) or not state.get("next_due")):
        state["dead"] = False
        state["next_due"] = now + ENGAGE_EVERY
        log(f"CLOCK: room alive, next engage in {ENGAGE_EVERY}s ({status_line(now, last_active, state)})")
        save_state(state)
        return

    if not force and now < int(state.get("next_due", 0)):
        log("WAIT: " + status_line(now, last_active, state))
        save_state(state)
        return

    snippets = get_recent_snippets(CONTEXT_WINDOW)
    context = build_context(snippets)
    log(f"ENGAGE: {len(snippets)} last-lines, {status_line(now, last_active, state)}")
    env = load_env_file(ENV_FILE)
    text = ask_xai(context, env)
    if not text:
        log("ENGAGE FAILED: no text (will retry next tick)")
        save_state(state)
        return
    if send_partyline(text):
        log("SENT: " + text)
        state["last_engage"] = now
        state["next_due"] = now + ENGAGE_EVERY
        state["dead"] = False
        save_state(state)
    else:
        log("ENGAGE FAILED: partyline (will retry next tick)")
        save_state(state)


def main(argv) -> int:
    args = [a for a in argv[1:] if a]
    if "--status" in args:
        now = int(time.time())
        last_active = get_last_activity()
        state = load_state()
        log("STATUS: " + status_line(now, last_active, state))
        snippets = get_recent_snippets(CONTEXT_WINDOW)
        log(f"CONTEXT: {len(snippets)} last-lines in {CONTEXT_WINDOW}s window")
        return 0
    if "--ping-pl" in args:
        return 0 if ping_partyline() else 1
    tick(force="--force" in args)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
