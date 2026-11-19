#!/usr/bin/env python3
"""Provision a new private, review-only PostgreSQL instance for a cold restore."""
import argparse
import os
from pathlib import Path
import re
import runpy
import secrets
import stat
import subprocess
import tempfile
import time

tools = runpy.run_path(str(Path(__file__).with_name("check-target-route.py")))
fence = runpy.run_path(str(Path(__file__).with_name("site-writer-fence.py")))
check, readback, query = (tools[k] for k in ("check", "readback", "query"))
DOCKER, OLD, DBS = (tools[k] for k in ("DOCKER", "OLD", "DBS"))
require, run, save = (fence[k] for k in ("require", "run", "save"))
PORT = 55455


def prove_empty(container, names):
    for db in names.values():
        command = [*DOCKER, "exec", "-i", container, "sh", "-c",
                   'exec psql -XqAt -v ON_ERROR_STOP=1 -h /var/run/postgresql -p "$1" -U "$POSTGRES_USER" -d "$2"',
                   "sh", str(PORT), db]
        tables = run(command, """
          BEGIN READ ONLY;
          SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
          WHERE c.relkind IN ('r','m','p','S','v','f') AND n.nspname NOT LIKE 'pg_%' AND n.nspname<>'information_schema';
          COMMIT;
        """)
        require(tables == "0", "a restore target already contains user relations")


def prepare(image, receipt):
    require(re.fullmatch(r"sha256:[0-9a-f]{64}", image), "use a pinned local image ID")
    require(receipt.parent.is_dir() and receipt.parent.stat().st_uid == os.getuid()
            and stat.S_IMODE(receipt.parent.stat().st_mode) == 0o700 and not receipt.exists(),
            "expected a new receipt under an owner-only directory")
    label = "persona-0551-fixture-" + secrets.token_hex(5)
    root = Path(tempfile.mkdtemp(prefix="persona-target-review-", dir="/tmp"))
    sock = root / "pgsock"
    sock.mkdir()
    # Docker creates the socket as its PG user. The parent retains mode 0700.
    sock.chmod(0o777)
    names = {"persona_dev": "lab_hub", "persona_dev_manager": "lab_hub_manager"}
    control = [*DOCKER, "exec", "-i", label, "sh", "-c"]
    try:
        run([*DOCKER, "run", "-d", "--name", label, "--network=none", "--pull=never",
             "--label", "persona.purpose=empty-review-target",
             "-v", f"{sock}:/var/run/postgresql", "-e", "POSTGRES_HOST_AUTH_METHOD=trust", "-e", "POSTGRES_USER=persona_review_admin",
             image, "postgres", "-p", str(PORT), "-c", "listen_addresses=", "-c", "unix_socket_directories=/var/run/postgresql"])
        for _ in range(80):
            try:
                query(label, PORT, "SELECT jsonb_build_object('ok',true)")
                break
            except (RuntimeError, OSError, subprocess.SubprocessError):
                time.sleep(.25)
        else:
            raise RuntimeError("private PostgreSQL did not become ready")
        for name in names.values():
            run(control + ['exec createdb -h /var/run/postgresql -p "$1" -U "$POSTGRES_USER" -- "$2"',
                           "sh", str(PORT), name])
        original = readback(OLD, 5432, {name: name for name in DBS})
        target = readback(label, PORT, names)
        keyed = lambda data: {key: data[key] for key in ("cluster", "databases")}
        config = {
            "container": label, "socket": str(sock), "port": PORT, "source_port": 5432,
            "databases": names, "source_identity": keyed(original), "target_identity": keyed(target),
        }
        result = check(config)
        prove_empty(label, names)
        save(receipt, dict(config, preflight=result, restore_empty=True, production_enabled=False))
    except BaseException:
        subprocess.run([*DOCKER, "rm", "-f", "-v", label], capture_output=True, timeout=25, check=False)
        raise


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("local_image_id")
    parser.add_argument("private_receipt", type=Path)
    args = parser.parse_args()
    prepare(args.local_image_id, args.private_receipt)
