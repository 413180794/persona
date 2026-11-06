import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


SCRIPTS = Path(__file__).resolve().parent

# A fake docker for an existing persona-postgres container: `inspect` answers its network mode ($TEST_NETWORK), and
# `exec … pg_isready` answers like the real one for a Postgres listening on $TEST_LISTEN_PORT inside the container:
# ready when the port it asks (PGPORT, else 5432) is that one. Every call is logged to $TEST_LOG.
FAKE_DOCKER = r'''#!/bin/bash
printf '%s\n' "$*" >> "$TEST_LOG"
case "$1" in
  ps) echo persona-postgres ;;
  start) ;;
  inspect) echo "$TEST_NETWORK" ;;
  exec)
    shift; port=5432
    while [ "$1" = -e ]; do case "$2" in PGPORT=*) port=${2#PGPORT=} ;; esac; shift 2; done
    shift # the container
    case "$1" in
      pg_isready)
        if [ "$port" = "$TEST_LISTEN_PORT" ]; then echo "/var/run/postgresql:$port - accepting connections"
        else echo "/var/run/postgresql:$port - no response"; exit 2; fi ;;
      psql) echo 1 ;;
    esac ;;
esac
'''


class DbShTestCase(unittest.TestCase):
    """db.sh with an existing container, against the fake docker above (and a sleep that returns at once)."""

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="persona-db-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.state, self.tools = self.root / "state", self.root / "tools"
        self.state.mkdir()
        self.tools.mkdir()
        self.log = self.root / "docker.log"
        for name, text in (("docker", FAKE_DOCKER), ("sleep", "#!/bin/sh\n")):
            (self.tools / name).write_text(text)
            (self.tools / name).chmod(0o755)

    def run_db(self, network, listen_port, url_port):
        (self.state / "hub.json").write_text(json.dumps(
            {"database_url": "postgres://persona:pw@127.0.0.1:%d/persona?sslmode=disable" % url_port}))
        env = {"PATH": str(self.tools) + os.pathsep + os.defpath, "HOME": str(self.root),
               "_PERSONA_STATE_DIR": str(self.state), "TEST_LOG": str(self.log),
               "TEST_NETWORK": network, "TEST_LISTEN_PORT": str(listen_port)}
        return subprocess.run(["bash", str(SCRIPTS / "db.sh")], env=env, capture_output=True, text=True, timeout=30)

    def test_a_container_on_the_host_network_is_asked_on_the_port_the_hub_connects_to(self):
        # --network host with postgres -c port=…: inside the container Postgres listens where database_url says
        result = self.run_db("host", 25432, 25432)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("postgres ready", result.stdout)
        self.assertIn("exec -e PGPORT=25432 persona-postgres pg_isready", self.log.read_text())

    def test_the_container_db_sh_creates_is_asked_on_5432_not_the_published_port(self):
        # -p 127.0.0.1:$PORT:5432: database_url has the published port, Postgres in the container listens on 5432
        result = self.run_db("bridge", 5432, 15432)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("exec -e PGPORT=5432 persona-postgres pg_isready", self.log.read_text())
        self.assertIn("exec -e PGPORT=5432 persona-postgres psql", self.log.read_text())

    def test_a_database_that_never_answers_stops_the_deploy_with_the_reason(self):
        result = self.run_db("host", 5432, 25432)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("not ready on port 25432", result.stderr)
        self.assertIn("no response", result.stderr)
        self.assertNotIn("psql", self.log.read_text())


if __name__ == "__main__":
    unittest.main()
