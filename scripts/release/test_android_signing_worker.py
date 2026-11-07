import contextlib
import fcntl
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest import mock

import android_signing_worker as worker

CERT = "a" * 64


class SigningWorkerTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.queue = self.root / "queue"
        self.queue.mkdir()
        self.apk = self.queue / "unsigned.apk"
        self.apk.write_bytes(b"unsigned")
        self.config = dict(ssh_target="builder", remote_queue_dir=str(self.queue),
                           keystore=str(self.root / "private.keystore"), alias="release", password_command=["password-reader"],
                           expected_cert_sha256=CERT, expected_package="test.persona", lock_file=str(self.root / "sign.lock"))
        self.request = dict(source_sha="b" * 40, unsigned_sha256=worker.digest_file(self.apk),
                            package="test.persona", version_code=42)
        (self.queue / "request.json").write_text(json.dumps(self.request))
        self.password = "secret-never-log-me"
        self.calls = []

    def fake_execute(self, command, *, environment=None):
        self.calls.append(command)
        if command[0] == "ssh":
            return subprocess.check_output(["sh", "-c", command[-1]], text=True)
        if command[0] == "scp":
            source, destination = command[-2:]
            shutil.copyfile(source.removeprefix("builder:"), destination.removeprefix("builder:"))
            return ""
        if command[0] == "password-reader":
            return self.password + "\n"
        if command[0] == "aapt2":
            return "package: name='test.persona' versionCode='42' versionName='1.0'\n"
        if command[0] == "zipalign":
            if "-c" not in command:
                shutil.copyfile(command[-2], command[-1])
            return ""
        if command[:2] == ["apksigner", "sign"]:
            self.assertEqual(environment["PERSONA_APK_STORE_PASSWORD"], self.password)
            self.assertNotIn(self.password, repr(command))
            Path(command[command.index("--out") + 1]).write_bytes(b"signed")
            return ""
        if command[:2] == ["apksigner", "verify"]:
            return "Number of signers: 1\nV3.0 Signer: certificate SHA-256 digest: " + CERT + "\n"
        raise AssertionError("unexpected command")

    def run_worker(self):
        with mock.patch.object(worker, "execute", side_effect=self.fake_execute):
            with mock.patch.object(worker, "sdk_tool", side_effect=lambda config, name: name):
                return worker.run(self.config)

    def test_full_poll_returns_atomic_receipt_without_secrets(self):
        self.assertEqual(self.run_worker(), "signed " + self.request["source_sha"])
        receipt = json.loads((self.queue / "receipt.json").read_text())
        self.assertEqual(receipt["signed_sha256"], worker.digest_file(self.queue / "signed.apk"))
        self.assertEqual(receipt["certificate_sha256"], CERT)
        self.assertEqual(receipt["unsigned_sha256"], self.request["unsigned_sha256"])
        self.assertEqual(list(self.queue.glob("*.pending-*")), [])
        self.assertNotIn(self.password, repr(self.calls))
        self.assertNotIn("PERSONA_APK_STORE_PASSWORD", os.environ)
        self.calls.clear()
        self.assertEqual(self.run_worker(), "already signed")
        self.assertFalse(any(command[0] == "apksigner" for command in self.calls))

    def assert_recovers_receipt(self, damage):
        self.assertEqual(self.run_worker(), "signed " + self.request["source_sha"])
        damage()
        self.calls.clear()
        self.assertEqual(self.run_worker(), "signed " + self.request["source_sha"])
        self.assertTrue(any(command[:2] == ["apksigner", "sign"] for command in self.calls))
        receipt = json.loads((self.queue / "receipt.json").read_text())
        self.assertEqual(receipt["signed_sha256"], worker.digest_file(self.queue / "signed.apk"))
        self.assertEqual(receipt["certificate_sha256"], CERT)
        self.assertEqual(self.run_worker(), "already signed")

    def test_missing_signed_apk_is_signed_again(self):
        self.assert_recovers_receipt(lambda: (self.queue / "signed.apk").unlink())

    def test_corrupt_signed_apk_is_signed_again(self):
        self.assert_recovers_receipt(lambda: (self.queue / "signed.apk").write_bytes(b"corrupt"))

    def test_wrong_receipt_certificate_is_signed_again(self):
        def damage():
            path = self.queue / "receipt.json"
            receipt = json.loads(path.read_text())
            receipt["certificate_sha256"] = "d" * 64
            path.write_text(json.dumps(receipt))
        self.assert_recovers_receipt(damage)

    def test_malformed_receipt_is_signed_again(self):
        for raw in ("{broken", "null", "[]"):
            with self.subTest(raw=raw):
                # Each damaged receipt is repaired before the next subtest.
                (self.queue / "receipt.json").unlink(missing_ok=True)
                self.assert_recovers_receipt(lambda: (self.queue / "receipt.json").write_text(raw))

    def test_corrupt_unsigned_is_rejected_before_signing(self):
        self.apk.write_bytes(b"tampered")
        with self.assertRaisesRegex(ValueError, "hash differs"):
            self.run_worker()
        self.assertFalse(any(command[0] == "password-reader" for command in self.calls))
        self.assertFalse((self.queue / "receipt.json").exists())

    def test_metadata_mismatch_is_rejected(self):
        self.request["version_code"] = 43
        (self.queue / "request.json").write_text(json.dumps(self.request))
        with self.assertRaisesRegex(ValueError, "package/version"):
            self.run_worker()
        self.assertFalse((self.queue / "receipt.json").exists())

    def test_wrong_certificate_is_rejected(self):
        self.config["expected_cert_sha256"] = "c" * 64
        with self.assertRaisesRegex(ValueError, "certificate differs"):
            self.run_worker()
        self.assertFalse((self.queue / "receipt.json").exists())

    def test_changed_request_is_not_marked_ready(self):
        execute = self.fake_execute

        def change_before_commit(command, **kwargs):
            if command[0] == "ssh" and command[-1].startswith("python3 -c"):
                changed = dict(self.request, source_sha="d" * 40)
                (self.queue / "request.json").write_text(json.dumps(changed))
                result = subprocess.run(["sh", "-c", command[-1]], capture_output=True, text=True)
                if result.returncode:
                    raise ValueError("remote commit failed")
            return execute(command, **kwargs)

        with mock.patch.object(worker, "execute", side_effect=change_before_commit):
            with mock.patch.object(worker, "sdk_tool", side_effect=lambda config, name: name):
                with self.assertRaisesRegex(ValueError, "remote commit failed"):
                    worker.run(self.config)
        self.assertFalse((self.queue / "receipt.json").exists())
        self.assertFalse((self.queue / "signed.apk").exists())
        self.assertEqual(list(self.queue.glob("*.pending-*")), [])

    def test_empty_queue_is_idle(self):
        (self.queue / "request.json").unlink()
        self.assertEqual(self.run_worker(), "idle")

    def test_concurrent_poll_skips_locked_worker(self):
        with Path(self.config["lock_file"]).open("a") as lock:
            fcntl.flock(lock.fileno(), fcntl.LOCK_EX)
            self.assertEqual(self.run_worker(), "another signing poll is running")
        self.assertEqual(self.calls, [])

    def test_config_rejects_unsafe_paths_and_password_string(self):
        for key, value in (("remote_queue_dir", "/tmp/queue;touch BAD"), ("ssh_target", "-oProxyCommand=bad"),
                           ("password_command", "secret-tool read")):
            config = dict(self.config, **{key: value})
            with self.assertRaises(ValueError):
                worker.validate_config(config)

    def test_subprocess_failure_does_not_disclose_password_output(self):
        result = subprocess.CompletedProcess(["password-reader"], 1, self.password, self.password)
        with mock.patch.object(worker.subprocess, "run", return_value=result):
            with self.assertRaisesRegex(ValueError, "subprocess failed") as caught:
                worker.execute(["password-reader"])
        self.assertNotIn(self.password, str(caught.exception))

    def test_cli_failure_does_not_print_secret_json(self):
        config = self.root / "config.json"
        config.write_text(self.password)
        output = io.StringIO()
        with mock.patch("sys.argv", ["worker", "--config", str(config)]):
            with contextlib.redirect_stderr(output):
                with self.assertRaises(SystemExit):
                    worker.main()
        self.assertNotIn(self.password, output.getvalue())


if __name__ == "__main__":
    unittest.main()
