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
    "websocket": ("existing_connections_enumerated",),
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


def validate_websocket(result, cut, observed, linux_pid):
    if result.get("existing_connection_rejected_write") is True:
        return
    require(result.get("fresh_app_admissions_closed") is True and
            type(result.get("remaining_old_sessions")) is int and result["remaining_old_sessions"] == 0 and
            type(result.get("unknown_connection_count")) is int and result["unknown_connection_count"] == 0,
            "websocket: app ingress or old sessions remain open")
    require(result.get("old_linux_before") == result.get("old_linux_after") and
            isinstance(result.get("old_linux_after"), dict) and
            result["old_linux_after"].get("pid") == linux_pid and
            all(type(result["old_linux_after"].get(key)) is int and result["old_linux_after"][key] > 0
                for key in ("attach_inode","connection_inode","start_ticks")),
            "websocket: old Linux connection generation changed")
    sessions = result.get("controlled_clients")
    require(isinstance(sessions, list) and type(result.get("old_app_sessions")) is int and
            len(sessions) == result["old_app_sessions"],
            "websocket: controlled client inventory is incomplete")
    seen = set()
    for session in sessions:
        fingerprint = session.get("transport_sha256")
        require(isinstance(fingerprint, str) and re.fullmatch(r"[a-f0-9]{64}", fingerprint) and fingerprint not in seen,
                "websocket: client transport identity missing or repeated")
        seen.add(fingerprint)
        freeze, stopped = instant(session["sender_paused_at"]), instant(session["closed_at"])
        require(cut <= freeze <= stopped <= observed and
                session.get("all_accepted_terminals_verified") is True and
                type(session.get("unverified_operations")) is int and session["unverified_operations"] == 0,
                "websocket: old client lacks a same-cut accepted-operation receipt")


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
        observed_at=instant(result["observed_at"])
        require(closed_at<=observed_at<=now,f"{name}: observation predates closure or is in the future")
        require(result.get("kind")=="actual_closed_path",f"{name}: a sample is not a closure receipt")
        require(all(result.get(key) is True for key in checks),f"{name}: required closed-path observations missing")
        require(type(result.get("unverified_accepted")) is int and result["unverified_accepted"]==0,
                f"{name}: accepted effects not fully settled")
        if name=="websocket":
            validate_websocket(result,closed_at,observed_at,fixed["old_linux_pid"])
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
