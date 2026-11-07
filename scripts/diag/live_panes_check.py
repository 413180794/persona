#!/usr/bin/env python3
"""Live sessions against the truth, on every machine: is each herdr pane listed with the session it really runs, and is
a Codex pane whose turn is open listed as working? (docs/archive/integrations/session-verify.md "Sessions in progress")

Run on the hub machine: python3 live_panes_check.py [machine id…]
Reads ~/.persona/hub.json (token, machines), asks the hub's /api/v1/sessions, runs pane_probe.py on each machine
(locally for kind=local, else over ssh to ssh_host). Prints one line per mismatch and a RESULT line; prints no
session content.
"""
import json, os, subprocess, sys, urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
cfg = json.load(open(os.path.expanduser("~/.persona/hub.json")))
port = cfg.get("listen", ":18660").rsplit(":", 1)[-1]
req = urllib.request.Request(f"http://127.0.0.1:{port}/api/v1/sessions", headers={"Authorization": "Bearer " + cfg["token"]})
listed = json.load(urllib.request.urlopen(req, timeout=60))["sessions"]
by_pane = {(s["machine_id"], s.get("pane_id")): s for s in listed if s.get("pane_id")}
probe = open(os.path.join(HERE, "pane_probe.py"), "rb").read()

only = set(sys.argv[1:])
checked = wrong = unverifiable = status_wrong = 0
for m in cfg["machines"]:
    if only and m["id"] not in only:
        continue
    cmd = ["python3", "-"] if m.get("kind") == "local" else ["ssh", "-o", "ConnectTimeout=10", m["ssh_host"], "python3", "-"]
    try:
        res = subprocess.run(cmd, input=probe, capture_output=True, timeout=120)
        panes = json.loads(res.stdout)
    except Exception as e:
        print(f"{m['id']}: probe failed: {e}")
        continue
    for p in panes:
        s = by_pane.get((m["id"], p["pane"]))
        if not p.get("truth"):
            unverifiable += 1
            continue
        checked += 1
        if not s or s["session_id"] != p["truth"]:
            wrong += 1
            print(f"{m['id']} {p['pane']} {p['agent']}: listed {s and s['session_id']}, runs {p['truth']} ({p['how']})")
            continue
        # An open Codex turn written to within two minutes is work in progress, whatever herdr's screen says.
        if p.get("turn") == "open" and p.get("age", 1e9) < 120 and s.get("status") != "working":
            status_wrong += 1
            print(f"{m['id']} {p['pane']} codex: turn open, written {p['age']}s ago, listed {s.get('status')}")
print(f"RESULT: {checked} panes checked, {wrong} on the wrong session, {status_wrong} with the wrong status, "
      f"{unverifiable} unverifiable (no thread name / no pid file)")
