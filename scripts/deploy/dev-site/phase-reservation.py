#!/usr/bin/env python3
"""Keep the Hub and Lark release under one durable host reservation."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import runpy

fence = runpy.run_path(str(Path(__file__).with_name("site-writer-fence.py")))
require, read_private, save, sync_dir = (fence[k] for k in ("require", "read_private", "save", "sync_dir"))
FILE = "deploy-phase.json"
PHASES = {
    "reserve": ("same_window_approved", "v3_confirmed", "decision_card_confirmed"),
    "handoff": ("hub_deployed", "hub_linux_healthy", "same_pid_skip_verified", "v3_owns_next_write"),
    "finish": ("lark_pair_verified", "single_writer_restored"),
    "cancel": ("no_release_in_progress", "old_or_authoritative_route_verified", "v3_released"),
}


def gate(state, args, attempt):
    active = state / FILE
    if not active.exists():
        return
    record = json.loads(read_private(active))
    profile = state / "database-production.json"
    require(args == ["--database"] and record["phase"] == "hub" and record["attempt"] == attempt
            and profile.is_file() and json.loads(read_private(profile)).get("attempt") == attempt,
            "another phase owns the host publication reservation")


def transition(state, action, attempt, path):
    require(re.fullmatch(r"[a-f0-9]{32}", attempt) is not None, "phase must carry the full attempt ID")
    proof = json.loads(read_private(path))
    require(proof.get("attempt") == attempt and all(proof.get(key) is True for key in PHASES[action]),
            "missing same-attempt phase evidence")
    active = state / FILE
    record = json.loads(read_private(active)) if active.exists() else None
    closed = state / f"deploy-phase-{attempt}.closed.json"
    require(not (state / "database-cutover.json").exists(), "database release needs reconciliation")
    if action == "reserve":
        require(record is None and not closed.exists(), "host already reserved or attempt already closed")
        record = {"attempt": attempt, "phase": "hub", "window_evidence": str(path)}
        save(active, record)
        return record
    require(record is not None and record["attempt"] == attempt, "another attempt owns this window")
    if action == "handoff":
        require(record["phase"] == "hub", "Hub already gave up this window")
        profile = json.loads(read_private(state / "database-production.json"))
        require(profile.get("attempt") == attempt, "site profile changed")
        target = profile["target"]
        deployed = pinned_receipt(proof, "deploy_receipt")
        skipped = pinned_receipt(proof, "skip_receipt")
        require(deployed.get("attempt") == attempt and deployed.get("result") == "deployed"
                and deployed.get("to_commit") == target and skipped.get("result") == "skipped"
                and skipped.get("reason") == "already_live"
                and skipped.get("from_commit") == skipped.get("to_commit") == target,
                "Hub deployment and skip receipts do not prove this target")
        record.update(phase="lark", hub_evidence=str(path))
        save(active, record)
        return record
    require(action == "cancel" or record["phase"] == "lark", "Lark has yet to receive this window")
    record.update(closed_as=action, close_evidence=str(path))
    save(closed, record)
    active.unlink()
    sync_dir(state)
    return record


def pinned_receipt(proof, key):
    item = proof[key]
    raw = read_private(Path(item["path"]))
    require(hashlib.sha256(raw).hexdigest() == item["sha256"], "phase receipt fingerprint differs")
    return json.loads(raw)


def main():
    import contextlib
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("state", type=Path)
    parser.add_argument("action", choices=tuple(PHASES))
    parser.add_argument("--attempt", required=True)
    parser.add_argument("--proof", required=True, type=Path)
    args = parser.parse_args()
    # The deploy entry holds this same lock while checking admission to the phase.
    core = runpy.run_path(str(Path(__file__).with_name("persona-fan-deploy.py")))
    try:
        with contextlib.ExitStack() as stack:
            core["lock"](stack, args.state / "deploy.lock")
            print(json.dumps(transition(args.state, args.action, args.attempt, args.proof)))
    except core["Busy"] as failure:
        parser.exit(75, str(failure) + "\n")
    except (OSError, ValueError, RuntimeError, KeyError, TypeError) as failure:
        parser.exit(1, str(failure) + "\n")


if __name__ == "__main__":
    main()
