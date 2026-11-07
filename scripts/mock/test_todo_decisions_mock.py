"""Tests mock_hub's to-do decision routes (#1618): the fixture, recording, revising and undoing, as the hub answers."""
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

DECISIONS = "/api/v1/todos/todo_weather/decisions"


class MockHub:
    def __init__(self, **env):
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        self.base = f"http://127.0.0.1:{port}"
        self.log = tempfile.TemporaryFile()
        self.process = subprocess.Popen(
            ["python3", "-I", str(Path(__file__).with_name("mock_hub.py"))],
            env=dict(os.environ, PORT=str(port), **env),
            stdout=self.log,
            stderr=self.log,
        )
        for _ in range(60):
            if self.process.poll() is not None:
                raise RuntimeError("the mock hub exited")
            try:
                with urlopen(self.base + "/api/v1/health", timeout=1):
                    return
            except (URLError, TimeoutError):
                time.sleep(0.1)
        raise RuntimeError("the mock hub did not start")

    def stop(self):
        self.process.terminate()
        try:
            self.process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            self.process.kill()
            self.process.wait(timeout=5)
        self.log.close()

    def request(self, path, method="GET", body=None):
        headers = {"Content-Type": "application/json", "Authorization": "Bearer mock"}
        data = json.dumps(body).encode() if body is not None else None
        with urlopen(Request(self.base + path, data=data, method=method, headers=headers), timeout=3) as response:
            return json.load(response)

    def refused(self, path, method="GET", body=None):
        try:
            self.request(path, method, body)
        except HTTPError as e:
            return e.code, json.load(e)
        raise AssertionError(f"{method} {path} was not refused")


class TodoDecisionsMockTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.hub = MockHub(MOCK_TODO_DECISIONS="1")
        cls.addClassCleanup(cls.hub.stop)

    def setUp(self):
        self.hub.request("/mock/reset")

    def weather(self):
        return next(t for t in self.hub.request("/api/v1/todos")["todos"] if t["id"] == "todo_weather")

    def test_the_fixture_has_a_revised_decision(self):
        every = self.hub.request(DECISIONS + "?all=1")["decisions"]
        self.assertEqual([d["id"] for d in every], ["dec_size_small", "dec_tests", "dec_size_medium"])
        self.assertEqual(every[0]["superseded_by"], "dec_size_medium")
        self.assertEqual(every[2]["supersedes_id"], "dec_size_small")
        self.assertEqual((every[1]["source"], every[1]["source_machine"]), ("card", "macbook"))
        current = self.hub.request(DECISIONS)["decisions"]
        self.assertEqual([d["id"] for d in current], ["dec_tests", "dec_size_medium"])
        self.assertEqual(self.hub.request("/api/v1/todos/todo_blog/decisions?all=1"), {"decisions": []})

    def test_recording_one_that_revises_another(self):
        before = self.weather()["updated_at"]
        time.sleep(0.01)
        d = self.hub.request(DECISIONS, "POST", {"question": " 单位？ ", "choice": "摄氏度", "supersedes_id": "dec_tests"})["decision"]
        self.assertEqual((d["question"], d["choice"], d["reason"], d["supersedes_id"]), ("单位？", "摄氏度", "", "dec_tests"))
        every = {x["id"]: x for x in self.hub.request(DECISIONS + "?all=1")["decisions"]}
        self.assertEqual(every["dec_tests"]["superseded_by"], d["id"])
        self.assertNotEqual(self.weather()["updated_at"], before, "clients read the decisions again when the to-do moves")

    def test_marking_and_undoing_a_revision(self):
        self.hub.request(DECISIONS + "/dec_size_medium", "PATCH", {"supersedes_id": ""})
        every = self.hub.request(DECISIONS + "?all=1")["decisions"]
        self.assertTrue(all(not d["superseded_by"] for d in every))
        self.hub.request(DECISIONS + "/dec_size_medium", "PATCH", {"supersedes_id": "dec_tests"})
        every = {x["id"]: x for x in self.hub.request(DECISIONS + "?all=1")["decisions"]}
        self.assertEqual(every["dec_tests"]["superseded_by"], "dec_size_medium")

    def test_refusals(self):
        self.assertEqual(self.hub.refused(DECISIONS, "POST", {"question": "q", "choice": " "})[0], 400)
        self.assertEqual(self.hub.refused(DECISIONS + "/dec_tests", "PATCH", {})[0], 400)
        later = self.hub.refused(DECISIONS + "/dec_tests", "PATCH", {"supersedes_id": "dec_size_medium"})
        self.assertEqual(later[0], 400, "only an earlier decision can be revised")
        self.assertEqual(self.hub.refused(DECISIONS + "/nope", "PATCH", {"supersedes_id": ""})[0], 404)
        code, body = self.hub.refused("/api/v1/todos/nope/decisions?all=1")
        self.assertEqual((code, body["code"], body["error"]), (404, "not_found", "todo not found"))


class LegacyHubDecisionsMockTests(unittest.TestCase):
    def test_a_hub_from_before_decisions_has_no_route(self):
        hub = MockHub(MOCK_LEGACY_HUB="1")
        self.addCleanup(hub.stop)
        code, body = hub.refused(DECISIONS + "?all=1")
        self.assertEqual((code, body["error"], body["code"]), (404, "Not Found", "not_found"))


if __name__ == "__main__":
    unittest.main()
