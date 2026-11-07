#!/usr/bin/env python3
"""Repeatable mixed-version hub/manager and frozen-client contract checks.

Requires an explicitly supplied Postgres admin URL in PERSONA_COMPAT_DATABASE_URL.
Only freshly created, randomly named fixture databases are mutated or dropped.
No production hub, resident node, agent, deployment or release command is run.
"""
from __future__ import annotations

import argparse
import importlib.util
import json
import os
from pathlib import Path
import secrets
import signal
import shlex
import socket
import subprocess
import sys
import tempfile
import time
from urllib.error import HTTPError, URLError
from urllib.parse import urlsplit, urlunsplit
from urllib.request import Request, urlopen

ROOT = Path(__file__).resolve().parents[2]
BASELINE = "39a706a338"
_spec = importlib.util.spec_from_file_location("rolling_fixtures", Path(__file__).with_name("rolling_compat_fixtures.py"))
fixtures = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(fixtures)

DIAGNOSTIC_DIR = None


def command(argv, *, env=None, cwd=ROOT, timeout=300):
    try:
        result = subprocess.run(argv, cwd=cwd, env=env, capture_output=True, text=True, timeout=timeout)
    except subprocess.TimeoutExpired as exc:
        retain_diagnostics(exc.stdout, exc.stderr)
        raise RuntimeError("command timed out: " + Path(argv[0]).name) from None
    if result.returncode:
        # Diagnostics can contain fixture credentials: retain them in the private
        # workspace, never in public verdicts, exception messages or command lines.
        retain_diagnostics(result.stdout, result.stderr)
        raise RuntimeError("command failed: " + Path(argv[0]).name + "; private diagnostics retained")
    return result.stdout


def retain_diagnostics(stdout, stderr):
    if DIAGNOSTIC_DIR is None:
        return
    descriptor, _ = tempfile.mkstemp(prefix="command-failure-", suffix=".log", dir=DIAGNOSTIC_DIR)
    with os.fdopen(descriptor, "wb") as file:
        for output in (stdout, stderr):
            if output:
                file.write(output.encode() if isinstance(output, str) else output)
                file.write(b"\n")

def port():
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]

def request(url, token, path, data=None, headers=None):
    body = None if data is None else json.dumps(data).encode()
    req = Request(url + path, data=body, headers=headers or fixtures.request_headers(token))
    with urlopen(req, timeout=10) as response:
        return json.load(response)

def wait_for(fn, predicate=lambda x: bool(x), timeout=90):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            result = fn()
            if predicate(result):
                return result
        except (URLError, HTTPError, OSError, ValueError):
            pass
        time.sleep(0.25)
    raise TimeoutError("isolated fixture did not become ready")

def sessions(payload):
    return payload if isinstance(payload, list) else payload.get("sessions", [])

def start(argv, env, logfile):
    log = logfile.open("wb")
    proc = subprocess.Popen(argv, env=env, cwd=ROOT, stdout=log, stderr=log, start_new_session=True)
    log.close()
    return proc

def stop(proc):
    if proc.poll() is not None:
        return
    os.killpg(proc.pid, signal.SIGTERM)
    try:
        proc.wait(timeout=10)
    except subprocess.TimeoutExpired:
        os.killpg(proc.pid, signal.SIGKILL)
        proc.wait(timeout=10)

