#!/usr/bin/env python3
"""Restart a hub over and over while a client watches, and check that the client could not tell (docs/operations/hub-restart.md
"How to verify").

What runs at the same time:
  - a probe: GET /api/v1/health every 100 ms; any refused / failed request is a failure (socket activation: a
    request made while the hub restarts waits, it is never refused);
  - a client that behaves like the web app: /api/v1/ws, every broadcast's event_seq must follow the last one (no gap,
    no repeat) across reconnects, every reconnect after the first must be resumed (hello.resumed) on the same stream,
    and the time from losing the connection to the next hello must stay under the apps' grace (--max-outage);
  - with --converse: a new assistant conversation (a side one, not the main conversation) asked for a long streamed
    answer and a background command; the answer must keep streaming across the restarts and end complete, and the
    background command must report back.

Usage (on the hub's machine):
  python3 scripts/test/hub_restart_seamless.py --restarts 3 --converse
  python3 scripts/test/hub_restart_seamless.py --hub http://127.0.0.1:18660 --restart-cmd 'systemctl --user restart personad'

Always ends with one line: PASS / FAIL / TIMEOUT and the numbers, exit 0 only on PASS.
"""

import argparse
import asyncio
import json
import os
import re
import subprocess
import sys
import time
import urllib.error
import urllib.request
import uuid

try:
    import websockets
except ImportError:  # pragma: no cover
    print("FAIL: python websockets is not installed (pip install websockets)")
    sys.exit(2)


def hub_json():
    root = os.environ.get("PERSONA_HOME") or os.environ.get("PERSONA_HOME")
    for d in [root, os.path.expanduser("~/.persona"), os.path.expanduser("~/.persona")]:
        if d and os.path.exists(os.path.join(d, "hub.json")):
            with open(os.path.join(d, "hub.json")) as f:
                return json.load(f)
    return {}


class Report:
    def __init__(self):
        self.failures = []
        self.notes = []
        self.probes = 0
        self.probe_failures = 0
        self.probe_slowest = 0.0
        self.connections = 0
        self.resumed = 0
        self.events = 0
        self.outages = []
        self.restarts = 0

    def fail(self, msg):
        print("  ✗", msg, flush=True)
        self.failures.append(msg)

    def note(self, msg):
        print("  ·", msg, flush=True)
        self.notes.append(msg)


def api(hub, token, method, path, body=None, timeout=120):
    req = urllib.request.Request(hub + path, method=method, headers={"Authorization": "Bearer " + token, "Content-Type": "application/json"})
    data = json.dumps(body).encode() if body is not None else None
    with urllib.request.urlopen(req, data=data, timeout=timeout) as r:
        return json.loads(r.read() or b"{}")


async def probe(hub, rep, stop):
    while not stop.is_set():
        start = time.monotonic()
        try:
            await asyncio.to_thread(lambda: urllib.request.urlopen(hub + "/api/v1/health", timeout=20).read())
        except Exception as e:  # refused, reset, 5xx: the restart showed
            rep.probe_failures += 1
            rep.fail(f"health probe failed: {e}")
        took = time.monotonic() - start
        rep.probes += 1
        rep.probe_slowest = max(rep.probe_slowest, took)
        await asyncio.sleep(0.1)


