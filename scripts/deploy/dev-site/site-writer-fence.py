#!/usr/bin/env python3
"""Durable Linux systemd/PostgreSQL writer fence, after separate ingress and drain proof."""
import argparse
from contextlib import contextmanager
from hashlib import sha256
import fcntl
import json
import os
from pathlib import Path
import re
import secrets
import stat
import subprocess
import sys
import tempfile

DEV_UNITS = ("persona-dev-hub.service", "personad-node.service")
DEV_DBS = ("persona_dev", "persona_dev_manager")
IDENT = r"[a-z][a-z0-9_]{0,62}"
FIXTURE_UNIT = r"persona-fence-fixture-[a-f0-9]{10}-(hub|node)\.service"
DB_LOCK = 1972418315


def require(condition, message):
    if not condition:
        raise ValueError(message)


def read_private(path):
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_uid == os.getuid() and not info.st_mode & 0o077,
            f"not an owner-only regular receipt: {path.name}")
    return path.read_bytes()


def save(path, value):
    with tempfile.NamedTemporaryFile("w", dir=path.parent, delete=False) as stream:
        temporary = Path(stream.name)
        try:
            json.dump(value, stream)
            stream.write("\n")
            stream.flush()
            os.fsync(stream.fileno())
            temporary.replace(path)
        finally:
            temporary.unlink(missing_ok=True)
    sync_dir(path.parent)


