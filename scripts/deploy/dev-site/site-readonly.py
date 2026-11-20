#!/usr/bin/env python3
"""Collect site observations without obtaining a release lock or changing its state."""
from collections import Counter
from datetime import datetime, timedelta, timezone
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import sqlite3
import subprocess
import sys
import time

HOME = Path.home()
RUNTIME = HOME / "projects/super-agent/.runtime/persona"
STATE = HOME / ".local/state/persona-fan-sync"
UTC8 = timezone(timedelta(hours=8))
ATTEMPT = r"[a-f0-9]{32}"


def at():
    return datetime.now(UTC8).isoformat(timespec="seconds")


def call(args, stdin=None, env=None, deadline=12):
    p = subprocess.run(args, input=stdin, env=env, timeout=deadline, text=True, capture_output=True)
    if p.returncode:
        raise RuntimeError(f"{Path(args[0]).name} exited {p.returncode}")
    return p.stdout.strip()


def https_origin():
    origin = os.environ.get("PERSONA_FAN_HUB_ORIGIN", "").rstrip("/")
    if not origin.startswith("https://"):
        raise RuntimeError("set PERSONA_FAN_HUB_ORIGIN to the site HTTPS URL")
    return origin


def health(port, tls):
    origin = https_origin() if tls else f"http://127.0.0.1:{port}"
    url = origin + "/api/v1/health"
    params = ["--cacert", RUNTIME / "home/tls/persona-ca.crt"] if tls else []
    data = json.loads(call(["curl", "-fsS", "--noproxy", "*", "--max-time", "5", *params, url]))
    return {name: data[name] for name in ("ok", "version", "commit")}


def rollback_runtime(edge, slot, linux):
    """Pin the running ELF generations and public Web before staging any candidate."""
    pids = {"edge": edge["MainPID"], "slot": int(slot), "node": linux["MainPID"]}
    def start_ticks(pid):
        return (Path("/proc") / str(pid) / "stat").read_text().rsplit(")", 1)[1].split()[19]
    def pin(pid):
        base = Path("/proc") / str(pid)
        with (base / "exe").open("rb") as elf:
            return hashlib.file_digest(elf, "sha256").hexdigest()
    before = {name: start_ticks(pid) for name, pid in pids.items()}
    digests = {name: pin(pid) for name, pid in pids.items()}
    index = subprocess.run(["curl", "-fsS", "--noproxy", "*", "--max-time", "5",
                            "--cacert", RUNTIME / "home/tls/persona-ca.crt",
                            https_origin() + "/"], check=True, capture_output=True,
                           timeout=6)
    digests["web"] = hashlib.sha256(index.stdout).hexdigest()
    if before != {name: start_ticks(pid) for name, pid in pids.items()}:
        raise RuntimeError("running generations changed during rollback pinning")
    return {"pids": pids, "start_ticks": before, "rollback_artifacts": digests}

def service(name):
    flags = ["MainPID", "ActiveState", "ControlGroup", "KillMode", "TimeoutStopUSec"]
    props = call(["systemctl", "--user", "show", name, *(f"--property={prop}" for prop in flags)])
    data = dict(line.split("=", 1) for line in props.splitlines())
    data["MainPID"] = int(data["MainPID"])
    ident = call(["busctl", "--user", "--json=short", "call", "org.freedesktop.systemd1",
                  "/org/freedesktop/systemd1", "org.freedesktop.systemd1.Manager", "GetUnit", "s", name])
    object_path = json.loads(ident)["data"][0]
    cond = call(["busctl", "--user", "--json=short", "get-property", "org.freedesktop.systemd1", object_path,
                 "org.freedesktop.systemd1.Unit", "Conditions"])
    data["condition_types"] = [item[0] for item in json.loads(cond)["data"]]
    return data


def descendants(cgroup, pid):
    for base in (Path("/sys/fs/cgroup"), Path("/sys/fs/cgroup/systemd")):
        root = base / cgroup.lstrip("/")
        if (root/"cgroup.procs").is_file():
            members = {int(item) for source in root.rglob("cgroup.procs") for item in source.read_text().split()}
            return len(members-{pid})
    return None


