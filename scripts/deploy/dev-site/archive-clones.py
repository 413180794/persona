#!/usr/bin/env python3
"""Export sealed development clones through a separately verified local administrator route."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import runpy
import subprocess
import tempfile

fence_code = runpy.run_path(str(Path(__file__).with_name("site-writer-fence.py")))
Fence = fence_code["Fence"]
require, read_private, save = (fence_code[name] for name in ("require", "read_private", "save"))
IDENT = fence_code["IDENT"]


class Archive:
    def __init__(self, site, attempt):
        self.fence = Fence(site)
        self.attempt = attempt
        self.pending = self.fence.root / "archive-open.json"
        self.receipt = self.fence.root / "archives.json"
        profile = json.loads(read_private(self.fence.root / "site.json"))
        self.user = profile.get("archive_admin", "")
        require(re.fullmatch(r"[a-z][a-z0-9_.-]{0,62}", self.user) is not None,
                "site must pin an archive administrator")

    def record(self):
        fence = self.fence
        record = json.loads(read_private(fence.journal))
        require(record["attempt"] == self.attempt and record["profile"] == fence.profile
                and record["phase"] == "sealed" and fence.block.is_file()
                and set(record.get("clones", {})) == set(fence.databases) and not record.get("pending_clone"),
                "need the complete sealed clone pair from this attempt")
        fence.assert_stopped(record)
        require(not any(fence.original_database(record)["connections_allowed"].values()), "originals reopened")
        return record

    def command(self, *args):
        cmd = ["sudo", "-n", "docker", "exec", "-i"]
        if self.fence.fixture:
            cmd.extend(("-e", "PGPORT=55455"))
        return [*cmd, self.fence.container, *args]

    def names(self, record):
        names = [record["clones"][db]["name"] for db in self.fence.databases]
        require(all(re.fullmatch(IDENT, name) is not None for name in names), "invalid clone route name")
        return names

    def hba(self, record):
        names = ",".join(self.names(record))
        return (f'local {names} "{self.user}" trust\nlocal {names} all reject\n'
                f"host {names} all 0.0.0.0/0 reject\nhost {names} all ::/0 reject\n")

    def check_route(self, record, proof):
        digest = self.fence.proof(proof, self.attempt, ("administrator_socket_exclusive", "deny_probes_observed"))
        catalog = json.loads(self.fence.sql("""
BEGIN READ ONLY;
SELECT jsonb_build_object('invalid', EXISTS(SELECT 1 FROM pg_hba_file_rules WHERE error IS NOT NULL),
  'first', (SELECT jsonb_agg(to_jsonb(h) ORDER BY rule_number) FROM
    (SELECT rule_number,type,database,user_name,address,netmask,auth_method FROM pg_hba_file_rules
     ORDER BY rule_number LIMIT 4) h));
COMMIT;
"""))
        names = self.names(record)
        expected = [
            ("local", [self.user], None, None, "trust"),
            ("local", ["all"], None, None, "reject"),
            ("host", ["all"], "0.0.0.0", "0.0.0.0", "reject"),
            ("host", ["all"], "::", "::", "reject"),
        ]
        require(not catalog["invalid"] and catalog["first"] and len(catalog["first"]) == len(expected),
                "archive HBA rules are incomplete or invalid")
        for rule, values in zip(catalog["first"], expected):
            actual = tuple(rule[k] for k in ("type", "user_name", "address", "netmask", "auth_method"))
            require(actual == values and rule["database"] == names, "archive HBA deny rules are not first")
        return digest

    def set_clone_flag(self, record, entry, enabled):
        name, oid = entry["name"], entry["oid"]
        require(re.fullmatch(IDENT, name) and re.fullmatch(r"[0-9]+", oid), "invalid clone identity")
        guard = self.fence.identity_guard(record)
        self.fence.sql(f"""
