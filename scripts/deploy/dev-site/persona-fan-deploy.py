#!/usr/bin/env python3
"""Deploy the published fan/develop revision to the local Persona development hub."""

import contextlib
import ctypes
from datetime import date, datetime, timedelta, timezone
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import runpy
import shutil
import signal
import ssl
import subprocess
import sys
import tempfile
import time
import urllib.request


HOME = Path.home()
REPO = HOME / "projects/super-agent/persona-fan-develop"
EXCHANGE = HOME / "repos/persona-fan.git"
STATE = HOME / ".local/state/persona-fan-sync"
RUNTIME = REPO.resolve().parent / ".runtime/persona"
DEPLOYMENTS = RUNTIME / "deployments"
HUB_HOME = RUNTIME / "home"
BINARY = RUNTIME / "bin/personad"
WEB = HUB_HOME / "web"
UNIT = "persona-dev-hub.service"
ORIGIN = os.environ.get("PERSONA_FAN_HUB_ORIGIN", "").rstrip("/")
CA = HUB_HOME / "tls/persona-ca.crt"
BRANCH = "refs/heads/fan/develop"
SHANGHAI = timezone(timedelta(hours=8))
FULL_SHA = r"[0-9a-f]{40}"


class Busy(Exception):
    pass


def now():
    return datetime.now(SHANGHAI)


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def output(*args, env=None, timeout=12):
    return subprocess.check_output(args, text=True, env=env, timeout=timeout).strip()


def git(*args, directory=None):
    return output("git", "-C", directory or REPO, *args)


def atomic_json(path, value):
    with tempfile.NamedTemporaryFile(mode="w", dir=path.parent, delete=False) as stream:
        temporary = Path(stream.name)
        try:
            json.dump(value, stream, indent=2)
            stream.write("\n")
            stream.flush()
            os.fsync(stream.fileno())
            os.replace(temporary, path)
            directory_fd = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
            try:
                os.fsync(directory_fd)
            finally:
                os.close(directory_fd)
        finally:
            temporary.unlink(missing_ok=True)


