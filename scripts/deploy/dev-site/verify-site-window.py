#!/usr/bin/env python3
"""Validate content-addressed observations made after a confirmed site closure."""
import argparse
import hashlib
import json
from datetime import datetime, timezone
from pathlib import Path
import re
import runpy
import sys

base=runpy.run_path(str(Path(__file__).with_name("site-writer-fence.py")))
read_private, require=base["read_private"],base["require"]
PATHS={
    "http": ("loopback_rejected_write", "lan_rejected_write", "closed_after_restart"),
    "websocket": ("existing_connection_rejected_write", "existing_connections_enumerated"),
    "lark_native": ("new_callback_admission_closed", "original_message_terminals_verified"),
    "lark_extension": ("polling_and_callbacks_paused", "sent_ledger_settled", "historical_items_untouched"),
    "schedule": ("new_occurrence_admission_closed", "original_occurrence_terminals_verified"),
    "background": ("writer_inventory_confirmed", "outbox_and_leases_settled"),
    "linux": ("sender_original_ledger_complete", "same_connection_commit_readback"),
}


def instant(value):
    t=datetime.fromisoformat(value)
    require(t.tzinfo is not None,"timestamps must carry an offset")
    return t


def validate_paths(plan, bundle, directory, paths, now=None):
    now = now or datetime.now(timezone.utc)
    require(re.fullmatch(r"[a-f0-9]{32}",plan["attempt"]) is not None,"invalid pinned attempt")
    fixed={k:plan[k] for k in ("attempt","baseline","source_cluster","old_slot_pid","old_linux_pid")}
    require(all(bundle.get(k)==value for k,value in fixed.items()),"bundle has another attempt, process or route")
    closed_at=instant(bundle["closed_at"])
    opened_at=instant(plan["window_from"])
    ended_at=instant(plan["window_until"])
    require(opened_at<=closed_at<=now<ended_at,"closure is outside the active approved window")
    require(set(bundle["paths"])==set(paths),"release lacks a complete writer inventory")
    observations={}
    for name, checks in paths.items():
        sha=bundle["paths"][name]
        require(isinstance(sha,str) and re.fullmatch(r"[0-9a-f]{64}",sha) is not None,f"{name}: evidence digest missing")
        raw=read_private(directory/f"{sha}.json")
        require(hashlib.sha256(raw).hexdigest()==sha,f"{name}: evidence digest changed")
        result=json.loads(raw)
        require(all(result.get(k)==value for k,value in fixed.items()) and result.get("path")==name
                and result.get("closed_at")==bundle["closed_at"],f"{name}: evidence has another boundary")
        require(closed_at<=instant(result["observed_at"])<=now,f"{name}: observation predates closure or is in the future")
        require(result.get("kind")=="actual_closed_path",f"{name}: a sample is not a closure receipt")
        require(all(result.get(key) is True for key in checks),f"{name}: required closed-path observations missing")
        require(type(result.get("unverified_accepted")) is int and result["unverified_accepted"]==0,
                f"{name}: accepted effects not fully settled")
        observations[name]=sha
    return {"attempt":fixed["attempt"],"closed_at":bundle["closed_at"],"path_receipts":observations}


def validate(plan, bundle, directory, now=None):
    return dict(validate_paths(plan,bundle,directory,PATHS,now),strict_paths_validated=True)


if __name__=="__main__":
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument("plan",type=Path)
    parser.add_argument("bundle",type=Path)
    parser.add_argument("evidence",type=Path)
    args=parser.parse_args()
    try:
        print(json.dumps(validate(json.loads(read_private(args.plan)),
                                  json.loads(read_private(args.bundle)),args.evidence)))
    except (KeyError,OSError,TypeError,ValueError) as exc:
        print(str(exc),file=sys.stderr)
        sys.exit(1)
