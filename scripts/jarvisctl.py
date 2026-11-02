#!/usr/bin/env python3
"""jarvisctl: drive a running DEBUG Jarvis in the simulator through its runtime CLI (docs/runtime-cli.md).

  jarvisctl.py launch --hub http://127.0.0.1:18760 [-- <more launch args>]   terminate, launch with the channel, wait
  jarvisctl.py ping | state
  jarvisctl.py settle [--timeout s] [--pixels n] [--stable n]                 wait until the screen stops changing
  jarvisctl.py online [--timeout s]                                           wait for the hub's live connection
  jarvisctl.py idle [--timeout s]                                             no request to the mock hub for 0.8 s
  jarvisctl.py screenshot <out.png> [--no-settle]                             settle, then `simctl io screenshot`
  jarvisctl.py reset [--cold-chats] [--drop-chat-cache]                      a new view tree, as after a launch
  jarvisctl.py navigate home|assistant|agents|chat|inbox|settings|machines [--session m/s] [--machine id]
               [--flag key=value …] [--keep] [--cold-chats]                    the page a launch switch opens
  jarvisctl.py set-style <glass|clay|island|lark|beautiful> [--color c]       界面风格 / 主题色
  jarvisctl.py appearance light|dark [--in-app]                               the simulator's (default) or the app's
  jarvisctl.py defaults [--set k=v …] [--remove k …] [--get k …]              this run's switches, saved keys
  jarvisctl.py tree [filters] | find [filters]                                the accessibility tree (what XCUITest sees)
  jarvisctl.py wait [filters] [--gone] [--timeout s]                          until an element is there (or gone)
  jarvisctl.py tap [filters] [--hid]                                          activate it; a real touch (idb) if it does not take that
  jarvisctl.py scroll [--pages n | --to top|bottom]                           the largest scroll view, no fling
  jarvisctl.py type --text T [filters] | dismiss-keyboard                     into the focused field (filters: focus that first)
  jarvisctl.py snapshot <out.png>                                             the app's windows, no status bar
  jarvisctl.py perf | refresh                                                 view-body counts (-perfCounts YES) | pull the hub
  jarvisctl.py call <command> [key=value …]                                   any command the app knows

Element filters: --id, --id-prefix, --label, --label-contains, --label-prefix, --trait button|text|header|selected…,
--visible, --index n (else the first match on screen). The simulator is $JARVISCTL_UDID (or --udid, default "booted"); the app is $JARVISCTL_BUNDLE / $JARVIS_BUNDLE_ID.
`launch` picks a one-time token and keeps it, with the port, in ~/.cache/jarvisctl/<udid>.json for the later
commands. Standard library only; scripts can `from jarvisctl import Ctl`.
"""
import argparse
import json
import os
import secrets
import socket
import subprocess
import sys
import time
import urllib.parse
import urllib.request

STATE_DIR = os.path.expanduser("~/.cache/jarvisctl")
DEFAULT_PORT = 18950


class CtlError(Exception):
    pass


def sh(*cmd, check=True):
    return subprocess.run(cmd, check=check, text=True, capture_output=True)


