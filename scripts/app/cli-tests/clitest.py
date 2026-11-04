"""clitest: a small harness for UI tests written against the runtime CLI (scripts/app/personactl.py) instead of XCUITest.

A case is a function taking a `T`. `run(cases, setup, runs, launch)` launches the app once, calls `setup(t)` before each
case (no relaunch: the mock is reset and the app moved back to where the case starts), times every case, prints one line
per run and a summary, and exits non-zero when any run failed. docs/runtime-cli.md「用 CLI 写的 UI 用例」.
"""
import json
import os
import subprocess
import sys
import time
import traceback
import urllib.request

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), ".."))
from personactl import Ctl, CtlError  # noqa: E402,F401

# What XCUITest's launchPinned() + launchJarvis() pass: the old tab-bar layout (unless JARVIS_LAYOUT=shell) and the
# pinned style. The cases launch the same way, or they would test another layout.
PINNED = ["-ui.oldLayout", "YES", "-ui.style", os.environ.get("JARVIS_UI_STYLE", "glass")]


class Fail(Exception):
    pass


def mock(c, path):
    """GET <hub>/mock/<path> on scripts/mock/mock_hub.py (reset, seen, churn…), as JSON."""
    with urllib.request.urlopen(c.hub.rstrip("/") + "/mock/" + path, timeout=10) as r:
        return json.load(r)


def allow_alerts(c, wait=4):
    """A system alert over the app (the notification prompt an earlier `xcodebuild test` leaves) answered with 允许.
    System alerts are not in the app's own tree: found through `idb ui describe-all`."""
    deadline = time.time() + wait
    while time.time() < deadline:
        if c.call("state").get("appState") == "active":
            return
        out = subprocess.run(["idb", "ui", "describe-all", "--udid", c.udid], capture_output=True, text=True).stdout
        try:
            items = json.loads(out)
        except ValueError:
            items = []
        for e in items:
            if e.get("AXLabel") in ("允许", "Allow") and e.get("frame"):
                f = e["frame"]
                c._idb("tap", round(f["x"] + f["width"] / 2), round(f["y"] + f["height"] / 2))
                break
        time.sleep(0.5)


class T:
    def __init__(self, ctl):
        self.c = ctl

    def find(self, **f):
        return self.c.call("find", **f)["element"]

    def see(self, what, timeout=10, **f):
        try:
            return self.c.call("wait", timeout=timeout, **f)["element"]
        except CtlError:
            raise Fail(f"{what} not found ({f})")

    def gone(self, what, timeout=10, **f):
        try:
            self.c.call("wait", timeout=timeout, gone=True, **f)
        except CtlError:
            raise Fail(f"{what}: still there ({f})")

    def label(self, **f):
        e = self.find(**f)
        if not e:
            raise Fail(f"no element {f}")
        return e["label"]

    def tap(self, what, **f):
        self.see(what, **f)
        return self.c.tap(**f)

    def settle(self):
        self.c.call("settle", timeout=3, stable=2, pixels=600)

    def reach(self, what, **f):
        """Swipes up until the element is where a touch reaches it, as XCUITest's reach (the list builds rows lazily:
        one below the fold is not in the tree yet)."""
        for _ in range(8):
            e = self.find(**f)
            if e and e["visible"]:
                return e
            self.c.swipe("up", distance=300, duration=0.5)
            self.c.call("settle", timeout=2, stable=2, every=100)
        return self.see(what, timeout=2, **f)

    def until(self, what, test, timeout=10, got=None):
        deadline = time.time() + timeout
        while time.time() < deadline:
            if test():
                return
            time.sleep(0.2)
        if not test():
            raise Fail(what + (f" (now {got()!r})" if got else ""))

    def eq(self, a, b, what=""):
        if a != b:
            raise Fail(f"{what}: {a!r} != {b!r}")

    def true(self, v, what):
        if not v:
            raise Fail(what)


def to_agents(t):
    """The Agents list at its root, on a new view tree; a chat still open is closed with its back button."""
    t.c.call("navigate", scene="agents")
    for _ in range(4):
        if t.find(id="agents.filter.all"):
            return
        if t.find(label="返回", trait="button", visibleOnly=True):  # not a hidden page's: tap acts only on a reachable one
            t.c.tap(label="返回", trait="button")
        t.settle()
    t.see("the Agents list", timeout=5, id="agents.filter.all")


def run(cases, setup, runs, launch):
    """`launch(ctl)` once, then every case `runs` times, each after `setup(t)`. CLITEST_LOG: one tab-separated line per
    run appended there (case, run, ok, seconds, why)."""
    c = Ctl()
    t = T(c)
    t0 = time.time()
    launch(c)
    allow_alerts(c)
    out = []
    for r in range(runs):
        for case in cases:
            s = time.time()
            ok, why = True, ""
            try:
                setup(t)
                case(t)
            except (Fail, CtlError) as e:
                ok, why = False, str(e)
            except Exception as e:  # a bug in the script itself
                ok, why = False, "".join(traceback.format_exception_only(type(e), e)).strip()
            d = time.time() - s
            out.append((case.__name__, r + 1, ok, d, why))
            print(f"{case.__name__}\trun {r + 1}\t{'PASS' if ok else 'FAIL'}\t{d:.1f} s\t{why}", flush=True)
    print(f"total {time.time() - t0:.1f} s")
    for case in cases:
        rows = [o for o in out if o[0] == case.__name__]
        ts = sorted(o[3] for o in rows)
        print(f"SUMMARY {case.__name__}: {sum(o[2] for o in rows)}/{len(rows)} passed, "
              f"{ts[0]:.1f} / {ts[len(ts) // 2]:.1f} / {ts[-1]:.1f} s (min / median / max)")
    if os.environ.get("CLITEST_LOG"):
        with open(os.environ["CLITEST_LOG"], "a") as f:
            for o in out:
                f.write("\t".join(map(str, o)) + "\n")
    return all(o[2] for o in out)


def main(cases, setup, launch):
    """`<script> [runs] [case …]`: the named cases (all without names), `runs` times (1)."""
    runs = int(sys.argv[1]) if len(sys.argv) > 1 else 1
    pick = sys.argv[2:]
    chosen = [c for c in cases if not pick or c.__name__ in pick]
    if not chosen:
        sys.exit(f"no such case: {pick} (cases: {[c.__name__ for c in cases]})")
    sys.exit(0 if run(chosen, setup, runs, launch) else 1)