class Client:
    """The web app's connection, as far as a restart goes."""

    def __init__(self, hub, token, rep, max_outage):
        self.ws_url = re.sub(r"^http", "ws", hub) + "/api/v1/ws"
        self.token, self.rep, self.max_outage = token, rep, max_outage
        self.stream, self.last = None, 0
        self.connected = asyncio.Event()
        self.restarting = False
        self.watch = {}  # (machine, session) -> covered item seq
        self.live = {}  # (machine, session) -> live text
        self.items = {}  # (machine, session) -> {seq: item}
        self.lost_at = None
        self.ws = None

    def url(self):
        if self.stream is None:
            return self.ws_url
        return f"{self.ws_url}?event_stream={self.stream}&event_seq={self.last}"

    async def subscribe(self, mid, sid):
        self.watch.setdefault((mid, sid), 0)
        if self.ws is not None:
            await self.ws.send(json.dumps({"type": "subscribe", "machine_id": mid, "session_id": sid, "since_seq": self.watch[(mid, sid)]}))

    def on_event_seq(self, seq):
        self.rep.events += 1
        if seq == self.last + 1:
            self.last = seq
        elif seq <= self.last:
            self.rep.fail(f"event {seq} again (last {self.last})")
        else:
            self.rep.fail(f"events {self.last + 1}..{seq - 1} lost")
            self.last = seq

    def on_hello(self, m):
        now = time.monotonic()
        self.rep.connections += 1
        stream, seq, resumed = m.get("event_stream"), m.get("event_seq"), m.get("resumed")
        if stream is None:
            self.rep.fail("hello has no event_stream: the hub does not number its events (not this build?)")
        if self.stream is not None:
            if resumed:
                self.rep.resumed += 1
            else:
                self.rep.fail(f"reconnect not resumed (stream {self.stream} → {stream}, had {self.last}, hub at {seq})")
        if not resumed:
            self.stream, self.last = stream, seq or 0
        if self.lost_at is not None:
            outage = now - self.lost_at
            self.rep.outages.append(outage)
            if outage > self.max_outage:
                self.rep.fail(f"no connection for {outage:.1f}s (the apps show it after {self.max_outage:.0f}s)")
            self.lost_at = None
        self.restarting = False

    async def run(self, stop):
        headers = {"Authorization": "Bearer " + self.token}
        while not stop.is_set():
            try:
                async with websockets.connect(self.url(), additional_headers=headers, open_timeout=20, max_size=64 << 20, ping_interval=20) as ws:
                    self.ws = ws
                    async for raw in ws:
                        m = json.loads(raw)
                        t = m.get("type")
                        if t == "hello":
                            self.on_hello(m)
                            for (mid, sid) in list(self.watch):
                                await self.subscribe(mid, sid)
                            self.connected.set()
                            continue
                        if "event_seq" in m:
                            self.on_event_seq(m["event_seq"])
                        if t == "hub_restarting":
                            self.restarting = True
                        elif t == "items":
                            key = (m.get("machine_id"), m.get("session_id"))
                            if key in self.watch:
                                got = self.items.setdefault(key, {})
                                for it in m.get("items") or []:
                                    got[it.get("seq", 0)] = it
                                    self.watch[key] = max(self.watch[key], it.get("seq", 0))
                                if m.get("to_seq"):
                                    self.watch[key] = max(self.watch[key], m["to_seq"])
                        elif t == "delta":
                            key = (m.get("machine_id"), m.get("session_id"))
                            if key in self.watch:
                                before, text = self.live.get(key, ""), m.get("text") or ""
                                if text and before and not text.startswith(before[: max(0, len(before) - 1)]) and not before.startswith(text):
                                    self.rep.note(f"live text changed course ({len(before)} → {len(text)} chars): a new message")
                                self.live[key] = text
            except (OSError, websockets.exceptions.WebSocketException, asyncio.TimeoutError) as e:
                code = getattr(getattr(e, "rcvd", None), "code", None)
                if code not in (None, 1001) and not stop.is_set():
                    self.rep.note(f"connection ended: {e}")
            self.ws = None
            self.connected.clear()
            if stop.is_set():
                return
            if self.lost_at is None:
                self.lost_at = time.monotonic()
            await asyncio.sleep(0.15 if self.restarting else 0.3)


def text_of(items):
    """What the assistant itself wrote (not the prompt, not the commands it ran)."""
    return "\n".join(it.get("text") or "" for _, it in sorted(items.items())
                     if it.get("role") == "assistant" and not it.get("tool_name") and not it.get("tool_use_id"))


