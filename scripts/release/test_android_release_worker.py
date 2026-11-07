"""Release orchestration invariants independent of machine-specific credentials."""
import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("worker", Path(__file__).with_name("android_release_worker.py"))
worker = importlib.util.module_from_spec(spec)
spec.loader.exec_module(worker)


class ReleaseWorkerTest(unittest.TestCase):
    def test_versions_follow_main_history_and_retries_are_stable(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            def git(*args):
                return subprocess.check_output(["git", *args], cwd=root, text=True).strip()
            git("init", "-q")
            git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.com", "commit", "--allow-empty", "-qm", "first")
            first = git("rev-parse", "HEAD")
            code = worker.version_code(root, first)
            git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.com", "commit", "--allow-empty", "-qm", "second")
            second = git("rev-parse", "HEAD")
            self.assertEqual(code, worker.version_code(root, first))
            self.assertEqual(code + 1, worker.version_code(root, second))
            worker.check_history(root, first, second)
            with self.assertRaises(subprocess.CalledProcessError):
                worker.check_history(root, second, first)

    def test_failure_retains_successful_state_and_reports_failure(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            previous = {"source_sha": "a" * 40, "version_code": 100001}
            worker.atomic_json(root / "published.json", previous)
            config = {"state_directory": tmp}
            with patch.object(worker, "prepare", return_value=(root, "b" * 40)), \
                    patch.object(worker, "check_history"), patch.object(worker, "status") as status, \
                    patch.object(worker, "build", side_effect=RuntimeError("test build failure")), \
                    patch.object(worker, "signing") as signing:
                with self.assertRaisesRegex(RuntimeError, "test build failure"):
                    worker.cycle(config)
                self.assertEqual([call.args[2] for call in status.call_args_list], ["pending", "failure"])
                signing.assert_not_called()
            self.assertEqual(json.loads((root / "published.json").read_text()), previous)

    def test_signing_receipt_rejects_wrong_identity_and_hash(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            request = dict(source_sha="a" * 40, unsigned_sha256="b" * 64, package="com.example.app", version_code=2)
            worker.atomic_json(root / "request.json", request)
            (root / "signed.apk").write_bytes(b"signed")
            config = dict(signing_queue=tmp, certificate_sha256="c" * 64, signing_timeout_seconds=1)
            for changes in ({"package": "com.example.other"}, {"signed_sha256": "d" * 64},
                            {"certificate_sha256": "e" * 64}):
                receipt = dict(request, signed_sha256=worker.digest(root / "signed.apk"), certificate_sha256="c" * 64)
                receipt.update(changes)
                worker.atomic_json(root / "receipt.json", receipt)
                with self.assertRaises(ValueError):
                    worker.signing(config, request, root)

    def test_already_published_does_not_build_or_publish_again(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            worker.atomic_json(root / "published.json", {"source_sha": "a" * 40})
            with patch.object(worker, "prepare", return_value=(root, "a" * 40)), \
                    patch.object(worker, "build") as build:
                worker.cycle({"state_directory": tmp})
                build.assert_not_called()


if __name__ == "__main__":
    unittest.main()
