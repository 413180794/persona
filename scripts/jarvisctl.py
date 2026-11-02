#!/usr/bin/env python3
"""jarvisctl: drive a running DEBUG Jarvis in the simulator through its runtime CLI (docs/runtime-cli.md).

  jarvisctl.py launch --hub http://127.0.0.1:18760 [-- <more launch args>]   terminate, launch with the channel, wait
  jarvisctl.py ping | state
  jarvisctl.py settle [--timeout s] [--pixels n] [--stable n]                 wait until the screen stops changing
  jarvisctl.py online [--timeout s]                                           wait for the hub's live connection
  jarvisctl.py screenshot <out.png> [--no-settle]                             settle, then `simctl io screenshot`
  jarvisctl.py call <command> [key=value …]                                   any command the app knows

The simulator is $JARVISCTL_UDID (or --udid, default "booted"); the app is $JARVISCTL_BUNDLE / $JARVIS_BUNDLE_ID.
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

    def screenshot(self, path, settle=True, **settle_args):
        """`simctl io screenshot` (status bar included, as the identity shots), after the screen settles."""
        if settle:
            self.call("settle", **({"timeout": 4, "stable": 3, "pixels": 600} | settle_args))
        sh("xcrun", "simctl", "io", self.udid, "screenshot", path)
        return path


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
    elif a.cmd == "online":
        r = c.call("online", timeout=a.timeout)
    elif a.cmd == "screenshot":
        if not a.rest:
            p.error("screenshot needs a path")
        r = {"path": c.screenshot(a.rest[0], not a.no_settle)}
    elif a.cmd == "call":
        if not a.rest:
            p.error("call needs a command")
        kv = dict(x.partition("=")[::2] for x in a.rest[1:])
        r = c.call(a.rest[0], **{k: value(v) for k, v in kv.items()})
    else:
        p.error(f"unknown command {a.cmd}")
    print(json.dumps(r, ensure_ascii=False))


if __name__ == "__main__":
    try:
        main(sys.argv[1:])
    except CtlError as e:
        print(f"jarvisctl: {e}", file=sys.stderr)
        sys.exit(1)
    except OSError as e:
        print(f"jarvisctl: cannot reach the app ({e}); launched with `jarvisctl.py launch`?", file=sys.stderr)
        sys.exit(1)
