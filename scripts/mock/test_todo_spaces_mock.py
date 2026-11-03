"""测试 mock_hub 对待办整组挂空间 (todo spaces) 相关接口与字段的支持。"""
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import time
import unittest
from urllib.error import HTTPError, URLError
from urllib.parse import quote
from urllib.request import Request, urlopen


class TodoSpacesMockTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        cls.base = f"http://127.0.0.1:{port}"
        cls.log = tempfile.TemporaryFile()
        cls.process = subprocess.Popen(
            ["python3", "-I", str(Path(__file__).with_name("mock_hub.py"))],
            env=dict(os.environ, PORT=str(port)),
            stdout=cls.log,
            stderr=cls.log,
        )
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

    def request(self, path, method="GET", role="full"):
        headers = {"Content-Type": "application/json"}
        if role == "readonly":
            headers["X-Mock-Role"] = "readonly"
        else:
            headers["Authorization"] = "Bearer mock"
        req = Request(self.base + path, method=method, headers=headers)
        with urlopen(req, timeout=3) as response:
            return json.load(response)

    def setUp(self):
        self.request("/mock/reset")

    def test_get_todos_includes_spaces_and_sources(self):
        data = self.request("/api/v1/todos")
        todos = data["todos"]
        weather = next(t for t in todos if t["id"] == "todo_weather")
        self.assertIn("spaces", weather)
        self.assertEqual(len(weather["spaces"]), 2)
        self.assertEqual(weather["spaces"][0]["space"], "weather")
        self.assertEqual(weather["spaces"][0]["machine_id"], "macbook")
        self.assertEqual(weather["spaces"][1]["space"], "docs")
        self.assertEqual(weather["spaces"][1]["machine_id"], "studio")

        # 检查 agents 的 source 和 via 字段
        agents = {a["session_id"]: a for a in weather["agents"]}
        # WIDGET & TESTS: source=space
        self.assertEqual(agents["a1f3c2d4-0000-4000-8000-000000000001"]["source"], "space")
        self.assertEqual(agents["a1f3c2d4-0000-4000-8000-000000000001"]["via"], "macbook/weather")
        # RELEASE: source=spawned
        self.assertEqual(agents["a1f3c2d4-0000-4000-8000-000000000008"]["source"], "spawned")
        self.assertEqual(agents["a1f3c2d4-0000-4000-8000-000000000008"]["via"], "macbook/a1f3c2d4-0000-4000-8000-000000000001")

    def test_put_and_delete_space(self):
        # 把 studio 的 blog 工作区整组挂到 todo_blog
        space_name = quote("blog")
        res = self.request(f"/api/v1/todos/todo_blog/spaces/studio/{space_name}", method="PUT")
        self.assertTrue(res["ok"])
        self.assertGreaterEqual(res["linked"], 1)

        # 验证 GET /todos 中已包含该 space
        data = self.request("/api/v1/todos")
        blog_todo = next(t for t in data["todos"] if t["id"] == "todo_blog")
        self.assertTrue(any(sp["machine_id"] == "studio" and sp["space"] == "blog" for sp in blog_todo["spaces"]))
        blog_agent = next(a for a in blog_todo["agents"] if a["session_id"] == "a1f3c2d4-0000-4000-8000-000000000003")
        self.assertEqual(blog_agent["source"], "space")
        self.assertEqual(blog_agent["via"], "studio/blog")

        # 移出 space
        del_res = self.request(f"/api/v1/todos/todo_blog/spaces/studio/{space_name}", method="DELETE")
        self.assertTrue(del_res["ok"])

        # 验证 GET /todos 中该 space 及随项目挂上的 agent 已被移除
        data2 = self.request("/api/v1/todos")
        blog_todo2 = next(t for t in data2["todos"] if t["id"] == "todo_blog")
        self.assertFalse(any(sp["machine_id"] == "studio" and sp["space"] == "blog" for sp in blog_todo2["spaces"]))
        self.assertFalse(any(a["session_id"] == "a1f3c2d4-0000-4000-8000-000000000003" for a in blog_todo2["agents"]))

    def test_put_space_conflict_and_permissions(self):
        # 只读角色
        with self.assertRaises(HTTPError) as cm:
            self.request("/api/v1/todos/todo_weather/spaces/macbook/weather", method="PUT", role="readonly")
        self.assertEqual(cm.exception.code, 403)

        # 待办不存在
        with self.assertRaises(HTTPError) as cm:
            self.request("/api/v1/todos/nonexistent/spaces/macbook/weather", method="PUT")
        self.assertEqual(cm.exception.code, 404)

        # 待办已完成 409
        with self.assertRaises(HTTPError) as cm:
            self.request("/api/v1/todos/todo_done_1/spaces/macbook/weather", method="PUT")
        self.assertEqual(cm.exception.code, 409)


if __name__ == "__main__":
    unittest.main()
