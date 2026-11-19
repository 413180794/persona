#!/usr/bin/env python3
"""Database rollout adapter for a pinned ingress and runtime driver."""
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import runpy
import stat
import subprocess
import sys

base = runpy.run_path(str(Path(__file__).with_name("site-writer-fence.py")))
require, read_private, save = (base[name] for name in ("require", "read_private", "save"))
SHA = r"[0-9a-f]{64}"
ATTEMPT = r"[0-9a-f]{32}"
ACTIONS = ("inspect", "fence", "stage", "verify", "open", "restore", "reconcile")


def exact(result, **fields):
    for key, value in fields.items():
        require(result.get(key) == value and type(result.get(key)) is type(value),
                f"site observation differs for {key}")


def identity(value, labels):
    return (isinstance(value, dict) and isinstance(value.get("cluster"), str)
            and re.fullmatch(r"[0-9]{1,20}", value["cluster"]) is not None
            and isinstance(value.get("databases"), dict) and set(value["databases"]) == set(labels)
            and len(set(value["databases"].values())) == len(labels)
            and all(isinstance(oid, str) and re.fullmatch(r"[0-9]{1,10}", oid) for oid in value["databases"].values()))


class Adapter:
    def __init__(self, config, request):
        self.root = config.parent
        raw = read_private(config)
        self.spec = json.loads(raw)
        self.request = request
        self.attempt = request["attempt"]
        require(self.spec.get("enabled") is True, "site adapter is disabled")
        require(re.fullmatch(ATTEMPT, self.attempt) is not None, "invalid release attempt")
        fixed = ("attempt", "baseline", "target", "databases", "artifacts", "binary_sha256", "index_sha256", "migrations")
        exact(request, **{name: self.spec[name] for name in fixed})
        self.original = self.spec["original_database"]
        self.target = self.spec["target_database"]
        require(identity(self.original, request["databases"]) and identity(self.target, request["databases"])
                and self.original["cluster"] != self.target["cluster"], "candidate needs a separate PostgreSQL instance")
        self.driver = Path(self.spec["driver"])
        self.state = self.root / "adapter-state.json"
        self.profile_sha = hashlib.sha256(raw).hexdigest()

    def execute(self, name, **context):
        # Keep authorization and credentials in the dedicated driver, never in coordinator receipts.
        st = self.driver.lstat()
        require(self.driver.is_absolute() and stat.S_ISREG(st.st_mode) and st.st_uid == os.getuid()
                and st.st_mode & 0o022 == 0 and os.access(self.driver, os.X_OK)
                and hashlib.sha256(self.driver.read_bytes()).hexdigest() == self.spec["driver_sha256"],
                "site driver changed or is not trusted")
        payload = dict(self.request, operation=name, context=context)
        # The rollout entry owns the timeout and kills this process group before
        # attempting recovery. A driver must keep mutating helpers in that group.
        result = subprocess.run([self.driver, name], input=json.dumps(payload), text=True,
                                stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
        require(result.returncode == 0 and len(result.stdout) <= 65536, f"site driver {name} failed")
        value = json.loads(result.stdout)
        exact(value, operation=name, attempt=self.attempt)
        return value

    def read(self):
        if not self.state.exists():
            return {"attempt": self.attempt, "profile": self.profile_sha, "phase": "idle"}
        record = json.loads(read_private(self.state))
        exact(record, attempt=self.attempt, profile=self.profile_sha)
        return record

    def write(self, record, phase, **fields):
        record.update(phase=phase, **fields)
        save(self.state, record)

    def run(self, action):
        record = self.read()
        phase = record["phase"]
        fence = self.attempt
        result = {"action": action, "attempt": self.attempt}
        if action == "inspect":
            require(phase == "idle", "site already has a release attempt")
            live = self.execute("inspect")
            exact(live, ready=True, ingress_proven=True, rollback_proven=True, writers_enumerated=True,
                  original_database=self.original, target_database=self.target)
            return dict(result, ready=True, ingress_proven=True, rollback_proven=True, writers_enumerated=True)
        if action == "fence":
            require(phase == "idle", "fence needs an unused attempt")
            self.write(record, "fencing")
            observed = self.execute("fence")
            exact(observed, ingress_closed=True, legacy_sender_verified=True, all_writers_drained=True,
                  unaccounted_writes=0, original_database=self.original, originals_sealed=True)
            self.write(record, "fenced")
            return dict(result, admission_closed=True, all_writers_drained=True, unaccounted_writes=0, fence=fence)
        if action == "restore":
            require(phase in ("idle", "fencing", "fenced", "staging", "staged", "verifying", "verified",
                              "restoring", "restored"), "candidate may have accepted production writes")
            require(self.request.get("fence") in (None, fence), "wrong rollback fence")
            self.write(record, "restoring")
            restored = self.execute("restore", prior_phase=phase)
            exact(restored, candidate_stopped=True, candidate_writes_admitted=0, original_database=self.original,
                  original_health_commit=self.request["baseline"], original_route=True, originals_untouched=True,
                  original_linux_ready=True)
            require(re.fullmatch(SHA, restored["original_web_sha256"]) is not None, "original Web checks missing")
            self.write(record, "restored")
            return dict(result, original_route=True, originals_untouched=True, candidate_writes_admitted=0,
                        original_health_commit=restored["original_health_commit"],
                        original_web_sha256=restored["original_web_sha256"])
        require(self.request.get("fence") == fence, "wrong admission fence")
        if action == "stage":
            require(phase == "fenced", "site was not fenced")
            self.write(record, "staging")
            staged = self.execute("stage")
            exact(staged, ingress_closed=True, originals_sealed=True, originals_untouched=True,
                  original_database=self.original, target_database=self.target)
            pairs = staged["databases"]
            require(set(pairs) == set(self.request["databases"])
                    and all(pair.get("verified") is True and re.fullmatch(SHA, pair.get("dump_sha256", ""))
                            for pair in pairs.values())
                    and re.fullmatch(SHA, staged["runtime_sha256"]), "archive receipts missing")
            self.write(record, "staged")
            return dict(result, admission_closed=True, fence=fence, databases=pairs, originals_untouched=True,
                        runtime_sha256=staged["runtime_sha256"])
        if action == "verify":
            require(phase == "staged", "target archives are not ready")
            self.write(record, "verifying")
            candidate = self.execute("verify")
            exact(candidate, ingress_closed=True, originals_sealed=True, clone_only=True, manager_ready=True,
                  target_database=self.target, commit=self.request["target"], web_sha256=self.request["index_sha256"],
                  migrations=self.request["migrations"], linux_commit=self.request["target"])
            require(re.fullmatch(SHA, candidate["linux_checkpoint_sha256"]) is not None
                    and type(candidate.get("linux_checkpoint_sessions")) is int
                    and candidate["linux_checkpoint_sessions"] >= 0, "Linux did not read back candidate state")
            self.write(record, "verified", linux_checkpoint_sha256=candidate["linux_checkpoint_sha256"],
                       linux_checkpoint_sessions=candidate["linux_checkpoint_sessions"])
            return dict(result, admission_closed=True, fence=fence, commit=candidate["commit"],
                        version=candidate["version"], web_sha256=candidate["web_sha256"], manager_ready=True,
                        clone_only=True, migrations=candidate["migrations"])
        if action == "open":
            require(phase == "verified", "candidate startup was not verified")
            # After this record, do not call open or restore again, even if the driver dies after applying it.
            self.write(record, "open_intent")
            observed = self.execute("open")
        elif action == "reconcile":
            require(phase in ("open_intent", "opened"), "no candidate admission to reconcile")
            observed = self.execute("reconcile")
        else:
            raise ValueError("unknown site action")
        exact(observed, candidate_authoritative=True, originals_retained=True, original_database=self.original,
              target_database=self.target, candidate_commit=self.request["target"])
        self.write(record, "opened")
        return dict(result, candidate_authoritative=True, originals_retained=True, fence=fence,
                    candidate_commit=observed["candidate_commit"])


def main():
    action, config = sys.argv[1:]
    require(action in ACTIONS, "unknown site action")
    request = json.load(sys.stdin)
    exact(request, action=action)
    adapter = Adapter(Path(config), request)
    if action == "inspect":
        result = adapter.run(action)
    else:
        descriptor = os.open(adapter.root / "adapter.lock", os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
        with os.fdopen(descriptor, "r+") as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            result = adapter.run(action)
    print(json.dumps(result))


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, RuntimeError, KeyError, TypeError, subprocess.SubprocessError) as exc:
        print(f"site adapter: {exc}", file=sys.stderr)
        sys.exit(1)
