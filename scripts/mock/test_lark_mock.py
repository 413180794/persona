"""The mock hub's Feishu chat lists decode in the app: every chat carries the keys the app's LarkConversation requires,
which the hub always sends (server/internal/lark/feed.go, no omitempty). 设置 › 翻译 › 飞书会话 failed with 「未能读取数据，
因为数据丢失。」 on the mock while they were missing."""
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import time
import unittest
from urllib.error import URLError
from urllib.request import urlopen

# LarkConversation's non-optional properties (apple/Persona/Views/Inbox/Lark/LarkModels.swift), by their JSON keys.
REQUIRED = {"id", "name", "is_group", "unread", "at_me", "muted", "preview", "is_mine"}


class MockHub:
    def __init__(self, **env):
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            self.port = sock.getsockname()[1]
        self.log = tempfile.TemporaryFile()
        self.process = subprocess.Popen(["python3", "-I", str(Path(__file__).with_name("mock_hub.py"))],
                                        env=dict(os.environ, PORT=str(self.port), **env), stdout=self.log, stderr=self.log)
        for _ in range(60):
            if self.process.poll() is not None:
                raise RuntimeError("mock hub exited before startup")
            try:
                with urlopen(self.url("/api/v1/health"), timeout=1):
                    return
            except (URLError, TimeoutError):
                time.sleep(0.1)
        raise RuntimeError("mock hub startup timed out")

    def url(self, path):
        return f"http://127.0.0.1:{self.port}{path}"

    def get(self, path):
        with urlopen(self.url(path), timeout=3) as response:
            return json.load(response)

    def stop(self):
        self.process.terminate()
        try:
            self.process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            self.process.kill()
            self.process.wait(timeout=5)
        self.log.close()


class LarkChatListTests(unittest.TestCase):
    def check(self, **env):
        hub = MockHub(**env)
        self.addCleanup(hub.stop)
        chats = hub.get("/api/v1/lark/conversations?scope=chats&limit=60")["conversations"]
        chats += hub.get("/api/v1/lark/pinned")["conversations"]
        self.assertTrue(chats)
        for chat in chats:
            self.assertEqual(REQUIRED - chat.keys(), set(), chat.get("name"))

    def test_the_default_chats_decode(self):
        self.check()

    def test_the_feishu_page_chats_decode(self):
        self.check(MOCK_LARK="1")


class LarkDeliveryProofTests(unittest.TestCase):
    def test_exact_message_read_preserves_identity_and_ownership(self):
        hub = MockHub()
        self.addCleanup(hub.stop)
        messages = hub.get("/api/v1/lark/conversations/f2/messages")["messages"]
        for mine in (False, True):
            message = next(m for m in messages if m["is_mine"] == mine)
            proof = hub.get("/api/v1/lark/raw/messages/" + message["id"])["item"]
            self.assertEqual(proof, {"id": message["id"], "feedId": "f2", "isMine": mine, "localState": "synced"})


class LarkMeasuringTests(unittest.TestCase):
    """The switches docs/testing.md「卡顿测量」 measures the Feishu pages with (#674)."""

    def test_rich_history_has_cards_pictures_and_posts(self):
        hub = MockHub(MOCK_LARK_RICH="1")
        self.addCleanup(hub.stop)
        kinds = {m["type"] for m in hub.get("/api/v1/lark/conversations/f3/messages?around=300")["messages"]}
        self.assertTrue({"CARD", "IMAGE", "POST", "TEXT"} <= kinds, kinds)

    def test_lark_churn_adds_messages_round_by_round(self):
        hub = MockHub()
        self.addCleanup(hub.stop)
        before = hub.get("/api/v1/lark/conversations/f3/messages")["messages"][-1]["position"]
        hub.get("/mock/lark_churn?feed=f3&n=3&ms=50&new=1")
        for _ in range(50):
            if hub.get("/mock/lark_churned")["rounds"] == 3:
                break
            time.sleep(0.05)
        self.assertEqual(hub.get("/mock/lark_churned")["rounds"], 3)
        after = hub.get("/api/v1/lark/conversations/f3/messages")["messages"][-1]["position"]
        self.assertEqual(int(after), int(before) + 3)


if __name__ == "__main__":
    unittest.main()
