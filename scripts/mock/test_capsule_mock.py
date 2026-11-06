"""Exercise the overlay's extension slot and action through an isolated HTTP mock."""
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import time
import unittest
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen


class CapsuleMockTests(unittest.TestCase):
    def start(self, plugins):
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        base = "http://127.0.0.1:%d" % port
        log = tempfile.TemporaryFile()
        env = {k: v for k, v in os.environ.items() if not k.startswith("MOCK_")}
        if plugins:
            env["MOCK_EXTENSIONS"] = "1"
        process = subprocess.Popen([sys.executable, "-I", str(Path(__file__).with_name("mock_hub.py"))],
                                   env=dict(env, PORT=str(port)), stdout=log, stderr=log)

        def stop():
            process.terminate()
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=5)
            log.close()
        self.addCleanup(stop)
        for _ in range(100):
            if process.poll() is not None:
                log.seek(0)
                self.fail("mock exited: " + log.read().decode(errors="replace"))
            try:
                self.request(base, "/api/v1/health")
                return base
            except (URLError, TimeoutError):
                time.sleep(0.1)
        self.fail("mock startup timed out")

    def request(self, base, path, body=None):
        request = Request(base + path, data=None if body is None else json.dumps(body).encode(),
                          method="GET" if body is None else "POST", headers={"Content-Type": "application/json"})
        with urlopen(request, timeout=3) as response:
            return json.load(response)

    def test_capsule_is_explicit_and_action_updates_its_view(self):
        base = self.start(plugins=True)
        path = "/api/v1/extensions/-/ui"
        cards = self.request(base, path + "?placement=capsule_card")["ui"]
        self.assertEqual(len(cards), 1)
        card = cards[0]
        self.assertEqual((card["extension"], card["id"], card["size"], card["opens"]),
                         ("example-gateway", "capsule", "compact", "usage"))
        # Existing aggregate slots and the settings' overview card are unchanged.
        self.assertNotIn("capsule_card", [p["placement"] for p in self.request(base, path)["ui"]])
        overview = self.request(base, path + "?placement=overview_card")["ui"]
        self.assertEqual([p["id"] for p in overview], ["today"])
        endpoint = "/api/v1/extensions/example-gateway/-/ui/capsule"
        context = {"platform": "android", "size": "compact", "ui_version": 3}
        view = self.request(base, endpoint, {"context": context})["view"]
        self.assertIn("扩展卡片已接入", json.dumps(view, ensure_ascii=False))
        self.assertEqual(view["state"], "refreshed=0")
        result = self.request(base, endpoint, {"context": context, "action": "capsule:refresh", "state": view["state"]})
        self.assertEqual(result["toast"], "摘要已刷新")
        self.assertEqual(result["view"]["state"], "refreshed=1")
        self.assertIn("1 次", json.dumps(result["view"], ensure_ascii=False))

    def test_capsule_stays_unavailable_without_plugins(self):
        base = self.start(plugins=False)
        with self.assertRaises(HTTPError) as error:
            self.request(base, "/api/v1/extensions/-/ui?placement=capsule_card")
        self.assertEqual(error.exception.code, 404)


if __name__ == "__main__":
    unittest.main()