BEGIN;
SET LOCAL lock_timeout = '8s';
SELECT pg_advisory_xact_lock({fence_code['DB_LOCK']});
{guard}
DO $route$
BEGIN
  IF (SELECT oid::text FROM pg_database WHERE datname='{name}') IS DISTINCT FROM '{oid}'
     OR EXISTS (SELECT 1 FROM pg_database WHERE datname IN
       ('{self.fence.databases[0]}','{self.fence.databases[1]}') AND datallowconn) THEN
    RAISE EXCEPTION 'archive route identity or original fence changed';
  END IF;
END
$route$;
ALTER DATABASE {name} ALLOW_CONNECTIONS {'true' if enabled else 'false'};
COMMIT;
""")

    def reclose(self, record):
        if not self.pending.exists():
            return
        pending = json.loads(read_private(self.pending))
        require(pending["attempt"] == self.attempt and pending["clone"] in record["clones"].values(),
                "archive recovery belongs to another cloned route")
        entry = pending["clone"]
        self.set_clone_flag(record, entry, False)
        self.fence.assert_offline(self.fence.clone_catalog([entry["name"]])[entry["name"]])
        self.pending.unlink()
        fence_code["sync_dir"](self.fence.root)

    def export(self, record, proof):
        self.reclose(record)
        self.fence.check_clones(record)
        proof_sha = self.check_route(record, proof)
        archive_hashes = {}
        for source, clone in record["clones"].items():
            self.check_route(record, proof)
            save(self.pending, {"attempt": self.attempt, "clone": clone})
            try:
                self.set_clone_flag(record, clone, True)
                archive_hashes[source] = self.dump(source, clone)
            finally:
                self.reclose(record)
        self.fence.check_clones(record)
        manifest = {"attempt": self.attempt, "administrator_route_sha256": proof_sha, "originals_sealed": True,
                    "archives": archive_hashes}
        save(self.receipt, manifest)
        return manifest

    def dump(self, source, clone):
        with tempfile.NamedTemporaryFile("w+b", dir=self.fence.root, prefix=".archive-", delete=False) as output:
            temporary = Path(output.name)
            try:
                subprocess.run(self.command("pg_dump", "-w", "-h", "/var/run/postgresql", "-U", self.user,
                                            "-d", clone["name"], "--lock-wait-timeout=2s", "--format=custom"),
                               stdout=output, stderr=subprocess.DEVNULL, check=True, timeout=120)
                output.seek(0)
                subprocess.run(self.command("pg_restore", "--list"), stdin=output, stdout=subprocess.DEVNULL,
                               stderr=subprocess.DEVNULL, check=True, timeout=15)
                output.seek(0)
                sha = hashlib.file_digest(output, "sha256").hexdigest()
                os.fsync(output.fileno())
                destination = self.fence.root / f"{source}-{sha}.dump"
                temporary.replace(destination)
                fence_code["sync_dir"](self.fence.root)
                return {"name": destination.name, "sha256": sha, "source_oid": clone["source_oid"]}
            finally:
                temporary.unlink(missing_ok=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("site", type=Path)
    parser.add_argument("action", choices=("hba", "check", "export", "reclose"))
    parser.add_argument("--attempt", required=True)
    parser.add_argument("--proof", type=Path)
    args = parser.parse_args()
    try:
        archive = Archive(args.site, args.attempt)
        with archive.fence.operation():
            record = archive.record()
            if args.action == "hba":
                print(archive.hba(record), end="")
            elif args.action == "check":
                print(json.dumps({"attempt": args.attempt, "route_proof": archive.check_route(record, args.proof)}))
            elif args.action == "reclose":
                archive.reclose(record)
                archive.fence.check_clones(record)
                print(json.dumps({"attempt": args.attempt, "closed": True}))
            else:
                print(json.dumps(archive.export(record, args.proof)))
    except (OSError, ValueError, RuntimeError, KeyError, TypeError, subprocess.SubprocessError) as exc:
        parser.exit(1, f"archive: {exc}\n")


if __name__ == "__main__":
    main()
