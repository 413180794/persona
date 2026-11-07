#!/usr/bin/env python3
"""mock_scenarios.py: scripts/mock/mock_hub.py whose data set can be switched while the app runs (docs/operations/runtime-cli.md "Injecting state").

    python3 scripts/mock/mock_scenarios.py --port 18897 --control 18896 [--scenario default]

mock_hub.py builds its data once, from MOCK_* environment variables, when it starts. This runs it as a child on --port
and answers on --control (127.0.0.1 only):
    GET /scenario                      the scenario and the MOCK_* it runs with
    GET /scenario?name=agents          restart the hub with that scenario's variables (below), wait until it answers
    GET /scenario?env=MOCK_X=1,MOCK_Y=2  any set of MOCK_* variables
    GET /scenario?name=offline         no hub at all: the app goes "Connecting..." / "Waiting for network"
The app reconnects by itself (`personactl.py scenario` waits for `online`). A hub that exits by itself (another agent's
`pkill -f mock_hub.py` took one down mid-run) is started again with the same scenario within a second; GET /scenario
lists those restarts (`restarts`: when, exit code, scenario) and `personactl.py online` reports them. Standard library
only; nothing in mock_hub.py changes, and it can still be run on its own as before.
"""
import argparse
import json
import os
import signal
import subprocess
import sys
import threading
import time
import urllib.parse
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

HERE = os.path.dirname(os.path.abspath(__file__))

# The data sets the UI tests and the screenshot scripts already use (tools: ui-regress, idrun), by name.
SCENARIOS = {
    "default": {},
    "quiet": {"MOCK_QUIET": "1"},
    "agents": {"MOCK_AGENTS": "1"},  # ~120 agents: AgentsListTests
    "churn": {"MOCK_AGENTS": "1", "MOCK_CHURN": "1"},  # pushes on demand: LiveUpdateTests
    "machines": {"MOCK_MACHINES": "1"},  # MachinePagesTests
    "errors": {"MOCK_API_ERRORS": "1", "MOCK_QUIET": "1"},  # the identity screenshots' error card
    "long": {"MOCK_LONG_HISTORY": "1"},  # ChatFollowTests
    "big": {"MOCK_BIG": "1"},  # LongSessionTests
    "rich": {"MOCK_RICH": "1"},  # RichReplyTests
    "tasks": {"MOCK_TASKS": "1"},  # BackgroundNoticeTests
    "offline": None,  # no hub
}
# The main conversation's readiness (docs/operations/testing.md "Readiness scenarios"): readiness-zero, readiness-not_logged_in…
sys.path.insert(0, HERE)
import readiness_mock  # noqa: E402

SCENARIOS.update({"readiness-" + name: {"MOCK_READINESS": name} for name in readiness_mock.SCENARIOS})


class Hub:
    def __init__(self, port, log):
        self.port, self.log = port, log
        self.child = None
        self.name, self.env = "offline", {}
        self.lock = threading.Lock()
        self.restarts = []
        self.closing = False
        threading.Thread(target=self.guard, daemon=True).start()

    def guard(self):
        """Starts the hub again when it exited without being asked (not a switch, not offline)."""
        while True:
            time.sleep(0.5)
            with self.lock:
                if self.closing or self.child is None or self.child.poll() is None:
                    continue
                rc = self.child.returncode
                self.restarts.append({"at": time.strftime("%H:%M:%S"), "rc": rc, "scenario": self.name})
                print(json.dumps({"restart": self.restarts[-1]}), flush=True)
                try:
                    self.start(self.env)
                except RuntimeError as e:
                    print(json.dumps({"restart failed": str(e)}), flush=True)

    def stop(self):
        if self.child and self.child.poll() is None:
            self.child.terminate()
            try:
                self.child.wait(5)
            except subprocess.TimeoutExpired:
                self.child.kill()
                self.child.wait()
        self.child = None

    def answers(self):
        try:
            urllib.request.urlopen(f"http://127.0.0.1:{self.port}/api/v1/machines", timeout=1).read()
            return True
        except OSError:
            return False

    def switch(self, name, env):
        """Stops the running hub and starts one with `env` (None: none); returns once it answers."""
        with self.lock:
            started = time.time()
            self.stop()
            self.name, self.env = name, env or {}
            if env is None:
                return {"scenario": name, "env": {}, "ms": int((time.time() - started) * 1000)}
            self.start(env)
            return {"scenario": name, "env": env, "ms": int((time.time() - started) * 1000)}

    def start(self, env):
        """mock_hub.py with `env` on the hub port; returns once it answers. Called with the lock held."""
        # The child gets only the scenario's MOCK_* (none inherited from this process).
        base = {k: v for k, v in os.environ.items() if not k.startswith("MOCK_")}
        out = open(self.log, "a") if self.log else subprocess.DEVNULL
        self.child = subprocess.Popen([sys.executable, "-u", os.path.join(HERE, "mock_hub.py")],
                                      env=dict(base, PORT=str(self.port), **env), stdout=out, stderr=out)
        deadline = time.time() + 30  # a big data set (MOCK_BIG) takes seconds to build
        while not self.answers():
            if self.child.poll() is not None:
                rc, self.child = self.child.returncode, None
                raise RuntimeError(f"mock_hub.py exited ({rc})")
            if time.time() > deadline:
                raise RuntimeError("mock_hub.py did not answer in 30 s")
            time.sleep(0.1)


def control(hub):
    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *a):
            pass

        def reply(self, code, obj):
            body = json.dumps(obj, ensure_ascii=False).encode()
            self.send_response(code)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def do_GET(self):
            u = urllib.parse.urlparse(self.path)
            if u.path != "/scenario":
                return self.reply(404, {"error": "GET /scenario[?name=…|?env=MOCK_X=1,…]"})
            q = urllib.parse.parse_qs(u.query)
            try:
                if "env" in q:
                    env = dict(p.split("=", 1) for p in q["env"][0].split(",") if "=" in p)
                    if any(not k.startswith("MOCK_") for k in env):
                        return self.reply(400, {"error": "only MOCK_* variables"})
                    return self.reply(200, hub.switch("custom", env))
                if "name" in q:
                    name = q["name"][0]
                    if name not in SCENARIOS:
                        return self.reply(400, {"error": f"unknown scenario {name}", "scenarios": sorted(SCENARIOS)})
                    return self.reply(200, hub.switch(name, SCENARIOS[name]))
                return self.reply(200, {"scenario": hub.name, "env": hub.env, "scenarios": sorted(SCENARIOS),
                                        "restarts": hub.restarts})
            except RuntimeError as e:
                return self.reply(500, {"error": str(e)})
    return Handler


def main():
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--port", type=int, default=18760)
    p.add_argument("--control", type=int)
    p.add_argument("--scenario", default="default")
    p.add_argument("--log", help="the hub's output (default: discarded)")
    a = p.parse_args()
    control_port = a.control or a.port - 1
    hub = Hub(a.port, a.log)
    print(json.dumps(hub.switch(a.scenario, SCENARIOS[a.scenario])), flush=True)
    server = ThreadingHTTPServer(("127.0.0.1", control_port), control(hub))
    print(f"control on 127.0.0.1:{control_port}, hub on {a.port}", flush=True)

    def interrupted(*_):
        raise KeyboardInterrupt

    # Stopped by port (`kill`), as CLAUDE.md asks: take the hub down too, not leave it behind on its port.
    signal.signal(signal.SIGTERM, interrupted)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        hub.closing = True
        with hub.lock:
            hub.stop()


if __name__ == "__main__":
    main()
