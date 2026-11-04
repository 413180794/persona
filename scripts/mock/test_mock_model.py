"""The made-up model answers the hub's inbox prompts with deterministic JSON, and switches modes for a test."""
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

GROUP_SYSTEM = "你是用户（称呼：你）的私人助理，替用户把一个会话里新来的消息归到「事件」里，让用户一件事看一行。"
GROUP_USER = ("会话：chat:F1\n要归类的新消息：m1060、m1061\n\n10:05 丙：周四下午可以吗？ (m1060)\n10:06 丁：接口改了 (m1061)\n\n"
              "这个会话里现在的事（events 不带 query 的结果）：\n"
              '[{"id":"lark:F1:1051","title":"示例","version":"v","done":false,"meta":null}]\n\n还能查 4 次。')


class MockModelTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        cls.base = f"http://127.0.0.1:{port}"
        cls.log = tempfile.TemporaryFile()
        cls.process = subprocess.Popen(["python3", "-I", str(Path(__file__).with_name("mock_model.py"))],
                                       env=dict(os.environ, PORT=str(port)), stdout=cls.log, stderr=cls.log)
        cls.addClassCleanup(cls.stop)
        for _ in range(60):
            if cls.process.poll() is not None:
                raise RuntimeError("mock model exited before startup")
            try:
                with urlopen(cls.base + "/_calls", timeout=1):
                    return
            except (URLError, TimeoutError):
                time.sleep(0.1)
        raise RuntimeError("mock model startup timed out")

    @classmethod
    def stop(cls):
        cls.process.terminate()
        try:
            cls.process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            cls.process.kill()
        cls.log.close()

    def post(self, path, body):
        req = Request(self.base + path, data=json.dumps(body).encode(), headers={"content-type": "application/json"})
        with urlopen(req, timeout=5) as resp:
            return json.loads(resp.read())

    def ask(self, system, user):
        out = self.post("/v1/messages", {"model": "fake", "max_tokens": 100, "system": system,
                                         "messages": [{"role": "user", "content": user}]})
        return json.loads(out["content"][0]["text"])

    def setUp(self):
        self.post("/_mode", {"mode": "ok"})

    def test_grouping_puts_the_batch_in_the_first_open_event(self):
        got = self.ask(GROUP_SYSTEM, GROUP_USER)
        self.assertEqual(got["assign"], [{"message": "m1060", "event": "lark:F1:1051"}, {"message": "m1061", "event": "lark:F1:1051"}])
        self.assertEqual(got["events"][0]["tier"], "action")
        self.assertEqual(got["events"][0]["title"], "模拟：m1060")

    def test_grouping_without_events_opens_one(self):
        got = self.ask(GROUP_SYSTEM, GROUP_USER.replace('"done":false', '"done":true'))
        self.assertEqual({a["event"] for a in got["assign"]}, {"new:1"})

    def test_bad_answer_leaves_a_message_out(self):
        self.post("/_mode", {"mode": "bad"})
        got = self.ask(GROUP_SYSTEM, GROUP_USER)
        self.assertEqual([a["message"] for a in got["assign"]], ["m1060"])

    def test_triage_and_judge(self):
        self.assertEqual(self.ask("你是用户的私人助理，替用户分拣收到的工作消息", "张三：明天能上线吗？")["tier"], "action")
        self.assertEqual(self.ask("你是用户的私人助理，替用户分拣收到的工作消息", "群里同步：已上线")["tier"], "fyi")
        self.assertFalse(self.ask("你是用户（称呼：你）的 AI 分身的调度员。", "x")["handle"])

    def test_fail_and_quota(self):
        for mode, code in (("fail", 500), ("quota", 429)):
            self.post("/_mode", {"mode": mode})
            with self.assertRaises(HTTPError) as err:
                self.ask(GROUP_SYSTEM, GROUP_USER)
            self.assertEqual(err.exception.code, code)

    def test_calls_are_kept(self):
        self.ask(GROUP_SYSTEM, GROUP_USER)
        with urlopen(self.base + "/_calls", timeout=5) as resp:
            calls = json.loads(resp.read())["calls"]
        self.assertEqual(calls[-1]["kind"], "group")


if __name__ == "__main__":
    unittest.main()
