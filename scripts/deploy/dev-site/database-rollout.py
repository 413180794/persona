"""Database-aware transaction for persona-fan-deploy.

The site controller owns admission, write draining, snapshots, routing and rollback.
This coordinator pins inputs, records intent and never rolls back after commit intent.
"""
import json
import os
from pathlib import Path
import re
import signal
import stat
import subprocess
import uuid

SHA = r"[0-9a-f]{64}"
ATTEMPT = r"[0-9a-f]{32}"
TOKEN = r"[A-Za-z0-9][A-Za-z0-9_.:-]{0,95}"
REQUIRED_DATABASES = {"persona_dev", "persona_dev_manager"}
BEFORE_OPEN = {"fencing", "staging", "verifying", "restoring", "restored"}
AFTER_OPEN = {"commit_intent", "committed"}


def expect(condition, reason):
    if not condition:
        raise RuntimeError(reason)


def private_file(path):
    s = path.lstat()
    expect(stat.S_ISREG(s.st_mode) and s.st_uid in {0, os.getuid()} and not s.st_mode & 0o022
           and path.is_absolute() and path.resolve() == path, f"untrusted release input: {path.name}")


def has_token(value):
    return isinstance(value, str) and re.fullmatch(TOKEN, value) is not None


def has_sha(value):
    return isinstance(value, str) and re.fullmatch(SHA, value) is not None


