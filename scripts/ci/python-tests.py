#!/usr/bin/env python3
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(Path(__file__).resolve().parent))
from run import run
sys.path.insert(0, str(ROOT / "scripts" / "test"))
from server_test_env import clean_environment


def main():
    suites = sorted(ROOT.joinpath("scripts").rglob("test_*.py"))
    suites = [path for path in suites if "node_modules" not in path.parts]
    if not suites:
        sys.exit("FAIL no Python script suites discovered")
    caches = json.loads(subprocess.check_output(["go", "env", "-json", "GOCACHE", "GOMODCACHE", "GOPATH"], text=True, timeout=30))
    failed = []
    with tempfile.TemporaryDirectory(prefix="persona-ci-home-", dir=os.environ.get("TMPDIR")) as home:
        cleaned = clean_environment(dict(os.environ))
        os.environ.clear()
        os.environ.update(cleaned)
        os.environ.update(caches)
        os.environ.update(HOME=home)
        reservations = [socket.socket(), socket.socket()]
        try:
            for reservation in reservations:
                reservation.bind(("127.0.0.1", 0))
            os.environ["MOCK_MESH_TEST_PORTS"] = ",".join(str(reservation.getsockname()[1]) for reservation in reservations)
        finally:
            for reservation in reservations:
                reservation.close()
        for suite in suites:
            os.chdir(suite.parent)
            label = str(suite.relative_to(ROOT))
            if run(label, 120, [sys.executable, "-B", "-m", "unittest", "-q", suite.stem]):
                failed.append(label)
    print(f"Python suites: {len(suites)}; failed: {len(failed)}")
    for suite in failed:
        print(f"FAIL {suite}")
    return bool(failed)


if __name__ == "__main__":
    sys.exit(main())