def test_case(work, db_tool, old_bin, new_bin, old_node, new_node, hub_version, manager_version, node_version, args, real_node=False):
    case = work / (f"hub-{hub_version}-manager-{manager_version}-node-{node_version}" + ("-real" if real_node else ""))
    case.mkdir()
    db_name = "persona_compat_" + secrets.token_hex(8)
    admin = os.environ["PERSONA_COMPAT_DATABASE_URL"]
    base = urlsplit(admin)
    database = urlunsplit(base._replace(path="/" + db_name))
    db_env = dict(os.environ, PERSONA_COMPAT_DATABASE_URL=admin)
    command([str(db_tool), "db-create", db_name], env=db_env)
    procs = []
    try:
        home = case / "home"
        state = home / ".persona"
        state.mkdir(parents=True)
        (home / fixtures.LEGACY_ROOT).symlink_to(".persona")
        fake_bin = case / "bin"
        fake_bin.mkdir()
        node_bin = old_node if node_version == "old" else new_node
        ssh = fake_bin / "ssh"
        if real_node:
            bridge = [sys.executable, "-I", str(ROOT / "scripts/test/compat_node_bridge.py"), "--binary", str(old_bin if node_version == "old" else new_bin), "--handshake-log", str(case / "node-handshake.json"), "--image", args.node_image]
            ssh.write_text("#!/bin/sh\nexec " + shlex.join(bridge) + "\n")
        else:
            ssh.write_text("#!/bin/sh\nexec " + shlex.quote(str(node_bin)) + "\n")
        ssh.chmod(0o755)
        url = f"http://127.0.0.1:{port()}"
        token = secrets.token_hex(32)
        config = {"listen": url.removeprefix("http://"), "public_url": url, "database_url": database, "token": token,
                  "assistant": {"external": True}, "machines": [{"id": "compat-machine", "name": "Fixture computer", "kind": "ssh", "ssh_host": "fixture.invalid", "botmux": False, "herdr_autostart": False}]}
        config_file = state / "hub.json"
        config_file.write_text(json.dumps(config))
        config_file.chmod(0o600)
        env = fixtures.paired_env({"HOME": str(home), "PATH": str(fake_bin) + os.pathsep + os.environ["PATH"],
                                   "PERSONA_HOME": str(state), "PERSONA_LOCAL_NODE": "0", "PERSONA_ASSISTANT_DEPLOY": "child",
                                   "PERSONA_COMPAT_SEND_LOG": str(case / "sends.log")})
        env["LANG"] = "C.UTF-8"
        hub_bin = old_bin if hub_version == "old" else new_bin
        manager_bin = old_bin if manager_version == "old" else new_bin
        procs.append(start([str(hub_bin), "hub"], env, case / "hub.log"))
        wait_for(lambda: request(url, token, "/api/v1/health"))
        token_output = command([str(hub_bin), "hub", "assistant-token"], env=env, timeout=30)
        bus_token = token_output.splitlines()[0].strip()
        manager_env = dict(env)
        manager_env.update(fixtures.paired_env({"PERSONA_ASSISTANT_TOKEN": bus_token, "PERSONA_ASSISTANT_HUB": url}))
        procs.append(start([str(manager_bin), "assistant"], manager_env, case / "manager.log"))
        if real_node:
            def online_machine():
                payload = request(url, token, "/api/v1/machines")
                values = payload if isinstance(payload, list) else payload.get("machines", [])
                return next((m for m in values if m.get("id") == "compat-machine" and m.get("online")), None)
            machine = wait_for(online_machine, timeout=90)
            assert machine.get("node_protocol") == 1, "real node handshake protocol was lost"
            handshake = wait_for(lambda: json.loads((case / "node-handshake.json").read_text()))
            expected = node_version == "new"
            assert ("persona_contracts_v1" in handshake.get("capabilities", [])) == expected, "real node naming capability does not match its version"
            assert isinstance(sessions(request(url, token, "/api/v1/sessions")), list)
            print(f"PASS real {hub_version} hub + real {manager_version} manager + real {node_version} node (isolated PID/network/home); handshake and sessions", flush=True)
            return
        listed = wait_for(lambda: sessions(request(url, token, "/api/v1/sessions")), lambda xs: any(x.get("session_id") == "compat-session" for x in xs))
        target = next(x for x in listed if x["session_id"] == "compat-session")
        assert target.get(fixtures.LEGACY_START_KEY) is True, "pre-cutover client lost session origin"
        if hub_version == "new":
            assert target.get("started_by_persona") is True, "canonical source field is missing"
            health = request(url, token, "/api/v1/health")
            assert "persona_contracts_v1" in health["features"], "contract capability is missing"
        count_file = case / "sends.log"
        before = len(count_file.read_text().splitlines()) if count_file.exists() else 0
        operation = "11111111-2222-4333-8444-" + secrets.token_hex(6)
        headers = fixtures.request_headers(token, operation)
        data = {"text": fixtures.LEGACY_CONTEXT + "Exercise frozen client send", "client_msg_id": operation}
        for _ in range(2):
            request(url, token, "/api/v1/sessions/compat-machine/compat-session/messages", data, headers)
        receipt = request(url, token, "/api/v1/ops/" + operation)
        assert receipt.get("state") == "done", "legacy operation header lost its receipt"
        after = len(count_file.read_text().splitlines())
        assert after == before + 1, "retry executed a second send"
        if args.clients and hub_version == "old":
            client_env = dict(os.environ, PERSONA_COMPAT_HUB=url, PERSONA_COMPAT_TOKEN=token,
                              PERSONA_COMPAT_MACHINE="compat-machine", PERSONA_COMPAT_SESSION="compat-session")
            before_client = len(count_file.read_text().splitlines())
            command(["npm", "exec", "vitest", "run", "src/lib/compatibility.live.test.ts"], env=client_env, cwd=ROOT / "web", timeout=180)
            assert len(count_file.read_text().splitlines()) == before_client + 1, "web retry executed more than one send"
            print("PASS new web API against real baseline hub", flush=True)
            if args.android:
                before_android = len(count_file.read_text().splitlines())
                gradle_args = shlex.split(os.environ.get("PERSONA_COMPAT_GRADLE_ARGS", ""))
                android_output = command([str(ROOT / "android/gradlew"), "--no-daemon", "--no-configuration-cache", "--no-build-cache", "--max-workers=1", *gradle_args,
                         ":core:hub:cleanTest", ":core:hub:test", "--rerun", "--tests", "persona.core.hub.ProtocolCompatibilityLiveTest"],
                        env=client_env, cwd=ROOT / "android", timeout=600)
                android_log = case / "android-probe.log"
                android_log.write_text(android_output)
                android_log.chmod(0o600)
                assert len(count_file.read_text().splitlines()) == before_android + 1, "Android replay did not execute exactly one send"
                print("PASS new Android protocol against real baseline hub", flush=True)
        print(f"PASS real {hub_version} hub + real {manager_version} manager + {node_version} baseline wire-node fixture; frozen client send/retry/receipt/source", flush=True)
        if args.hold:
            print("Fixture remains live; endpoint and token are in the private fixture config, not public output.", flush=True)
            deadline = time.monotonic() + args.hold
            while time.monotonic() < deadline:
                time.sleep(1)
    finally:
        for proc in reversed(procs):
            stop(proc)
        for owned in [db_name + "_manager", db_name]:
            command([str(db_tool), "db-drop", owned], env=db_env, timeout=40)

