#!/usr/bin/env python3
"""Run server tests without inheriting a live Persona profile or runtime configuration."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
from urllib.parse import urlsplit, urlunsplit

RUNTIME_PREFIXES = ("PERSONA_", "JARVIS_")
PROFILE_VARIABLES = {
    "CLAUDE_CONFIG_DIR", "CODEX_HOME", "CODEX_CONFIG_DIR",
    "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME",
    "BASH_ENV", "ENV", "ZDOTDIR",
}


def test_database_url(config_file):
    with open(config_file) as source:
        config = json.load(source)
    value = config.get("database_url", "")
    if not isinstance(value, str):
        raise ValueError("hub database_url must be a Postgres URL")
    parsed = urlsplit(value)
    if parsed.scheme not in {"postgres", "postgresql"} or not parsed.netloc or parsed.fragment:
        raise ValueError("hub database_url must be a Postgres URL")
    if parsed.path != "/jarvis":
        raise ValueError("server tests require the production jarvis database configuration")
    test_url = urlunsplit(parsed._replace(path="/jarvis_test"))
    if test_url == value:
        raise ValueError("server test database must differ from the production database")
    return test_url


def clean_environment(environment):
    cleaned = {key: value for key, value in environment.items()
               if not key.startswith(RUNTIME_PREFIXES) and key not in PROFILE_VARIABLES}
    roots = [Path(value).expanduser().resolve() for key, value in environment.items()
             if key in {"PERSONA_HOME", "JARVIS_HOME"} and value]
    if roots and "PATH" in cleaned:
        entries = []
        for entry in cleaned["PATH"].split(os.pathsep):
            if not entry:
                continue
            path = Path(entry).expanduser().resolve()
            if any(path == root or root in path.parents for root in roots):
                continue
            entries.append(entry)
        cleaned["PATH"] = os.pathsep.join(entries)
    return cleaned


def run_tests(config_file, command, environment=None):
    database = test_database_url(config_file)
    environment = clean_environment(os.environ if environment is None else environment)
    # Resolve only build/toolchain cache locations before replacing HOME. Never carry the service profile into Go.
    result = subprocess.run(["go", "env", "-json", "GOCACHE", "GOMODCACHE", "GOPATH"],
                            env=environment, capture_output=True, text=True, timeout=30)
    if result.returncode:
        raise ValueError("could not resolve Go build cache locations")
    caches = json.loads(result.stdout)
    if not isinstance(caches, dict):
        raise ValueError("invalid Go build cache locations")
    for key in ("GOCACHE", "GOMODCACHE", "GOPATH"):
        value = caches.get(key)
        if not isinstance(value, str) or not value:
            raise ValueError("invalid Go build cache locations")
        environment[key] = value
    with tempfile.TemporaryDirectory(prefix="persona-server-test-") as directory:
        home = Path(directory).resolve()
        temporary = home / "tmp"
        temporary.mkdir()
        environment.update(HOME=str(home), TMPDIR=str(temporary), TMP=str(temporary), TEMP=str(temporary),
                           JARVIS_TEST_DATABASE_URL=database, PERSONA_TEST_DATABASE_URL=database)
        return subprocess.run(command, env=environment).returncode


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--database-file", required=True)
    parser.add_argument("command", nargs=argparse.REMAINDER)
    args = parser.parse_args()
    command = args.command[1:] if args.command[:1] == ["--"] else args.command
    if not command:
        parser.error("a test command is required")
    try:
        return run_tests(args.database_file, command)
    except (OSError, ValueError, subprocess.TimeoutExpired):
        # Exceptions can contain credentials or profile paths; never echo config values or captured command output.
        print("server test environment preparation failed; tests were not started", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
