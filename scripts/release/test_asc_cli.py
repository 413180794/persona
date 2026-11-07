import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

HERE = Path(__file__).resolve().parent
SCRIPT = HERE / "asc_cli.py"
sys.path.insert(0, str(HERE))
import asc_cli  # noqa: E402

# Prints what asc would have received, so no test runs the real CLI or reaches App Store Connect.
FAKE_ASC = """#!/usr/bin/env python3
import json, os, sys
with open(os.environ["FAKE_ASC_OUT"], "w") as f:
    json.dump({"args": sys.argv[1:], "env": {k: v for k, v in os.environ.items() if k.startswith(("ASC_", "DO_NOT"))}}, f)
"""


class AscCliTests(unittest.TestCase):
    def setUp(self):
        holder = tempfile.TemporaryDirectory(prefix="asc cli ")
        self.addCleanup(holder.cleanup)
        self.tmp = Path(holder.name)
        self.key = self.tmp / "AuthKey_TEST.p8"
        self.key.write_text("placeholder, not a key\n")
        self.key.chmod(0o600)
        self.config = self.tmp / "config.json"
        self.fake = self.tmp / "asc"
        self.fake.write_text(FAKE_ASC)
        self.fake.chmod(0o755)
        self.out = self.tmp / "out.json"

    def env(self, **extra):
        base = {"PATH": os.environ["PATH"], "HOME": str(self.tmp), "XDG_CACHE_HOME": str(self.tmp / "cache"),
                "PERSONA_CONFIG": str(self.config), "FAKE_ASC_OUT": str(self.out)}
        base.update(extra)
        return base

    def write_config(self, **store):
        self.config.write_text(json.dumps({"app_store": store}))

    def run_cli(self, *args, **env):
        return subprocess.run([sys.executable, "-B", str(SCRIPT), *args], env=self.env(**env),
                              capture_output=True, text=True, timeout=30)

    def received(self):
        return json.loads(self.out.read_text())

    def test_without_a_key_it_says_what_to_set_and_runs_nothing(self):
        result = self.run_cli("builds", "list", ASC_BIN=str(self.fake))
        self.assertEqual(result.returncode, 1)
        for name in ("ASC_KEY_ID", "ASC_ISSUER_ID", "ASC_PRIVATE_KEY_PATH", "app_store.key_path", "docs/operations/release.md"):
            self.assertIn(name, result.stderr)
        self.assertFalse(self.out.exists())

    def test_a_missing_key_file_is_named(self):
        self.write_config(key_id="KEY1234", issuer_id="issuer-secret", key_path=str(self.tmp / "gone.p8"))
        result = self.run_cli("check")
        self.assertEqual(result.returncode, 1)
        self.assertIn("gone.p8 does not exist", result.stderr)

    def test_check_reads_the_config_offline_and_prints_no_secret(self):
        self.write_config(key_id="KEY1234", issuer_id="issuer-secret", key_path=str(self.key), app_id="123456")
        result = self.run_cli("check")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(result.stdout.strip().splitlines()[-1].startswith("ok:"))
        self.assertIn("…1234", result.stdout)
        self.assertIn("123456", result.stdout)
        self.assertIn("not installed yet", result.stdout)
        self.assertNotIn("KEY1234", result.stdout)
        self.assertNotIn("issuer-secret", result.stdout)
        self.assertNotIn("placeholder", result.stdout)
        self.assertFalse((self.tmp / "cache").exists(), "check must not download anything")

    def test_check_flags_a_key_file_others_can_read(self):
        self.write_config(key_id="KEY1234", issuer_id="i", key_path=str(self.key))
        self.key.chmod(0o644)
        self.assertIn("chmod 600", self.run_cli("check").stdout)

    def test_runs_asc_with_the_config_key_and_telemetry_off(self):
        self.write_config(key_id="KEY1234", issuer_id="issuer", key_path=str(self.key), app_id="123456")
        result = self.run_cli("builds", "list", "--output", "table", ASC_BIN=str(self.fake),
                              ASC_PROFILE="other", ASC_PRIVATE_KEY_B64="inline", ASC_CONFIG_PATH="/elsewhere")
        self.assertEqual(result.returncode, 0, result.stderr)
        got = self.received()
        self.assertEqual(got["args"], ["builds", "list", "--output", "table"])
        self.assertEqual(got["env"]["ASC_KEY_ID"], "KEY1234")
        self.assertEqual(got["env"]["ASC_ISSUER_ID"], "issuer")
        self.assertEqual(got["env"]["ASC_PRIVATE_KEY_PATH"], str(self.key))
        self.assertEqual(got["env"]["ASC_APP_ID"], "123456")
        self.assertEqual(got["env"]["ASC_TELEMETRY_DISABLED"], "1")
        self.assertEqual(got["env"]["DO_NOT_TRACK"], "1")
        for name in asc_cli.FOREIGN:
            self.assertNotIn(name, got["env"])

    def test_the_environment_wins_over_the_config(self):
        self.write_config(key_id="CONFIG1", issuer_id="config", key_path=str(self.tmp / "gone.p8"))
        result = self.run_cli("apps", "list", ASC_BIN=str(self.fake),
                              ASC_KEY_ID="ENV1", ASC_ISSUER_ID="env", ASC_KEY_PATH=str(self.key))
        self.assertEqual(result.returncode, 0, result.stderr)
        env = self.received()["env"]
        self.assertEqual((env["ASC_KEY_ID"], env["ASC_ISSUER_ID"], env["ASC_PRIVATE_KEY_PATH"]),
                         ("ENV1", "env", str(self.key)))
        self.assertNotIn("ASC_APP_ID", env)

    def test_help_and_version_need_no_key(self):
        for args in (["--help"], ["builds", "upload", "--help"], ["version"]):
            with self.subTest(args=args):
                result = self.run_cli(*args, ASC_BIN=str(self.fake))
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(self.received()["args"], args)


class InstallTests(unittest.TestCase):
    def setUp(self):
        holder = tempfile.TemporaryDirectory(prefix="asc cache ")
        self.addCleanup(holder.cleanup)
        self.cache = holder.name
        patcher = mock.patch.dict(os.environ, {"XDG_CACHE_HOME": self.cache})
        patcher.start()
        self.addCleanup(patcher.stop)

    def test_a_download_with_another_checksum_is_not_installed(self):
        with mock.patch.object(asc_cli, "fetch", return_value=b"tampered"), \
                mock.patch.object(asc_cli, "target", return_value="macOS_arm64"):
            with self.assertRaises(SystemExit) as raised:
                asc_cli.install()
        self.assertIn("not installed", str(raised.exception))
        self.assertFalse(os.path.exists(asc_cli.cached()))

    def test_the_pinned_checksum_installs_an_executable_once(self):
        data = b"asc build"
        pins = {"macOS_arm64": hashlib.sha256(data).hexdigest()}
        with mock.patch.object(asc_cli, "fetch", return_value=data) as fetch, \
                mock.patch.object(asc_cli, "target", return_value="macOS_arm64"), \
                mock.patch.object(asc_cli, "SHA256", pins):
            path = asc_cli.install()
            self.assertEqual(asc_cli.install(), path)
        self.assertEqual(fetch.call_count, 1)
        self.assertEqual(fetch.call_args[0][0],
                         f"{asc_cli.RELEASES}/{asc_cli.VERSION}/asc_{asc_cli.VERSION}_macOS_arm64")
        self.assertTrue(os.access(path, os.X_OK))
        self.assertIn(os.path.join("persona", "asc", asc_cli.VERSION), path)


if __name__ == "__main__":
    unittest.main()