def tcp():
    sockets = {}
    rows = call(["ss", "-Hnet", "state", "established", "( sport = :18660 or sport = :18662 )"])
    for entry in rows.splitlines():
        columns = entry.split()
        if len(columns) < 4 or not columns[2].rsplit(":", 1)[-1] in ("18660", "18662"):
            continue
        match = re.search(r"\bino:([0-9]+)", entry)
        if match:
            inode = match.group(1)
            port = columns[2].rsplit(":",1)[-1]
            sockets[inode] = (port, int(columns[0]), int(columns[1]))
    return sockets


def db(name, query):
    command = ["sudo","-n","docker","exec","-i","persona-dev-postgres","sh","-c",
               'exec psql -XqAt -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$1"', "sh",name]
    return json.loads(call(command, Path(__file__).with_name(query).read_text()))


def lark():
    path = HOME / ".local/share/lark-context/lark-context.sqlite3"
    if not all(item.is_file() for item in (path, path.with_name(path.name+"-wal"), path.with_name(path.name+"-shm"))):
        raise RuntimeError("existing WAL and SHM files not available for read-only SQLite observation")
    candidates = call(["ps","-eo","pid=,comm="])
    owners = []
    database = path.stat()
    for entry in candidates.splitlines():
        fields = entry.split()
        if len(fields)!=2 or fields[1]!="lark-context":
            continue
        try:
            for fd in (Path("/proc")/fields[0]/"fd").iterdir():
                try:
                    observed=fd.stat()
                except FileNotFoundError:
                    continue
                if (observed.st_ino,observed.st_dev)==(database.st_ino,database.st_dev):
                    owners.append(int(fields[0]))
                    break
        except (FileNotFoundError,PermissionError):
            continue
    if len(owners)!=1:
        raise RuntimeError("could not establish the sole local service with this SQLite file open")
    with sqlite3.connect(path.resolve().as_uri()+"?mode=ro",uri=True,timeout=2) as conn:
        conn.execute("PRAGMA query_only=ON")
        conn.execute("BEGIN")
        queries = {
            "twin": "SELECT count(*) FROM twin_turns WHERE state IN ('pending','running','sending','sent_unrecorded')",
            "wakeup": "SELECT count(*) FROM persona_wakeups WHERE state IN ('ready','claiming')",
            "wakeup_unknown": "SELECT count(*) FROM persona_wakeups WHERE state='unknown'",
            "notice_unsettled": "SELECT count(*) FROM twin_findings WHERE notice_id IS NOT NULL AND notice_delivered_at IS NULL",
            "outbox_near": "SELECT count(*) FROM outbox WHERE status='sending' OR (status='pending' AND created_at>unixepoch('now','-1 day')*1000)",
            "outbox_historical": "SELECT count(*) FROM outbox WHERE status='pending' AND created_at<=unixepoch('now','-1 day')*1000",
            "outbox_unknown": "SELECT count(*) FROM outbox WHERE status='unknown'",
            "outbox_sent_unrecorded": """SELECT count(*) FROM outbox b WHERE b.purpose='persona_inbox_reply'
                AND b.status='sent' AND b.message_id IS NOT NULL
                AND NOT EXISTS (SELECT 1 FROM settings s WHERE s.key='persona.inbox_reply.recorded.'||b.id)""",
            "active_leases":"SELECT count(*) FROM twin_leases WHERE state='active' AND expires_at>unixepoch('now')*1000"
        }
        counts = {key:conn.execute(query).fetchone()[0] for key,query in queries.items()}
        return {"sampled_at":at(),"pid":owners[0],"query_only":conn.execute("PRAGMA query_only").fetchone()[0] == 1,
                "schema":conn.execute("PRAGMA user_version").fetchone()[0],"counts":counts}