def main():
    global DIAGNOSTIC_DIR
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--baseline-worktree", type=Path)
    parser.add_argument("--workspace", type=Path, required=True)
    parser.add_argument("--old-bin", type=Path)
    parser.add_argument("--new-bin", type=Path)
    parser.add_argument("--clients", action="store_true")
    parser.add_argument("--real-nodes", action="store_true", help="Also run each node binary in a network/PID/home-isolated container")
    parser.add_argument("--node-image", default="debian:bookworm-slim", help="Already available public glibc image; no pull is performed")
    parser.add_argument("--android", action="store_true")
    parser.add_argument("--hold", type=int, default=0, help="Keep one isolated case live for this many bounded seconds")
    parser.add_argument("--case", choices=["all", "old", "new"], default="all")
    args = parser.parse_args()
    if not os.environ.get("PERSONA_COMPAT_DATABASE_URL"):
        raise RuntimeError("PERSONA_COMPAT_DATABASE_URL must be supplied explicitly")
    args.workspace.mkdir(parents=True, exist_ok=True)
    work = Path(tempfile.mkdtemp(prefix="rolling-", dir=args.workspace))
    work.chmod(0o700)
    DIAGNOSTIC_DIR = work
    baseline = args.baseline_worktree or work / "baseline"
    created = args.baseline_worktree is None
    try:
        if created:
            command(["git", "worktree", "add", "--detach", str(baseline), BASELINE])
        assert command(["git", "rev-parse", "HEAD"], cwd=baseline).strip() == command(["git", "rev-parse", BASELINE]).strip(), "wrong baseline"
        old_bin, new_bin = args.old_bin or work / "old-hub", args.new_bin or work / "new-hub"
        if args.old_bin is None:
            command(["go", "-C", str(baseline / "server"), "build", "-o", str(old_bin), "./cmd/personad"], timeout=600)
        if args.new_bin is None:
            command(["go", "-C", str(ROOT / "server"), "build", "-o", str(new_bin), "./cmd/personad"], timeout=600)
        # Build exactly the same test-only fixture against both versions' wire
        # structs. The baseline checkout receives no product implementation patch.
        fixture = ROOT / "server/internal/compatprobe/fixture.go"
        baseline_fixture = baseline / "server/internal/compatprobe"
        baseline_fixture.mkdir(exist_ok=True)
        target = baseline_fixture / "fixture.go"
        if target.exists() and target.read_bytes() != fixture.read_bytes():
            raise RuntimeError("existing baseline probe differs; inspect it first")
        target.write_bytes(fixture.read_bytes())
        old_node, new_node = work / "old-wire-node", work / "new-wire-node"
        command(["go", "-C", str(baseline / "server"), "build", "-o", str(old_node), "./internal/compatprobe"], timeout=300)
        command(["go", "-C", str(ROOT / "server"), "build", "-o", str(new_node), "./internal/compatprobe"], timeout=300)
        cases = [("new", "old", "old"), ("old", "new", "new"), ("new", "new", "old"), ("old", "old", "new")]
        for case in cases:
            if args.case != "all" and case[0] != args.case:
                continue
            test_case(work, new_node, old_bin, new_bin, old_node, new_node, *case, args)
            if args.hold:
                break
        if args.real_nodes:
            command(["docker", "version", "--format", "{{.Server.Version}}"], timeout=15)
            for case in cases:
                if args.case != "all" and case[0] != args.case:
                    continue
                test_case(work, new_node, old_bin, new_bin, old_node, new_node, *case, args, real_node=True)
    finally:
        if created:
            command(["git", "worktree", "remove", "--force", str(baseline)])
    print("PASS rolling compatibility matrix (local isolated instances; no production operation)")

if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, TimeoutError, AssertionError, subprocess.TimeoutExpired, OSError, ValueError) as error:
        print("FAIL rolling compatibility: " + str(error), file=sys.stderr)
        sys.exit(1)
