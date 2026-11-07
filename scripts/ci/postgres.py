#!/usr/bin/env python3
import json
from pathlib import Path
import subprocess
import sys
import time

container, destination = sys.argv[1:]
for attempt in range(40):
    result = subprocess.run(["docker", "exec", container, "pg_isready", "-U", "persona", "-d", "persona_test"], timeout=5)
    if result.returncode == 0:
        break
    time.sleep(1)
else:
    sys.exit("Postgres readiness TIMEOUT")
port = subprocess.check_output(["docker", "port", container, "5432/tcp"], text=True, timeout=10).strip().rsplit(":", 1)[1]
Path(destination).write_text(json.dumps({"database_url": f"postgres://persona:ci-only@127.0.0.1:{port}/persona?sslmode=disable"}))
Path(destination).chmod(0o600)
print("PASS isolated Postgres fixture")
