"""Exercise the unified model settings mock without a simulator or real providers."""
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import time
import unittest
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen


class ModelConfigMockTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        cls.base = f"http://127.0.0.1:{port}"
        cls.log = tempfile.TemporaryFile()
        cls.process = subprocess.Popen(
            ["python3", "-I", str(Path(__file__).with_name("mock_hub.py"))],
            env=dict(os.environ, PORT=str(port)), stdout=cls.log, stderr=cls.log)
        cls.addClassCleanup(cls.stop)
        for _ in range(60):
            if cls.process.poll() is not None:
                raise RuntimeError("mock hub exited before startup")
            try:
                with urlopen(cls.base + "/api/v1/health", timeout=1):
                    return
            except (URLError, TimeoutError):
                time.sleep(0.1)
        raise RuntimeError("mock hub startup timed out")

    @classmethod
    def stop(cls):
        cls.process.terminate()
        try:
            cls.process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            cls.process.kill()
            cls.process.wait(timeout=5)
        cls.log.close()

    def request(self, path, body=None):
        request = Request(self.base + path,
                          data=None if body is None else json.dumps(body).encode(),
                          method="GET" if body is None else "PATCH",
                          headers={"Content-Type": "application/json"})
        with urlopen(request, timeout=3) as response:
            return json.load(response)

    def test_catalog(self):
        catalog = self.request("/api/v1/model-config")
        ids = [p["id"] for p in catalog["purposes"]]
        self.assertIn("lark.agent", ids)
        self.assertIn("inbox.triage", ids)
        self.assertIn("kb.compress", ids)
        self.assertTrue(any(p.startswith("task.") for p in ids))
        self.assertEqual(len(ids), len(set(ids)))

    def test_save_conflict_and_restore(self):
        path = "/api/v1/model-config/purposes/lark.agent"
        original = self.request(path)["purpose"]
        saved = self.request(path, {"revision": original["revision"], "model": "model-mock-test"})["purpose"]
        self.assertEqual(saved["effective"]["model"], "model-mock-test")
        self.assertEqual(saved["revision"], original["revision"] + 1)
        with self.assertRaises(HTTPError) as error:
            self.request(path, {"revision": original["revision"], "model": "stale"})
        self.assertEqual(error.exception.code, 409)
        restored = self.request(path, {"revision": saved["revision"], "model": None})["purpose"]
        self.assertNotIn("model", restored["override"])
        self.assertEqual(restored["effective"]["model"], "model-chat")

    def test_unknown_purpose_is_not_created(self):
        for body in (None, {"revision": 0, "model": "x"}):
            with self.assertRaises(HTTPError) as error:
                self.request("/api/v1/model-config/purposes/unknown", body)
            self.assertEqual(error.exception.code, 404)


if __name__ == "__main__":
    unittest.main()