class Rollout:
    def __init__(self, core):
        self.core = core
        self.state = core["STATE"].resolve()
        self.journal = self.state / "database-cutover.json"
        self.profile = self.state / "database-production.json"

    def write(self, marker, phase):
        marker["phase"] = phase
        self.core["atomic_json"](self.journal, marker)

    def check_profile(self, previous, target, artifact_check=True):
        expect(self.profile.is_file(),
               "database rollout blocked: no verified site controller for production admission, clone routing and rollback")
        private_file(self.profile)
        config = json.loads(self.profile.read_text())
        expect(config.get("schema") == 1 and config.get("baseline") == previous and config.get("target") == target,
               "database profile does not match the exact live and target commits")
        controller = Path(config["controller"])
        private_file(controller)
        expect(os.access(controller, os.X_OK)
               and self.core["digest"](controller) == config["controller_sha256"],
               "site controller executable differs from the validated profile")
        databases = config["databases"]
        expect(isinstance(databases, list) and len(databases) == len(set(databases))
               and REQUIRED_DATABASES <= set(databases) and all(map(has_token, databases)),
               "database inventory must include the actual Hub and Manager databases")
        changes = self.core["git"]("diff", "--name-only", previous, target, "--", "server/internal/db/migrations")
        migrations = sorted(Path(path).name for path in changes.splitlines())
        expect(migrations and sorted(config["migrations"]) == migrations,
               "approved migration inventory differs from Git")
        manifest = Path(config["artifacts"])
        private_file(manifest)
        artifact = json.loads(manifest.read_text())
        expect(artifact.get("commit") == target and isinstance(artifact.get("version"), str)
               and has_sha(artifact.get("binary_sha256")) and has_sha(artifact.get("index_sha256")),
               "artifact receipt does not identify the target release")
        build_path = Path(artifact["artifacts"])
        expect(build_path.is_absolute() and not build_path.is_symlink(), "untrusted artifact directory")
        build = build_path.resolve(strict=True)
        if artifact_check:
            expect(self.core["digest"](build / "personad.next") == artifact["binary_sha256"]
                   and self.core["digest"](build / "web/index.html") == artifact["index_sha256"],
                   "built artifact hashes differ from the pinned receipt")
        plan = dict(baseline=previous, target=target, databases=databases, migrations=migrations,
                    controller=str(controller), controller_sha256=config["controller_sha256"],
                    version=artifact["version"], binary_sha256=artifact["binary_sha256"],
                    index_sha256=artifact["index_sha256"], artifacts=str(build))
        if "attempt" in config:
            attempt = config["attempt"]
            expect(isinstance(attempt, str) and re.fullmatch(ATTEMPT, attempt) is not None,
                   "site profile attempt must be a full lowercase 32-hex ID")
            plan["attempt"] = attempt
        return plan

    def start_marker(self, plan, receipt):
        attempt = plan.get("attempt")
        if attempt is None:
            return {"attempt": uuid.uuid4().hex}
        receipt["attempt"] = attempt
        used = self.state / f"database-attempt-{attempt}.json"
        expect(not os.path.lexists(used), "registered attempt was already started; use a fresh agreed attempt")
        return {"attempt": attempt, "reservation": used}

    def action(self, plan, marker, name):
        control = Path(plan["controller"])
        private_file(control)
        expect(self.core["digest"](control) == plan["controller_sha256"], "site controller changed during rollout")
        payload = json.dumps({
            "action": name, "attempt": marker["attempt"], "fence": marker.get("fence"),
            "baseline": plan["baseline"], "target": plan["target"], "databases": plan["databases"],
            "migrations": plan["migrations"], "artifacts": plan["artifacts"],
            "binary_sha256": plan["binary_sha256"], "index_sha256": plan["index_sha256"],
        })
        with subprocess.Popen([control, name], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                              stderr=subprocess.DEVNULL, text=True, start_new_session=True) as child:
            try:
                output, _ = child.communicate(payload, timeout=600 if name == "stage" else 120)
            except BaseException:
                # Wait for the controller process group before invoking an opposite action.
                try:
                    os.killpg(child.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                child.wait(timeout=10)
                raise
        if child.returncode != 0:
            raise RuntimeError(f"site controller {name} failed with exit {child.returncode}")
        expect(len(output) <= 65536, f"oversized site controller receipt for {name}")
        result = json.loads(output)
        expect(result.get("action") == name and result.get("attempt") == marker["attempt"],
               f"mismatched site controller receipt for {name}")
        return result

    @staticmethod
    def staged(plan, result):
        pairs = result["databases"]
        expect(isinstance(pairs, dict) and set(pairs) == set(plan["databases"]),
               "snapshot receipt did not cover every database")
        identities = set()
        for pair in pairs.values():
            names = (pair.get("original"), pair.get("candidate"))
            expect(all(map(has_token, names)) and names[0] != names[1] and not identities.intersection(names)
                   and pair.get("verified") is True and has_sha(pair.get("dump_sha256")),
                   "snapshot receipt lacks a distinct, verified original and candidate")
            identities.update(names)
        expect(result.get("originals_untouched") is True and has_sha(result.get("runtime_sha256")),
               "runtime snapshot or preservation of the original data is unverified")
        return {name: pairs[name]["dump_sha256"] for name in plan["databases"]}

    def inspect(self, plan, marker):
        result = self.action(plan, marker, "inspect")
        expect(result.get("ready") is True and result.get("ingress_proven") is True
               and result.get("rollback_proven") is True and result.get("writers_enumerated") is True,
               "site controller could not prove production admission, writer coverage and rollback")

    def current(self, receipt):
        target = self.core["ready"](receipt)
        if target is None:
            return None, None
        self.core["require_published"](target)
        previous = self.core["health"]()
        baseline = self.core["full_commit"](previous["commit"])
        receipt.update(from_commit=baseline, from_version=previous["version"], health=previous)
        return previous, target

    def preflight(self, receipt):
        expect(not self.journal.exists(), "database cutover journal requires reconciliation")
        expect(not (self.state / "deploy-pending.json").exists(),
               "artifact-only deployment journal requires recovery first")
        previous, target = self.current(receipt)
        expect(target is not None, "no published target for database rollout")
        if receipt["from_commit"] == target:
            receipt.update(result="skipped", reason="already_live")
            return
        plan = self.check_profile(receipt["from_commit"], target)
        self.inspect(plan, self.start_marker(plan, receipt))
        receipt.update(result="ready_for_fenced_deploy", migrations=plan["migrations"])

    def verify_live(self, phase, marker):
        status = self.core["health"]()
        expected = marker["plan"]["target"] if phase == "candidate" else marker["plan"]["baseline"]
        version = marker["plan"]["version"] if phase == "candidate" else marker["previous_version"]
        expect(self.core["full_commit"](status["commit"]) == expected and status["version"] == version,
               f"public health does not match the {phase} release")
        web = self.core["verify_web"](self.core["WEB"])
        sha = marker["plan"]["index_sha256"] if phase == "candidate" else marker["previous_web"]
        expect(web["index_sha256"] == sha, f"public web does not match the {phase} release")
        return status, web

    def restore(self, plan, marker, receipt):
        self.write(marker, "restoring")
        try:
            result = self.action(plan, marker, "restore")
            expect(result.get("original_route") is True and result.get("originals_untouched") is True
                   and type(result.get("candidate_writes_admitted")) is int and result["candidate_writes_admitted"] == 0
                   and result.get("original_health_commit") == plan["baseline"]
                   and result.get("original_web_sha256") == marker["previous_web"],
                   "old route, data preservation or pre-admission condition could not be proven")
            status, web = self.verify_live("original", marker)
        except (OSError, RuntimeError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as failure:
            receipt.update(result="rollback_failed", rollback_error=str(failure))
            return
        self.write(marker, "restored")
        receipt.update(result="rolled_back", health=status, web=web)

    def diagnose_journal(self, receipt):
        marker = json.loads(self.journal.read_text())
        plan = marker["plan"]
        receipt.update(from_commit=plan["baseline"], to_commit=plan["target"], attempt=marker["attempt"],
                       artifacts=plan["artifacts"])
        if "database_snapshots" in marker:
            receipt["database_snapshots"] = marker["database_snapshots"]
        if marker["phase"] in AFTER_OPEN:
            if marker["phase"] == "committed":
                status, web = self.verify_live("candidate", marker)
                receipt.update(result="deployed", health=status, web=web)
                return
            try:
                configured = self.check_profile(plan["baseline"], plan["target"], artifact_check=False)
                expect(configured == plan, "site profile changed since commit intent")
                observed = self.action(plan, marker, "reconcile")
                expect(observed.get("candidate_authoritative") is True
                       and observed.get("originals_retained") is True
                       and observed.get("fence") == marker.get("fence")
                       and observed.get("candidate_commit") == plan["target"],
                       "site controller could not prove that candidate admission committed")
                status, web = self.verify_live("candidate", marker)
            except (OSError, RuntimeError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as failure:
                receipt.update(result="needs_reconcile", reason=str(failure))
                return
            self.write(marker, "committed")
            receipt.update(result="deployed", health=status, web=web, reconciled=True)
            return
        expect(marker["phase"] in BEFORE_OPEN, "unrecognized cutover phase; reconcile before continuing")
        configured = self.check_profile(plan["baseline"], plan["target"], artifact_check=False)
        expect(configured == plan, "database profile changed since cutover; reconcile with the original controller")
        receipt["reason"] = "interrupted_before_admission"
        self.restore(plan, marker, receipt)

    def deploy(self, receipt):
        if self.journal.exists():
            self.diagnose_journal(receipt)
            return
        previous, target = self.current(receipt)
        if target is None:
            return
        baseline = receipt["from_commit"]
        if baseline == target:
            receipt.update(result="skipped", reason="already_live")
            return
        plan = self.check_profile(baseline, target)
        marker = self.start_marker(plan, receipt)
        reservation = marker.pop("reservation", None)
        marker.update(plan=plan, previous_version=previous["version"],
                      previous_web=self.core["verify_web"](self.core["WEB"])["index_sha256"])
        self.inspect(plan, marker)
        expect(self.core["health"]() == previous, "live release changed after preflight")
        self.core["require_published"](target)
        receipt.update(attempt=marker["attempt"], artifacts=plan["artifacts"])
        if reservation is not None:
            # The caller holds the publication locks. Record one-shot use before any fencing.
            # A crash before the next journal write leaves the live route unchanged.
            self.core["atomic_json"](reservation, dict(attempt=marker["attempt"], baseline=baseline, target=target,
                                                      started_at=self.core["now"]().isoformat()))
        self.write(marker, "fencing")
        try:
            fenced = self.action(plan, marker, "fence")
            expect(fenced.get("admission_closed") is True and fenced.get("all_writers_drained") is True
                   and type(fenced.get("unaccounted_writes")) is int and fenced["unaccounted_writes"] == 0
                   and has_token(fenced.get("fence")), "writer drain and admission fence were not proven")
            marker["fence"] = fenced["fence"]
            self.write(marker, "staging")
            staged = self.action(plan, marker, "stage")
            expect(staged.get("admission_closed") is True and staged.get("fence") == marker["fence"],
                   "snapshot stage no longer proves the same admission fence")
            receipt["database_snapshots"] = self.staged(plan, staged)
            marker["database_snapshots"] = receipt["database_snapshots"]
            self.write(marker, "verifying")
            candidate = self.action(plan, marker, "verify")
            expect(candidate.get("admission_closed") is True and candidate.get("fence") == marker["fence"]
                   and candidate.get("commit") == target and candidate.get("version") == plan["version"]
                   and candidate.get("web_sha256") == plan["index_sha256"]
                   and candidate.get("manager_ready") is True and candidate.get("clone_only") is True
                   and sorted(candidate.get("migrations", [])) == plan["migrations"],
                   "isolated candidate failed version, Web, Manager, routing or migration verification")
            self.core["require_published"](target)
        except (OSError, RuntimeError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as failure:
            receipt["reason"] = str(failure)
            self.restore(plan, marker, receipt)
            return

        # From this durable boundary the candidate might accept writes even without a response.
        self.write(marker, "commit_intent")
        try:
            admitted = self.action(plan, marker, "open")
            expect(admitted.get("candidate_authoritative") is True and admitted.get("originals_retained") is True
                   and admitted.get("fence") == marker["fence"], "candidate admission is indeterminate")
            status, web = self.verify_live("candidate", marker)
        except (OSError, RuntimeError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as failure:
            receipt.update(result="needs_reconcile", reason=str(failure))
            return
        self.write(marker, "committed")
        receipt.update(result="deployed", health=status, web=web)

    def finish(self, result):
        if result in {"deployed", "rolled_back"} and self.journal.exists():
            marker = json.loads(self.journal.read_text())
            expect(marker["phase"] == {"deployed": "committed", "rolled_back": "restored"}[result],
                   "cutover journal does not match the completed receipt")
            self.journal.unlink()