def collect(attempt):
    outcome={"attempt":attempt,"started_at":at(),"scope":"observation","ready":False,"sources":{},"errors":{}}
    evidence=outcome["sources"]
    def sample(name, read):
        try:
            evidence[name]=read()
        except (RuntimeError,ValueError,OSError,KeyError,TypeError,sqlite3.Error,subprocess.SubprocessError) as exc:
            outcome["errors"][name]=str(exc)

    sample("primary_before", lambda:health(18662,True))
    sample("http_loopback",lambda:health(18660,False))
    sample("edge_slot",lambda:call([RUNTIME/"bin/personad","hub","switch","--status"],
                                  env=dict(os.environ,PERSONA_HOME=str(RUNTIME/"home"))))
    sample("hub_service",lambda:service("persona-dev-hub.service"))
    sample("linux_service",lambda:service("personad-node.service"))
    def runtime():
        active = re.search(r"slot [ab] serving [^,]+, build [^,]+, pid ([0-9]+) ", evidence["edge_slot"])
        if not active:
            raise RuntimeError("active Hub slot could not be identified")
        return rollback_runtime(evidence["hub_service"], active[1], evidence["linux_service"])
    sample("rollback_runtime", runtime)
    if node:=evidence.get("linux_service"):
        sample("linux_cgroup_other_processes",lambda:descendants(node["ControlGroup"],node["MainPID"]))
    def related():
        data={}
        for name in ("persona-link.service","persona-agy-runtime.service","persona-agy-usage.service"):
            data[name]=service(name)
        return data
    sample("other_persona_units", related)
    def local_socket():
        path = HOME/".persona/node-attach.sock"
        inode = path.stat().st_ino
        rows = call(["ss","-Hxnp"])
        return {"listener_inode":inode, "established_rows_with_path":sum(
            any(str(p) in line for p in (path,path.resolve())) and "ESTAB" in line for line in rows.splitlines()),
                "scope":"local socket presence only"}
    sample("linux_attach_socket",local_socket)
    initial={}
    try:
        initial=tcp()
        evidence["edge_tcp_first_at"]=at()
    except (RuntimeError,OSError,ValueError,subprocess.SubprocessError) as exc:
        outcome["errors"]["edge_tcp_first"]=str(exc)
    sample("http_client_operations",lambda:db("persona_dev","preflight-hub.sql"))
    sample("manager_ledgers",lambda:db("persona_dev_manager","preflight-manager.sql"))
    sample("postgres_activity",lambda:db("postgres","preflight-postgres.sql"))
    sample("lark_local",lark)
    time.sleep(1.5)
    try:
        last=tcp()
        stable=last.keys() & initial.keys()
        evidence["edge_tcp"]={"last_at":at(),"established":dict(Counter(x[0] for x in last.values())),
                             "same_inode_two_samples":dict(Counter(last[i][0] for i in stable)),
                             "last_nonempty_tcp_queues":sum(bool(item[1] or item[2]) for item in last.values()),
                             "protocol_classification":"unavailable"}
    except (RuntimeError,OSError,ValueError,subprocess.SubprocessError) as exc:
        outcome["errors"]["edge_tcp_last"]=str(exc)
    sample("primary_after",lambda:health(18662,True))
    sample("dev_json",lambda:{k:json.loads((STATE/"dev.json").read_text())[k] for k in ("sha","date")})
    outcome["end_at"]=at()
    outcome["blockers"]=["unverified_persistent_http_ws_gate","legacy_linux_sender_frontier",
                         "no_closed_attempt_lark_and_schedule_receipts","actual_writer_fence_and_restore_unproven"]
    return outcome


if __name__=="__main__":
    parser=argparse.ArgumentParser(description=__doc__)
    mode=parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--attempt")
    mode.add_argument("--adapter-inspect",action="store_true",help="accept the inspection request on stdin")
    args=parser.parse_args()
    attempt=args.attempt
    if args.adapter_inspect:
        request=json.load(sys.stdin)
        if request.get("operation")!="inspect" or request.get("action")!="inspect":
            parser.error("this driver only performs inspection")
        attempt=request.get("attempt")
    if not isinstance(attempt,str) or re.fullmatch(ATTEMPT,attempt) is None:
        parser.error("expected full 32-hex attempt")
    result=collect(attempt)
    if args.adapter_inspect:
        print(json.dumps(dict(result,operation="inspect",ingress_proven=False,rollback_proven=False,
                              writers_enumerated=False)))
        sys.exit(0)
    print(json.dumps(result,indent=2))
    raise SystemExit(1)
