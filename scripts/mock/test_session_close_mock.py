"""The close mock keeps history, isolates targets, returns failures and can be reset."""
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


class SessionCloseMockTests(unittest.TestCase):
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
                raise RuntimeError("mock hub 启动失败")
            try:
                with urlopen(cls.base + "/api/v1/health", timeout=1):
                    return
            except (URLError, TimeoutError):
                time.sleep(0.1)
        raise RuntimeError("mock hub 启动超时")

    @classmethod
    def stop(cls):
        cls.process.terminate()
        try:
            cls.process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            cls.process.kill()
            cls.process.wait(timeout=5)
        cls.log.close()

    def request(self, path, method="GET"):
        req = Request(self.base + path, method=method,
                      data=b"{}" if method == "POST" else None,
                      headers={"Content-Type": "application/json", "Authorization": "Bearer mock"})
        with urlopen(req, timeout=3) as response:
            return json.load(response)

    def setUp(self):
        self.request("/mock/reset")
        self.target = "a1f3c2d4-0000-4000-8000-000000000006"
        self.close = "/api/v1/sessions/macbook/" + self.target + "/close"

    def sessions(self):
        return {s["session_id"]: s for s in self.request("/api/v1/sessions")["sessions"]}

    def test_close_preserves_history_and_other_agents(self):
        before = self.sessions()
        self.request(self.close, "POST")
        after = self.sessions()
        self.assertEqual(after[self.target]["state"], "offline")
        self.assertEqual(after[self.target]["title"], before[self.target]["title"])
        for sid in before.keys() - {self.target}:
            self.assertEqual(after[sid]["state"], before[sid]["state"])
        self.assertEqual(self.request("/mock/session_close")["closed"], ["macbook/" + self.target])
        self.request("/mock/reset")
        self.assertEqual(self.sessions()[self.target]["state"], before[self.target]["state"])
        self.assertEqual(self.request("/mock/session_close")["attempts"], [])

    def test_failure_does_not_remove_the_agent(self):
        before = self.sessions()[self.target]
        self.request("/mock/session_close?fail=1")
        with self.assertRaises(HTTPError) as caught:
            self.request(self.close, "POST")
        self.assertEqual(caught.exception.code, 503)
        caught.exception.close()
        self.assertEqual(self.sessions()[self.target], before)
        self.assertEqual(self.request("/mock/session_close")["closed"], [])

    def test_unknown_agent_is_not_closed(self):
        with self.assertRaises(HTTPError) as caught:
            self.request("/api/v1/sessions/macbook/missing/close", "POST")
        self.assertEqual(caught.exception.code, 404)
        caught.exception.close()
        self.assertEqual(self.request("/mock/session_close")["closed"], [])

    def test_reset_restores_activity_after_live_updates(self):
        before = self.sessions()
        self.request("/mock/churn?n=2&ms=1&steps=0")
        deadline = time.monotonic() + 5
        while self.request("/mock/churned").get("rounds", 0) < 2:
            self.assertLess(time.monotonic(), deadline, "live updates did not finish")
            time.sleep(0.05)
        widget = "a1f3c2d4-0000-4000-8000-000000000001"
        self.assertNotEqual(self.sessions()[widget]["updated_at"], before[widget]["updated_at"])
        self.request("/mock/reset")
        after = self.sessions()
        for sid, session in before.items():
            self.assertEqual(after[sid]["updated_at"], session["updated_at"])
            self.assertEqual(after[sid].get("last_text"), session.get("last_text"))


if __name__ == "__main__":
    unittest.main()
