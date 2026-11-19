#!/usr/bin/env python3
"""Read-only preflight for independent local development PostgreSQL routing."""
import json
import os
from pathlib import Path
import re
import runpy
import stat
import subprocess
import sys

base = runpy.run_path(str(Path(__file__).with_name("site-writer-fence.py")))
require, read_private, run = (base[key] for key in ("require", "read_private", "run"))
DOCKER = ["sudo", "-n", "docker"]
OLD = "persona-dev-postgres"
DBS = {"persona_dev", "persona_dev_manager"}
IDENT = r"[a-z][a-z0-9_]{0,62}"


def query(container, port, sql):
    return json.loads(run([*DOCKER, "exec", "-i", container, "sh", "-c",
        'exec psql -XqAt -v ON_ERROR_STOP=1 -h /var/run/postgresql -p "$1" -U "$POSTGRES_USER" -d postgres',
        "sh", str(port)], "BEGIN READ ONLY;\nSET LOCAL statement_timeout='4s';\n" + sql + ";\nCOMMIT;\n"))


def topology(container):
    fmt = "{{json .HostConfig.NetworkMode}}\n{{json .NetworkSettings.Ports}}\n{{json .Mounts}}"
    network, ports, mounts = map(json.loads, run([*DOCKER, "inspect", "--format", fmt, container]).splitlines())
    return network, ports, mounts


def readback(container, port, names):
    require(len(names) == 2 and all(re.fullmatch(IDENT, name) for name in names.values()),
            "invalid database names")
    pairs = ",".join(f"('{label}','{name}')" for label, name in sorted(names.items()))
    data = query(container, port, f"""
      WITH wanted(label,db) AS (VALUES {pairs})
      SELECT jsonb_build_object(
        'cluster',system_identifier::text,
        'listen',current_setting('listen_addresses'),
        'read_only',current_setting('transaction_read_only'),
        'databases',(SELECT jsonb_object_agg(label,d.oid::text) FROM wanted JOIN pg_database d ON d.datname=wanted.db),
        'client_count',(SELECT count(*) FROM pg_stat_activity WHERE datname IN (SELECT db FROM wanted)
                         AND backend_type='client backend')
      ) FROM pg_control_system()
    """)
    require(data["read_only"] == "on" and set(data["databases"] or {}) == DBS, "incomplete database readback")
    return data


def check(config):
    target, path, names = (config[key] for key in ("container", "socket", "databases"))
    require(re.fullmatch(r"persona-0551-fixture-[a-f0-9]{10}", target), "expected review-only target container")
    require(set(names) == DBS, "expected exact development database coverage")
    port = config["port"]
    require(type(port) is int and 1024 <= port <= 65535, "invalid fixture port")
    sock = Path(path)
    require(sock.is_absolute() and sock.resolve() == sock and sock.parent.parent == Path("/tmp")
            and sock.parent.name.startswith("persona-target-review-") and sock.name == "pgsock",
            "unexpected review socket path")
    info = sock.parent.stat()
    require(info.st_uid == os.getuid() and stat.S_IMODE(info.st_mode) == 0o700
            and stat.S_ISSOCK((sock / f".s.PGSQL.{port}").lstat().st_mode), "target socket is not private")
    _, _, old_mounts = topology(OLD)
    network, ports, new_mounts = topology(target)
    require(network == "none" and not any((ports or {}).values()), "target has network exposure")
    bound = [Path(m["Source"]).resolve() for m in new_mounts if m["Destination"] == "/var/run/postgresql"]
    require(bound == [sock], "target server is not bound to the declared socket")
    mounts = {Path(m["Source"]).resolve() for m in old_mounts if m["Source"]}
    for m in new_mounts:
        if m["Source"]:
            new = Path(m["Source"]).resolve()
            require(not any(new == old or new in old.parents or old in new.parents for old in mounts),
                    "source and target share a mount")
    source = readback(OLD, config["source_port"], {name: name for name in DBS})
    result = readback(target, port, names)
    require(source["cluster"] != result["cluster"] and result["listen"] == "", "target route is not independent")
    require(all(source["databases"][label] == oid for label, oid in config["source_identity"]["databases"].items())
            and set(config["source_identity"]["databases"]) == DBS
            and source["cluster"] == config["source_identity"]["cluster"], "source identity has changed")
    require(result["cluster"] == config["target_identity"]["cluster"]
            and result["databases"] == config["target_identity"]["databases"], "target identity has changed")
    require(result["client_count"] == 0, "review target already has application clients")
    return {"review_only": True, "production_ready": False, "source": source, "target": result,
            "socket_verified": True, "separate_mounts": True}


if __name__ == "__main__":
    try:
        require(len(sys.argv) == 2, "usage: check-target-route.py <private-review-config>")
        print(json.dumps(check(json.loads(read_private(Path(sys.argv[1]))))))
    except (ValueError, OSError, RuntimeError, KeyError, TypeError, subprocess.SubprocessError) as error:
        print(f"target route: {error}", file=sys.stderr)
        sys.exit(1)
