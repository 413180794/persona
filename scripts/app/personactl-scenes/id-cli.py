#!/usr/bin/env python3
"""id-cli.py <out dir> <combos…>: the identity shots of scenes-id1.txt through the runtime CLI: one launch, then for
every combo (<style>:<light|dark>) × scene: set the style, the appearance, go to the scene on a new view tree (`navigate`
resets it, as a launch would), wait for the screen to settle, `simctl io screenshot`. Same file names as id-old.sh; the time per shot in <out dir>/times.tsv.

  PERSONACTL_UDID=<udid> id-cli.py out/cli glass:light glass:dark …    (mock hub on 127.0.0.1:18897, as id-old.sh)

FREEZE=1: `freeze` before each shot (Reduce Motion, running animations removed: the same pixels run after run; a
needs-you dot is then drawn still, so compare such shots with shots taken the same way, not with id-old.sh's).
"""
import os
import sys
import time

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), ".."))
from personactl import Ctl, CtlError  # noqa: E402

ERR = "server/b2e4d6f8-0000-4000-8000-000000000001"
CHAT = "macbook/a1f3c2d4-0000-4000-8000-000000000001"
# A page that changes by itself on a timer, not when data comes in: the chat's open header folds after 3 s idle
# (ChatView.chromeIdleSeconds); the old shots, 7 s after the launch, always have it folded. Seconds after navigate.
AT_LEAST = {"chat": 3.6}
# scenes-id1.txt, as navigate calls: (scene, session, launch-only flags the page reads).
SCENES = [
    ("home", dict(scene="home")),
    ("agents", dict(scene="agents")),
    ("chat", dict(scene="chat", session=CHAT, defaults={"openAllWork": True})),
    ("inbox", dict(scene="inbox")),
    ("settings", dict(scene="settings")),
    ("machines", dict(scene="machines")),
    ("error", dict(scene="assistant", session=ERR)),
]


def main():
    out, combos = sys.argv[1], sys.argv[2:]
    os.makedirs(out, exist_ok=True)
    c = Ctl()
    t0 = time.time()
    c.launch(os.environ.get("HUB", "http://127.0.0.1:18897"))
    c.navigate = lambda **a: c.call("navigate", **a)
    c.appearance(combos[0].split(":")[1])
    # Warm up as id-old.sh does (paired, sessions loaded): the Agents list, then wait for it to settle.
    c.navigate(scene="agents")
    c.call("settle", timeout=20, stable=6)
    freeze = os.environ.get("FREEZE") == "1"
    times = []
    ap = None
    try:
        for combo in combos:
            style, appear = combo.split(":")
            if appear != ap:
                c.appearance(appear)
                ap = appear
            c.call("set-style", style=style)
            for name, nav in SCENES:
                s = time.time()
                restarts = c.online(15).get("mockRestarts", [])
                flags = {"openAllWork": False}
                flags.update(nav.get("defaults", {}))
                if freeze:
                    c.call("freeze", on=False)  # a transition must end: navigate first, freeze before the shot
                c.navigate(**dict(nav, defaults=flags, coldChats=os.environ.get("COLD", "1") == "1"))
                c.idle()  # the page's data in (machines' metrics come after the page)
                if "session" in nav:
                    c.call("wait-loaded", timeout=15)  # the chat's history from the hub, not only the disk cache's
                time.sleep(max(0.0, AT_LEAST.get(name, 0) - (time.time() - s)))
                if freeze:
                    c.call("freeze")
                r = c.call("settle", timeout=10, stable=int(os.environ.get("STABLE", "4")), every=150, pixels=int(os.environ.get("PIXELS", "600")))
                f = f"{style}-{appear}-{name}.png"
                c.screenshot(os.path.join(out, f), settle=False)
                times.append((f, time.time() - s, r.get("settled"), r.get("frames")))
    finally:
        if freeze:  # the simulator's Reduce Motion back as it was, also when a shot failed
            try:
                c.call("freeze", on=False)
            except (OSError, CtlError, ValueError) as e:  # ValueError: a dying app's half answer
                print(f"WARNING freeze not turned off ({e}): the app's next launch puts Reduce Motion back")
    with open(os.path.join(out, "times.tsv"), "w") as fh:
        for f, t, ok, n in times:
            fh.write(f"{f}\t{t:.2f}\t{ok}\t{n}\n")
    print(f"DONE id-cli {out}: {len(times)} shots in {time.time() - t0:.0f} s "
          f"({sum(1 for t in times if not t[2])} not settled)")
    if restarts:  # mock_scenarios.py had to start the hub again: shots around then may show "Connecting..."
        print(f"WARNING the mock hub was restarted {len(restarts)} time(s): {restarts}")


if __name__ == "__main__":
    main()
