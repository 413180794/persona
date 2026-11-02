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

# LarkConversation's non-optional properties (apple/Jarvis/Views/Inbox/Lark/LarkModels.swift), by their JSON keys.
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


if __name__ == "__main__":
    unittest.main()