class Ctl:
    def __init__(self, udid=None, bundle=None, port=None):
        self.udid = udid or os.environ.get("JARVISCTL_UDID") or "booted"
        self.bundle = bundle or os.environ.get("JARVISCTL_BUNDLE") or os.environ.get("JARVIS_BUNDLE_ID") or "com.example.jarvis"
        saved = self._load()
        self.port = port or saved.get("port") or DEFAULT_PORT
        self.token = saved.get("token", "")
        self.hub = saved.get("hub")

    def _path(self):
        return os.path.join(STATE_DIR, self.udid + ".json")

    def _load(self):
        try:
            with open(self._path()) as f:
                return json.load(f)
        except (OSError, ValueError):
            return {}

    def _save(self):
        os.makedirs(STATE_DIR, exist_ok=True)
        path = self._path()
        with open(path, "w") as f:
            json.dump({"port": self.port, "token": self.token, "hub": self.hub}, f)
        os.chmod(path, 0o600)

    def call(self, cmd, _timeout=None, **args):
        """One request per connection: a JSON line out, a JSON line back. Raises CtlError when the app says no."""
        req = json.dumps({"token": self.token, "cmd": cmd, "args": {k: v for k, v in args.items() if v is not None}})
        wait = _timeout or (args.get("timeout") or 0) + 30
        with socket.create_connection(("127.0.0.1", self.port), timeout=wait) as s:
            s.sendall(req.encode() + b"\n")
            buf = b""
            while not buf.endswith(b"\n"):
                chunk = s.recv(1 << 16)
                if not chunk:
                    break
                buf += chunk
        r = json.loads(buf or b'{"ok": false, "error": "no answer (did the app crash?)"}')
        if not r.get("ok"):
            raise CtlError(f"{cmd}: {r.get('error')}")
        return r

    def launch(self, hub=None, extra=(), wait=20):
        """Terminates the app and launches it with a fresh token (paired with `hub` when given); waits for `ping`."""
        self.hub = hub or self.hub
        self.token = secrets.token_hex(16)
        sh("xcrun", "simctl", "terminate", self.udid, self.bundle, check=False)
        args = ["-runtimeCLI", str(self.port), "-runtimeCLIToken", self.token]
        if self.hub:
            args += ["-pairURL", "jarvis://pair?url=" + urllib.parse.quote(self.hub, safe="") + "&token=mock"]
        sh("xcrun", "simctl", "launch", self.udid, self.bundle, *args, *extra)
        self._save()
        deadline = time.time() + wait
        while True:
            try:
                return self.call("ping", _timeout=2)
            except (OSError, CtlError, ValueError):
                if time.time() > deadline:
                    raise CtlError("the app did not answer (a DEBUG build? the port taken?)")
                time.sleep(0.2)

    def idle(self, quiet=0.8, timeout=10):
        """Network idle, seen from the mock hub (scripts/mock_hub.py counts every GET at /mock/hits): waits until no
        request has come in for `quiet` seconds. A page that is still waiting for its data (machines' metrics) looks
        settled to `settle`. Returns how many requests came in meanwhile."""
        if not self.hub:
            raise CtlError("idle needs the mock hub (launch --hub)")

        def total():
            with urllib.request.urlopen(self.hub.rstrip("/") + "/mock/hits", timeout=5) as r:
                # The app's requests only: these polls of /mock/hits are counted too.
                return sum(n for k, n in json.load(r).get("hits", {}).items() if " /mock/" not in k)

        start = last = total()
        changed, deadline = time.time(), time.time() + timeout
        while time.time() < deadline:
            time.sleep(0.1)
            now = total()
            if now != last:
                last, changed = now, time.time()
            elif time.time() - changed >= quiet:
                return {"ok": True, "idle": True, "requests": now - start}
        return {"ok": True, "idle": False, "requests": last - start}

    def tap(self, hid=False, **f):
        """Accessibility activation (a button's action, a menu opening), or with `hid` / when the element does not take
        it (a menu's items) a real touch at the middle of its frame through `idb ui tap`, once the screen settles (an
        opening menu moves its items under the touch)."""
        if not hid:
            r = self.call("activate", **f)
            if r.get("activated"):
                return r
        self.call("settle", timeout=2, stable=2, every=80, pixels=600)
        e = self.call("find", **f)["element"]
        if not e:
            raise CtlError(f"tap: not found {f}")
        x, y, w, h = e["frame"]
        for _ in range(2):  # idb's companion may need a first call to come up
            r = subprocess.run(["idb", "ui", "tap", "--udid", self.udid, str(round(x + w / 2)), str(round(y + h / 2))],
                               text=True, capture_output=True)
            if r.returncode == 0:
                return {"ok": True, "hid": True, "element": e}
            sh("idb", "connect", self.udid, check=False)
        raise CtlError(f"tap: idb failed: {r.stderr.strip()[-200:]}")

    def type(self, text, **f):
        """Types into the focused field; with filters, focuses that element first: accessibility activation, and when
        that leaves no field focused (a SwiftUI field often), a real touch on it."""
        try:
            return self.call("type", text=text, **f)
        except CtlError as e:
            if not f or "no text field has focus" not in str(e):
                raise
        self.tap(hid=True, **f)
        time.sleep(0.4)
        return self.call("type", text=text)

    def appearance(self, value, in_app=False):
        """Light or dark: the whole simulator (status bar included, as the screenshot scripts do) or the app only."""
        if in_app:
            return self.call("set-appearance", value=value)
        sh("xcrun", "simctl", "ui", self.udid, "appearance", value)
        return {"ok": True, "appearance": value}

    def screenshot(self, path, settle=True, **settle_args):
        """`simctl io screenshot` (status bar included, as the identity shots), after the screen settles."""
        if settle:
            self.call("settle", **({"timeout": 4, "stable": 3, "pixels": 600} | settle_args))
        sh("xcrun", "simctl", "io", self.udid, "screenshot", path)
        return path


def filters(a):
    """The element filters of the command line, as the app's commands take them."""
    f = {"id": a.id, "idPrefix": a.id_prefix, "label": a.label, "labelContains": a.label_contains,
         "labelPrefix": a.label_prefix, "trait": a.trait, "index": a.index, "visibleOnly": a.visible or None}
    return {k: v for k, v in f.items() if v is not None}


def value(v):
    """key=value from the command line: numbers and YES/NO as JSON, the rest as text."""
    if v in ("YES", "true"):
        return True
    if v in ("NO", "false"):
        return False
    try:
        return json.loads(v)
    except ValueError:
        return v