def lock(stack, path):
    stream = stack.enter_context(path.open("a"))
    try:
        fcntl.flock(stream, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BlockingIOError as exc:
        raise Busy(f"deployment lock held: {path}") from exc


def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def request(path):
    http = urllib.request.build_opener(
        urllib.request.ProxyHandler({}),
        urllib.request.HTTPSHandler(context=ssl.create_default_context(cafile=str(CA))),
    )
    require(ORIGIN.startswith("https://"), "set PERSONA_FAN_HUB_ORIGIN to the site HTTPS URL")
    url = ORIGIN + path
    req = urllib.request.Request(url, headers={"Accept-Encoding": "identity", "Cache-Control": "no-cache"})
    with http.open(req, timeout=5) as response:
        require(response.status == 200 and response.geturl() == url, f"unexpected response for {path}")
        return response.read()


def health():
    status = json.loads(request("/api/v1/health"))
    require(status.get("ok") is True and status.get("update", {}).get("confirmed") is not False
            and status.get("version") and status.get("commit"), "hub health or commit is unavailable")
    return {key: status[key] for key in ("ok", "version", "commit")}


def full_commit(commit):
    require(re.fullmatch(r"[0-9a-f]{7,40}", commit), "live build has no clean Git commit")
    return git("rev-parse", "--verify", f"{commit}^{{commit}}")


def verify_web(expected):
    entry = request("/")
    require(hashlib.sha256(entry).hexdigest() == digest(expected / "index.html"),
            "served web index differs from the deployed artifact")
    script = re.search(rb'<script\b[^>]*\bsrc="(?:\./|/)?(assets/[\w.-]+\.js)"', entry)
    require(script is not None, "web entry script is missing")
    asset = script[1].decode("ascii")
    require(hashlib.sha256(request("/" + asset)).hexdigest() == digest(expected / asset),
            "served web entry script differs from the deployed artifact")
    return {"index_sha256": hashlib.sha256(entry).hexdigest(), "entry_asset": asset}


def ready(receipt):
    path = STATE / "dev.json"
    if not path.exists():
        receipt.update(result="skipped", reason="sync_receipt_missing")
        return None
    published = json.loads(path.read_text())
    day = date.fromisoformat(published["date"])
    commit = published["sha"]
    require(re.fullmatch(FULL_SHA, commit), "dev.json must contain a full sha")
    receipt.update(ready_date=day.isoformat(), to_commit=commit)
    require(day <= now().date(), "dev.json has a future date")
    if day < now().date():
        receipt.update(result="skipped", reason="sync_receipt_stale")
        return None
    return commit


def require_published(commit):
    require(git("symbolic-ref", "HEAD") == BRANCH, "deploy checkout is not on fan/develop")
    require(git("rev-parse", BRANCH) == commit
            and git("rev-parse", BRANCH, directory=EXCHANGE) == commit,
            "dev.json does not match both checkout and private exchange branch")


def require_rollback_compatible(previous, target):
    changes = git("diff", "--name-only", previous, target, "--", "server/internal/db/migrations")
    if changes:
        raise RuntimeError("database migrations changed and artifact-only rollback is unverified: "
                           + ", ".join(Path(path).name for path in changes.splitlines()))


def command_log(log, command, directory, env=None):
    with log.open("a") as stream:
        print("+ " + " ".join(map(str, command)), file=stream, flush=True)
        subprocess.run(command, cwd=directory, env=env, stdout=stream, stderr=subprocess.STDOUT, check=True)


def build(record, target):
    checkout = record / "source"
    log = record / "build.log"
    features = dict(os.environ, CGO_ENABLED="0", GOOS="linux", GOARCH="amd64")
    go = shutil.which("go") or "/usr/local/go/bin/go"
    try:
        command_log(log, ["git", "worktree", "add", "--detach", checkout, target], REPO)
        require(git("rev-parse", "HEAD", directory=checkout) == target, "build checkout differs from target")
        server = checkout / "server"
        command_log(log, [go, "generate", "./internal/runtimepkg"], server, features)
        build_number = datetime.now(timezone.utc).strftime("%Y%m%d%H%M")
        describe = git("describe", "--tags", "--match", "v[0-9]*", "--always", directory=checkout)
        version = f"{describe}-{build_number}"
        command_log(log, [go, "build", "-tags", (server / "build-tags.txt").read_text().strip(),
                          "-trimpath", "-ldflags",
                          f"-s -w -X main.version={version} -X main.channel=dev "
                          f"-X main.build={build_number} -X main.commit={target}",
                          "-o", record / "personad.next", "./cmd/personad"], server, features)
        web = checkout / "web"
        command_log(log, ["npm", "ci", "--no-audit", "--no-fund"], web)
        # Use the repository's Web development compile gate for this private hub.
        command_log(log, ["npm", "run", "build:development"], web)
        shutil.move(web / "dist", record / "web")
        require((record / "web/index.html").is_file(), "web build did not produce an index")
        require(output(record / "personad.next", "version") == version, "candidate binary version mismatch")
        return version
    finally:
        if checkout.exists():
            command_log(log, ["git", "worktree", "remove", "--force", checkout], REPO)


def swap_web(other):
    # Same-filesystem exchange keeps the live web path present throughout activation and rollback.
    rename = ctypes.CDLL(None, use_errno=True).renameat2
    rename.argtypes = [ctypes.c_int, ctypes.c_char_p, ctypes.c_int, ctypes.c_char_p, ctypes.c_uint]
    if rename(-100, os.fsencode(WEB), -100, os.fsencode(other), 2) != 0:
        raise OSError(ctypes.get_errno(), f"could not exchange web with {other}")


def install_binary(source):
    fd, name = tempfile.mkstemp(prefix=".personad-deploy-", dir=BINARY.parent)
    os.close(fd)
    temporary = Path(name)
    try:
        shutil.copy2(source, temporary)
        with temporary.open("rb") as stream:
            os.fsync(stream.fileno())
        os.replace(temporary, BINARY)
    finally:
        temporary.unlink(missing_ok=True)


def restart():
    subprocess.run(["systemctl", "--user", "restart", UNIT], check=True, timeout=105)


def success_health(expected, timeout=60):
    deadline = time.monotonic() + timeout
    while True:
        try:
            status = health()
            require(status == expected, f"expected {expected['version']}, got {status['version']}")
            return status, verify_web(WEB)
        except (OSError, ValueError, RuntimeError) as exc:
            if time.monotonic() >= deadline:
                raise RuntimeError(f"deployment verification failed: {exc}") from exc
            time.sleep(1)


def rollback(record, transaction):
    old_web = record / "web"
    if WEB.stat().st_ino != transaction["previous_web_inode"]:
        require(old_web.stat().st_ino == transaction["previous_web_inode"],
                "previous web directory could not be identified")
        swap_web(old_web)
    if digest(BINARY) != transaction["previous_binary_sha256"]:
        backup = record / "personad.prev"
        require(digest(backup) == transaction["previous_binary_sha256"], "previous binary checksum mismatch")
        install_binary(backup)
    previous = transaction["previous_health"]
    try:
        status, web = success_health(previous, timeout=0)
    except RuntimeError:
        restart()
        status, web = success_health(previous)
    require(web == transaction["previous_web"], "restored web differs from the previous release")
    return status, web


def prepare_rollback(record, previous, receipt):
    unit = output("systemctl", "--user", "show", UNIT, "--property=ExecStart", "--value")
    require(f"path={BINARY} ;" in unit and f"argv[]={BINARY} hub run ;" in unit,
            "hub unit no longer runs the expected binary")
    require(not BINARY.is_symlink() and not WEB.is_symlink()
            and WEB.stat().st_dev == record.stat().st_dev,
            "hub artifacts do not have the expected local layout")
    require(output(BINARY, "version") == previous["version"], "on-disk binary differs from live version")
    status = output(BINARY, "hub", "switch", "--status", env=dict(os.environ, PERSONA_HOME=str(HUB_HOME)))
    slot = re.search(r"\bpid (\d+) \(", status)
    require(slot is not None, "serving hub slot could not be identified")
    running = Path(f"/proc/{slot[1]}/exe")
    binary_sha = digest(BINARY)
    # A native switch can serve a staged copy while the service starts the installed copy.
    require(os.path.samestat(BINARY.stat(), running.stat()) or digest(running) == binary_sha,
            "serving hub slot differs from the on-disk binary")
    web = verify_web(WEB)
    backup = record / "personad.prev"
    shutil.copy2(BINARY, backup)
    require(digest(backup) == binary_sha, "previous binary backup checksum mismatch")
    transaction = {"previous_health": previous, "previous_binary_sha256": binary_sha,
                   "previous_web_inode": WEB.stat().st_ino, "previous_web": web, "receipt": receipt}
    atomic_json(record / "rollback.json", transaction)
    atomic_json(STATE / "deploy-pending.json", {"record": record.name})
    return transaction


def activate(record, previous, receipt, version):
    transaction = prepare_rollback(record, previous, receipt)
    try:
        install_binary(record / "personad.next")
        swap_web(record / "web")
        restart()
        expected = {"ok": True, "commit": receipt["to_commit"], "version": version}
        status, web = success_health(expected)
        receipt.update(result="deployed", health=status, web=web)
    except (OSError, RuntimeError, ValueError, subprocess.SubprocessError) as exc:
        receipt["reason"] = str(exc)
        recover(record, transaction, receipt)


def recover(record, transaction, receipt):
    try:
        status, web = rollback(record, transaction)
        receipt.update(result="rolled_back", health=status, web=web)
    except (OSError, RuntimeError, ValueError, subprocess.SubprocessError) as exc:
        receipt.update(result="rollback_failed", rollback_error=str(exc))


def deploy(receipt):
    pending = STATE / "deploy-pending.json"
    if pending.exists():
        name = json.loads(pending.read_text())["record"]
        require(re.fullmatch(r"fan-sync-[a-f0-9]{12}-[a-z0-9_]+", name), "invalid pending deployment record")
        record = DEPLOYMENTS / name
        transaction = json.loads((record / "rollback.json").read_text())
        completed = STATE / "deploy.json"
        if completed.exists():
            saved = json.loads(completed.read_text())
            if saved.get("result") in {"deployed", "rolled_back"} and saved.get("artifacts") == str(record):
                pending.unlink()
        if pending.exists():
            receipt.update({k: v for k, v in transaction["receipt"].items() if k != "started_at"},
                           reason="interrupted_activation")
            recover(record, transaction, receipt)
            return
    target = ready(receipt)
    if target is None:
        return
    previous = health()
    baseline = full_commit(previous["commit"])
    receipt.update(from_commit=baseline, from_version=previous["version"], health=previous)
    require_published(target)
    if baseline == target:
        receipt.update(result="skipped", reason="already_live")
        return
    require_rollback_compatible(baseline, target)
    record = Path(tempfile.mkdtemp(prefix=f"fan-sync-{target[:12]}-", dir=DEPLOYMENTS))
    receipt["artifacts"] = str(record)
    version = build(record, target)
    require_published(target)
    require(ready(receipt) == target, "published receipt changed during the build")
    require(health() == previous, "live hub changed during the build")
    activate(record, previous, receipt, version)


def interrupt(signum, _frame):
    raise RuntimeError(f"interrupted by signal {signum}")


def database_rollout():
    module = Path(__file__).with_name("database-rollout.py")
    return runpy.run_path(str(module))["Rollout"](globals())


def main():
    args = sys.argv[1:]
    if args not in ([], ["--database-check"], ["--database"]):
        print("usage: persona-fan-deploy [--database-check | --database]", file=sys.stderr)
        return 2
    os.umask(0o077)
    for sig in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
        signal.signal(sig, interrupt)
    receipt = {"started_at": now().isoformat(), "from_commit": None, "to_commit": None}
    if args == ["--database-check"]:
        try:
            database_rollout().preflight(receipt)
        except (OSError, RuntimeError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as exc:
            receipt.update(result="failed", reason=str(exc))
        print(json.dumps(receipt))
        return 1 if receipt["result"] == "failed" else 0
    rollout = None
    try:
        with contextlib.ExitStack() as stack:
            STATE.mkdir(mode=0o700, parents=True, exist_ok=True)
            lock(stack, STATE / "deploy.lock")
            phase = runpy.run_path(str(Path(__file__).with_name("phase-reservation.py")))
            try:
                phase["gate"](STATE, args, os.environ.get("PERSONA_FAN_DEPLOY_ATTEMPT"))
            except (ValueError, KeyError, TypeError) as failure:
                raise Busy(str(failure)) from failure
            lock(stack, DEPLOYMENTS / ".personad-deploy.lock")
            lock(stack, HUB_HOME / "releases/hub/.publish.lock")
            try:
                if args == ["--database"]:
                    require(not (STATE / "deploy-pending.json").exists(),
                            "artifact-only deployment journal requires recovery first")
                    rollout = database_rollout()
                    rollout.deploy(receipt)
                else:
                    require(not (STATE / "database-cutover.json").exists(),
                            "database cutover journal requires reconciliation before another deployment")
                    deploy(receipt)
            except (OSError, RuntimeError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as exc:
                receipt.update(result="failed", reason=str(exc))
            receipt["finished_at"] = now().isoformat()
            atomic_json(STATE / "deploy.json", receipt)
            if receipt["result"] in {"deployed", "rolled_back"}:
                if rollout is not None:
                    rollout.finish(receipt["result"])
                else:
                    (STATE / "deploy-pending.json").unlink(missing_ok=True)
            print(json.dumps(receipt))
            return 0 if receipt["result"] in {"deployed", "skipped"} else 1
    except Busy as exc:
        print(exc, file=sys.stderr)
        return 75
    except (OSError, RuntimeError) as exc:
        print(exc, file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