async def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    cfg = hub_json()
    listen = cfg.get("listen") or ":18660"
    ap.add_argument("--hub", default="http://127.0.0.1:" + listen.rsplit(":", 1)[-1])
    ap.add_argument("--token", default=os.environ.get("PERSONA_HUB_TOKEN") or cfg.get("token"))
    ap.add_argument("--restarts", type=int, default=3)
    ap.add_argument("--interval", type=float, default=15, help="seconds between restarts")
    ap.add_argument("--restart-cmd", default="systemctl --user restart personad")
    ap.add_argument("--max-outage", type=float, default=8, help="the apps' grace before they show a lost connection")
    ap.add_argument("--converse", action="store_true", help="also stream an assistant conversation through the restarts")
    ap.add_argument("--count", type=int, default=300, help="--converse: how many numbered lines the answer has")
    ap.add_argument("--timeout", type=float, default=900)
    args = ap.parse_args()
    if not args.token:
        print("FAIL: no token (--token, PERSONA_HUB_TOKEN or hub.json)")
        return 2
    rep = Report()
    stop = asyncio.Event()
    client = Client(args.hub, args.token, rep, args.max_outage)
    tasks = [asyncio.create_task(probe(args.hub, rep, stop)), asyncio.create_task(client.run(stop))]
    deadline = time.monotonic() + args.timeout
    verdict = None
    try:
        await asyncio.wait_for(client.connected.wait(), 30)
        print(f"connected: stream {client.stream} at {client.last}", flush=True)
        talk = None
        if args.converse:
            prompt = (f"这是 Persona 的重启验证，不要用别的工具，按顺序做两件事：1) 用 Bash 工具的 run_in_background 跑 "
                      f"`sleep 75 && echo restart-check-bg-done`；2) 不等它，直接逐行输出从 1 到 {args.count} 的整数，每行一个数字，"
                      f"不加别的文字。之后等后台命令结束，再回复一行它的输出。")
            res = await asyncio.to_thread(api, args.hub, args.token, "POST", "/api/v1/assistant/conversations", {"prompt": prompt})
            s = res["session"]
            talk = (s["machine_id"], s["session_id"])
            await client.subscribe(*talk)
            print(f"conversation {talk[0]}/{talk[1][:8]}: asked for {args.count} lines and a background command", flush=True)
            for _ in range(600):  # until it is streaming
                if len(client.live.get(talk, "")) > 20:
                    break
                await asyncio.sleep(0.1)
        for i in range(args.restarts):
            await asyncio.sleep(args.interval if i else 3)
            if time.monotonic() > deadline:
                verdict = "TIMEOUT"
                break
            print(f"restart {i + 1}/{args.restarts}: {args.restart_cmd}", flush=True)
            t0 = time.monotonic()
            r = await asyncio.to_thread(subprocess.run, args.restart_cmd, shell=True, capture_output=True, text=True, timeout=120)
            if r.returncode != 0:
                rep.fail(f"restart command failed: {r.stderr.strip() or r.stdout.strip()}")
            rep.restarts += 1
            # The client notices on its own (hub_restarting, then the close); wait for it to be back.
            for _ in range(600):
                if client.connected.is_set() and client.lost_at is None and client.rep.connections > i + 1:
                    break
                await asyncio.sleep(0.05)
            print(f"  back after {time.monotonic() - t0:.1f}s (command included); outage seen by the client: "
                  f"{rep.outages[-1] if rep.outages else 0:.2f}s", flush=True)
        if talk and verdict is None:
            print("waiting for the conversation to finish …", flush=True)
            while time.monotonic() < deadline:
                text = text_of(client.items.get(talk, {}))
                if "restart-check-bg-done" in text and str(args.count) in text:
                    break
                await asyncio.sleep(1)
            text = text_of(client.items.get(talk, {}))
            numbers = [int(x) for x in re.findall(r"(?m)^\s*(\d+)\s*$", text)]
            want = list(range(1, args.count + 1))
            if not all(n in numbers for n in want):
                missing = [n for n in want if n not in numbers]
                rep.fail(f"the streamed answer is incomplete: {len(missing)} of {args.count} lines missing (first {missing[:5]})")
            if "restart-check-bg-done" not in text:
                rep.fail("the background command never reported back (lost in a restart?)")
            if re.search(r"personad 刚重启过|persona 刚重启过|后台任务.*停了", text, re.I):
                rep.fail("the conversation was told its background tasks were lost in a restart")
            print(f"conversation: {len(set(numbers) & set(want))}/{args.count} lines, background done: {'restart-check-bg-done' in text}", flush=True)
        if verdict is None and time.monotonic() > deadline:
            verdict = "TIMEOUT"
    except asyncio.TimeoutError:
        verdict = "TIMEOUT"
    finally:
        stop.set()
        for t in tasks:
            t.cancel()
        await asyncio.gather(*tasks, return_exceptions=True)
    verdict = verdict or ("PASS" if not rep.failures else "FAIL")
    worst = max(rep.outages) if rep.outages else 0
    print(f"{verdict}: {rep.restarts} restarts, {rep.connections} connections ({rep.resumed} resumed), {rep.events} events in order, "
          f"longest outage {worst:.2f}s, health probes {rep.probes} ({rep.probe_failures} failed, slowest {rep.probe_slowest:.2f}s), "
          f"{len(rep.failures)} failures")
    return 0 if verdict == "PASS" else 1


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