def main(argv):
    extra = []
    if "--" in argv:
        i = argv.index("--")
        argv, extra = argv[:i], argv[i + 1:]
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--udid")
    p.add_argument("--port", type=int)
    p.add_argument("--bundle")
    p.add_argument("--hub")
    p.add_argument("--timeout", type=float)
    p.add_argument("--pixels", type=int)
    p.add_argument("--stable", type=int)
    p.add_argument("--no-settle", action="store_true")
    p.add_argument("--session")
    p.add_argument("--machine")
    p.add_argument("--color")
    p.add_argument("--flag", action="append", default=[])
    p.add_argument("--set", action="append", default=[])
    p.add_argument("--remove", action="append", default=[])
    p.add_argument("--get", action="append", default=[])
    p.add_argument("--keep", action="store_true")
    p.add_argument("--in-app", action="store_true")
    for f in ("--id", "--id-prefix", "--label", "--label-contains", "--label-prefix", "--trait"):
        p.add_argument(f)
    p.add_argument("--index", type=int)
    p.add_argument("--visible", action="store_true")
    p.add_argument("--gone", action="store_true")
    p.add_argument("--hid", action="store_true")
    p.add_argument("--text")
    p.add_argument("--pages", type=float)
    p.add_argument("--to")
    p.add_argument("--cold-chats", action="store_true")
    p.add_argument("--drop-chat-cache", action="store_true")
    p.add_argument("cmd")
    p.add_argument("rest", nargs="*")
    a = p.parse_args(argv)
    c = Ctl(a.udid, a.bundle, a.port)
    if a.cmd == "launch":
        r = c.launch(a.hub, extra)
    elif a.cmd in ("ping", "state"):
        r = c.call(a.cmd)
    elif a.cmd == "settle":
        r = c.call("settle", timeout=a.timeout, pixels=a.pixels, stable=a.stable)
    elif a.cmd == "idle":
        r = c.idle(timeout=a.timeout or 10)
    elif a.cmd == "online":
        r = c.call("online", timeout=a.timeout)
    elif a.cmd == "screenshot":
        if not a.rest:
            p.error("screenshot needs a path")
        r = {"path": c.screenshot(a.rest[0], not a.no_settle)}
    elif a.cmd == "reset":
        r = c.call("reset", coldChats=a.cold_chats or None, dropChatCache=a.drop_chat_cache or None)
    elif a.cmd == "navigate":
        if not a.rest:
            p.error("navigate needs a scene")
        flags = {k: value(v) for k, v in (x.partition("=")[::2] for x in a.flag)}
        r = c.call("navigate", scene=a.rest[0], session=a.session, machine=a.machine, defaults=flags or None,
                   keep=a.keep or None, coldChats=a.cold_chats or None, dropChatCache=a.drop_chat_cache or None)
    elif a.cmd == "set-style":
        r = c.call("set-style", style=a.rest[0] if a.rest else None, color=a.color)
    elif a.cmd == "appearance":
        if not a.rest:
            p.error("appearance needs light or dark")
        r = c.appearance(a.rest[0], a.in_app)
    elif a.cmd == "defaults":
        sets = {k: value(v) for k, v in (x.partition("=")[::2] for x in a.set)}
        r = c.call("defaults", set=sets or None, remove=a.remove or None, get=a.get or None)
    elif a.cmd in ("tree", "find", "wait"):
        f = filters(a)
        if a.cmd == "wait":
            f.update(timeout=a.timeout, gone=a.gone or None)
        r = c.call(a.cmd, **f)
    elif a.cmd == "tap":
        r = c.tap(a.hid, **filters(a))
    elif a.cmd == "scroll":
        r = c.call("scroll", pages=a.pages, to=a.to)
    elif a.cmd == "type":
        r = c.type(a.text or "", **filters(a))
    elif a.cmd == "snapshot":
        if not a.rest:
            p.error("snapshot needs a path")
        r = c.call("snapshot", path=os.path.abspath(a.rest[0]))
    elif a.cmd in ("perf", "refresh", "dismiss-keyboard"):
        r = c.call(a.cmd)
    elif a.cmd == "call":
        if not a.rest:
            p.error("call needs a command")
        kv = dict(x.partition("=")[::2] for x in a.rest[1:])
        r = c.call(a.rest[0], **{k: value(v) for k, v in kv.items()})
    else:
        p.error(f"unknown command {a.cmd}")
    print(json.dumps(r, ensure_ascii=False, indent=1 if a.cmd == "tree" else None))


if __name__ == "__main__":
    try:
        main(sys.argv[1:])
    except CtlError as e:
        print(f"jarvisctl: {e}", file=sys.stderr)
        sys.exit(1)
    except OSError as e:
        print(f"jarvisctl: cannot reach the app ({e}); launched with `jarvisctl.py launch`?", file=sys.stderr)
        sys.exit(1)