def sync_dir(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def run(command, statement=None, seconds=30):
    result = subprocess.run(command, input=statement, text=True, capture_output=True, timeout=seconds)
    if result.returncode:
        raise RuntimeError(f"{command[0]} operation failed with exit {result.returncode}")
    return result.stdout.strip()


class Fence:
    def __init__(self, root):
        self.root = root.resolve()
        info = self.root.stat()
        require(stat.S_IMODE(info.st_mode) == 0o700 and info.st_uid == os.getuid(), "site directory must be owned and 0700")
        raw = read_private(self.root / "site.json")
        self.profile = sha256(raw).hexdigest()
        site = json.loads(raw)
        self.units, self.databases, self.container = tuple(site["units"]), tuple(site["databases"]), site["container"]
        require(len(self.units) == 2 and len(set(self.units)) == 2, "expected a distinct Linux node and Hub pair")
        require(len(self.databases) == 2 and len(set(self.databases)) == 2 and
                all(re.fullmatch(IDENT, name) for name in self.databases), "expected two valid database names")
        self.fixture = self.container != "persona-dev-postgres"
        self.expected_original_identity = site.get("original_identity")
        if not self.fixture:
            require(self.units == DEV_UNITS and self.databases == DEV_DBS, "site scope must be the exact development pair")
            require(site.get("production_site_verified") is True, "production site not verified")
            require(isinstance(self.expected_original_identity, dict), "production database identity must be pinned")
        else:
            require(re.fullmatch(r"persona-0551-fixture-[a-f0-9]{10}", self.container)
                    and all(re.fullmatch(FIXTURE_UNIT, unit) for unit in self.units)
                    and self.root.parent == Path("/tmp") and self.root.name.startswith("persona-site-fence-"),
                    "invalid fixture scope")
            require(run(["sudo", "-n", "docker", "inspect", "--format", "{{.HostConfig.NetworkMode}}", self.container]) == "none",
                    "database fixture must have no network")
            mount_format = '{{range .Mounts}}{{if eq .Destination "/var/run/postgresql"}}{{.Source}}{{end}}{{end}}'
            pgroot = run(["sudo", "-n", "docker", "inspect", "--format", mount_format, self.container])
            require(pgroot == str(self.root / "pgsock"), "fixture database socket must belong to the private site directory")
        self.block = self.root / "restart-block.json"
        self.journal = self.root / "handover.json"
        require(re.fullmatch(r"[a-zA-Z0-9_./-]+", str(self.block)), "unsupported path in systemd condition")

    @contextmanager
    def operation(self):
        fd = os.open(self.root / "lock", os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, "r+") as stream:
            require(stat.S_ISREG(os.fstat(stream.fileno()).st_mode), "operation lock is not a file")
            fcntl.flock(stream, fcntl.LOCK_EX | fcntl.LOCK_NB)
            yield

    def sql(self, text, seconds=15):
        command = ["sudo", "-n", "docker", "exec", "-i"]
        if self.fixture:
            command.extend(["-e", "PGHOST=/var/run/postgresql", "-e", "PGPORT=55455"])
        command.extend([self.container, "sh", "-c",
                        'exec psql -XqAt -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d postgres'])
        return run(command, text, seconds=seconds)

    def info(self):
        placeholders = ",".join(f"'{db}'" for db in self.databases)
        data = json.loads(self.sql(f"""
BEGIN READ ONLY;
SELECT jsonb_build_object(
 'connections_allowed', (SELECT jsonb_object_agg(datname,datallowconn) FROM pg_database WHERE datname IN ({placeholders})),
 'identity', jsonb_build_object('cluster', (SELECT system_identifier::text FROM pg_control_system()),
     'databases', (SELECT jsonb_object_agg(datname,oid::text) FROM pg_database WHERE datname IN ({placeholders}))),
 'clients', (SELECT count(*) FROM pg_stat_activity WHERE backend_type='client backend' AND datname IN ({placeholders})));
COMMIT;"""))
        require(set(data["connections_allowed"] or {}) == set(self.databases), "could not identify both databases")
        require(self.expected_original_identity is None or data["identity"] == self.expected_original_identity,
                "database identity differs from the site profile")
        return data

    def original_database(self, record):
        data = self.info()
        require(data["identity"] == record.get("original_identity"), "original PostgreSQL route identity changed or missing")
        return data

    def inspect_units(self):
        states = {}
        for unit in self.units:
            values = dict(line.split("=", 1) for line in run([
                "systemctl", "--user", "--no-pager", "show", "-p", "ActiveState", "-p", "MainPID", "-p", "ControlGroup", unit
            ]).splitlines())
            bus = ["busctl", "--user", "--json=short"]
            obj = json.loads(run([*bus, "call", "org.freedesktop.systemd1", "/org/freedesktop/systemd1",
                                  "org.freedesktop.systemd1.Manager", "LoadUnit", "s", unit]))["data"][0]
            conditions = json.loads(run([*bus, "get-property", "org.freedesktop.systemd1", obj,
                                          "org.freedesktop.systemd1.Unit", "Conditions"]))["data"]
            require(any(row[:4] == ["ConditionPathExists", False, True, str(self.block)] for row in conditions),
                    f"persistent restart guard absent or unenforced for {unit}")
            pid = int(values["MainPID"])
            states[unit] = dict(state=values["ActiveState"], pid=pid)
            if unit == self.units[1]:
                states[unit]["other_cgroup_processes"] = self.other_processes(values["ControlGroup"], pid)
        return states

    @staticmethod
    def other_processes(control_group, main_pid):
        if not control_group and not main_pid:
            return []
        require(control_group.startswith("/") and not control_group.startswith("//") and
                ".." not in Path(control_group).parts, "unexpected Linux node cgroup")
        relative = control_group.removeprefix("/")
        paths = [Path("/sys/fs/cgroup") / relative, Path("/sys/fs/cgroup/systemd") / relative]
        group = next((path for path in paths if (path / "cgroup.procs").is_file()), None)
        require(group is not None or not main_pid, "cannot inspect the active Linux node cgroup")
        if group is None:
            return []
        processes = {int(pid) for file in group.rglob("cgroup.procs") for pid in file.read_text().split()}
        return sorted(processes - {main_pid})

    def assert_stopped(self, record):
        require(all(item["pid"] == 0 and not item.get("other_cgroup_processes")
                    and item["state"] in ("inactive", "failed")
                    for item in self.inspect_units().values()), "original units did not stop")
        require(self.original_database(record)["clients"] == 0, "original database clients did not drain")

    def identity_guard(self, record):
        identity = record["original_identity"]
        databases = identity["databases"]
        require(re.fullmatch(r"[0-9]{1,20}", identity["cluster"]) and
                set(databases) == set(self.databases) and all(re.fullmatch(r"[0-9]{1,10}", oid) for oid in databases.values()),
                "invalid original database identity")
        placeholders = ",".join(f"'{db}'" for db in self.databases)
        return f"""DO $identity$
BEGIN
  IF (SELECT system_identifier::text FROM pg_control_system()) IS DISTINCT FROM '{identity["cluster"]}'
     OR (SELECT jsonb_object_agg(datname,oid::text) FROM pg_database WHERE datname IN ({placeholders}))
        IS DISTINCT FROM '{json.dumps(databases)}'::jsonb
  THEN RAISE EXCEPTION 'original PostgreSQL route identity changed';
  END IF;
END
$identity$;"""

    def set_flags(self, record, flags):
        require(set(flags) == set(self.databases) and all(type(flag) is bool for flag in flags.values()),
                "invalid original connection flags")
        guard = self.identity_guard(record)
        statements = " ".join(
            f"ALTER DATABASE {db} ALLOW_CONNECTIONS {'true' if flags[db] else 'false'};" for db in self.databases)
        self.sql(f"BEGIN; SET LOCAL lock_timeout = '8s'; SELECT pg_advisory_xact_lock({DB_LOCK}); "
                 + guard + statements + guard + " COMMIT;")
        require(self.original_database(record)["connections_allowed"] == flags,
                "database connection flags did not read back")

    def clone_catalog(self, names):
        require(all(re.fullmatch(IDENT, name) for name in names), "invalid clone database name")
        if not names:
            return {}
        selected = ", ".join(f"'{name}'" for name in names)
        return json.loads(self.sql(f"""
BEGIN READ ONLY;
SELECT coalesce(jsonb_object_agg(d.datname, jsonb_build_object(
 'oid', d.oid::text, 'accepts_connections', d.datallowconn, 'owner', d.datdba,
 'metadata', jsonb_build_array(d.encoding, d.datlocprovider, d.datcollate, d.datctype, d.datlocale,
                             d.daticurules, d.datcollversion, d.dattablespace, d.datconnlimit),
 'custom_acl', d.datacl IS NOT NULL,
 'custom_settings', EXISTS (SELECT 1 FROM pg_db_role_setting WHERE setdatabase=d.oid),
 'subscription', EXISTS (SELECT 1 FROM pg_subscription WHERE subdbid=d.oid),
 'prepared', EXISTS (SELECT 1 FROM pg_prepared_xacts WHERE database=d.datname),
 'connected', EXISTS (SELECT 1 FROM pg_stat_activity WHERE datid=d.oid))), '{{}}')
FROM pg_database d WHERE d.datname IN ({selected});
COMMIT;
"""))

    @staticmethod
    def assert_offline(data):
        require(not data["accepts_connections"] and not data["connected"] and not data["prepared"]
                and not data["subscription"], "snapshot database is not offline")

    def check_clones(self, record):
        clones = record.get("clones", {})
        quarantined = [*record.get("quarantined_clones", [])]
        if record.get("pending_clone"):
            quarantined.append(record["pending_clone"]["name"])
        catalog = self.clone_catalog([*(c["name"] for c in clones.values()), *quarantined])
        for name, item in catalog.items():
            self.assert_offline(item)
        for clone in clones.values():
            require(clone["name"] in catalog and catalog[clone["name"]]["oid"] == clone["oid"],
                    "verified clone route identity changed or disappeared")

    def clone_sealed(self, record):
        require(record["phase"] == "sealed" and self.block.is_file(), "site is not sealed")
        self.assert_stopped(record)
        self.check_clones(record)
        source = self.clone_catalog(self.databases)
        originals = record["original_identity"]["databases"]
        for db, expected_oid in originals.items():
            item = source[db]
            self.assert_offline(item)
            require(item["oid"] == expected_oid and not item["custom_acl"] and not item["custom_settings"],
                    "source has an unverified identity, database ACL or settings")

        # CREATE DATABASE cannot share a transaction with the file journal. Retain a possibly
        # created name offline after interruption and build a fresh clone instead of adopting it.
        if record.get("pending_clone"):
            record.setdefault("quarantined_clones", []).append(record.pop("pending_clone")["name"])
            save(self.journal, record)
        clones = record.setdefault("clones", {})
        for db in self.databases:
            if db in clones:
                continue
            name = f"{db[:18]}_staged_{record['attempt'][:10]}_{secrets.token_hex(6)}"
            require(not self.clone_catalog([name]), "clone name already exists")
            record["pending_clone"] = {"source": db, "name": name}
            save(self.journal, record)
            self.sql(f"""
SET lock_timeout = '8s';
SELECT pg_advisory_lock({DB_LOCK});
{self.identity_guard(record)}
DO $offline$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_database WHERE datname IN ('{self.databases[0]}', '{self.databases[1]}')
             AND datallowconn) OR EXISTS (SELECT 1 FROM pg_stat_activity
             WHERE datname IN ('{self.databases[0]}', '{self.databases[1]}')) THEN
      RAISE EXCEPTION 'original databases reopened before copying';
  END IF;
END
$offline$;
CREATE DATABASE {name} WITH TEMPLATE {db} CONNECTION LIMIT {source[db]['metadata'][-1]}
  ALLOW_CONNECTIONS false STRATEGY FILE_COPY;
{self.identity_guard(record)}
""", seconds=90)
            item = self.clone_catalog([name])[name]
            self.assert_offline(item)
            require(item["oid"] not in originals.values() and item["owner"] == source[db]["owner"]
                    and item["metadata"] == source[db]["metadata"]
                    and not item["custom_acl"] and not item["custom_settings"],
                    "clone identity or database metadata differs")
            self.assert_stopped(record)
            clones[db] = {"name": name, "oid": item["oid"], "source_oid": source[db]["oid"]}
            del record["pending_clone"]
            save(self.journal, record)
        self.check_clones(record)
        return dict(attempt=record["attempt"], phase="sealed", cluster=record["original_identity"]["cluster"],
                    clones=clones, quarantined=record.get("quarantined_clones", []))

    def proof(self, path, attempt, keys):
        require(path is not None, "missing separately verified site proof")
        raw = read_private(path)
        parsed = json.loads(raw)
        require(parsed.get("attempt") == attempt and all(parsed.get(key) is True for key in keys),
                "required same-attempt site evidence is absent")
        fingerprint = parsed.get("evidence_sha256", "")
        require(re.fullmatch(r"[0-9a-f]{64}", fingerprint), "proof has no source fingerprint")
        source = read_private(self.root / "evidence" / f"{fingerprint}.json")
        require(sha256(source).hexdigest() == fingerprint, "proof source fingerprint does not match")
        require(json.loads(source).get("attempt") == attempt, "proof source belongs to another attempt")
        return sha256(raw).hexdigest()

    def phase(self, record, phase):
        record.update(phase=phase)
        save(self.journal, record)

    def action(self, action, attempt, proof=None):
        require(re.fullmatch(r"[a-f0-9]{32}", attempt), "expected a pinned 32-hex attempt")
        if action == "inspect":
            return dict(attempt=attempt, units=self.inspect_units(), postgres=self.info(),
                        block_present=self.block.is_file(), journal_present=self.journal.is_file())
        with self.operation():
            record = json.loads(read_private(self.journal)) if self.journal.exists() else None
            if record is not None:
                require(record["attempt"] == attempt and record["profile"] == self.profile, "another site attempt owns the fence")
                self.original_database(record)
            if action == "fence":
                drain = self.proof(proof, attempt, ("ingress_closed", "all_writers_settled", "legacy_sender_verified"))
                current = self.inspect_units()
                require(not current[self.units[1]]["other_cgroup_processes"],
                        "Linux node cgroup still contains active descendants")
                if record is None:
                    require(not self.block.exists(), "unreconciled restart marker")
                    require(all(i["state"] == "active" and i["pid"] > 0 for i in current.values()),
                            "baseline services are not both active")
                    database = self.info()
                    record = dict(attempt=attempt, profile=self.profile, original_flags=database["connections_allowed"],
                                  original_identity=database["identity"], drain=drain)
                    self.phase(record, "stopping")
                require(record["phase"] in ("stopping", "stopped") and record["drain"] == drain,
                        "site is past this step or drain proof changed")
                save(self.block, dict(attempt=attempt))
                run(["systemctl", "--user", "stop", *self.units], seconds=130)
                self.assert_stopped(record)
                self.phase(record, "stopped")
                return dict(attempt=attempt, phase="stopped", restart_blocked=True)
            require(record is not None, "site has no recorded attempt")
            if action == "seal":
                require(record["phase"] in ("stopped", "sealing", "sealed") and self.block.is_file(), "site is not stopped")
                snapshot = self.proof(proof, attempt, ("original_recovery_plan_verified", "candidate_routes_distinct"))
                require(record.get("snapshot", snapshot) == snapshot, "snapshot proof changed")
                self.assert_stopped(record)
                record["snapshot"] = snapshot
                self.phase(record, "sealing")
                self.set_flags(record, dict.fromkeys(self.databases, False))
                self.assert_stopped(record)
                self.phase(record, "sealed")
                return dict(attempt=attempt, phase="sealed", originals_accept_connections=False)
            if action == "clone":
                return self.clone_sealed(record)
            if action == "commit-intent":
                require(record["phase"] in ("sealed", "commit_intent") and self.block.is_file(), "site is not sealed")
                require(not record.get("pending_clone")
                        and ("clones" not in record or set(record["clones"]) == set(self.databases)),
                        "candidate snapshot creation is incomplete")
                self.check_clones(record)
                admit = self.proof(proof, attempt, ("snapshots_verified_after_seal", "candidate_verified",
                                                    "candidate_still_isolated"))
                require(record.get("admit", admit) == admit, "candidate proof changed")
                record["admit"] = admit
                self.assert_stopped(record)
                require(not any(self.original_database(record)["connections_allowed"].values()), "original DB fence changed")
                self.phase(record, "commit_intent")
                return dict(attempt=attempt, phase="commit_intent", unsafe_to_restore_old_route=True)
            if action == "restore":
                require(record["phase"] in ("stopping", "stopped", "sealing", "sealed", "restoring", "restarted"),
                        "candidate may already have accepted writes")
                restore = self.proof(proof, attempt, ("candidate_stopped", "candidate_no_admitted_writes",
                                                      "original_route_verified", "ingress_closed"))
                require(record.get("restore", restore) == restore, "restore proof changed")
                self.check_clones(record)
                if record["phase"] == "restarted":
                    require(not self.block.exists() and
                            self.original_database(record)["connections_allowed"] == record["original_flags"] and
                            all(i["state"] == "active" and i["pid"] > 0 for i in self.inspect_units().values()),
                            "restarted site diverged from the recorded proof")
                    return dict(attempt=attempt, phase="restarted", already_restarted=True,
                                application_ingress_reopened_by_this_tool=False)
                record["restore"] = restore
                self.phase(record, "restoring")
                self.set_flags(record, record["original_flags"])
                # Repeat safely after an interruption here or after start.
                self.block.unlink(missing_ok=True)
                sync_dir(self.root)
                run(["systemctl", "--user", "start", *self.units], seconds=130)
                require(all(i["state"] == "active" and i["pid"] > 0 for i in self.inspect_units().values()),
                        "original services did not resume")
                self.phase(record, "restarted")
                return dict(attempt=attempt, phase="restarted", application_ingress_reopened_by_this_tool=False)
        raise ValueError("unknown site action")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("root", type=Path)
    parser.add_argument("action", choices=("inspect", "fence", "seal", "clone", "commit-intent", "restore"))
    parser.add_argument("--attempt", required=True)
    parser.add_argument("--proof", type=Path)
    args = parser.parse_args()
    try:
        print(json.dumps(Fence(args.root).action(args.action, args.attempt, args.proof)))
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError) as exc:
        print(json.dumps({"result":"failed", "reason":str(exc)}), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
