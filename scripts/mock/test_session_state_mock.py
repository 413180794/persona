"""Verify targeted completion fixtures use real session snapshots and WebSocket pushes."""
import base64
import json
import os
import socket
import unittest
from urllib.parse import urlencode
from test_mock_hub_mesh import Mock


class SessionStateMockTests(unittest.TestCase):
    def setUp(self):
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        self.hub = Mock(port)
        self.addCleanup(self.hub.stop)
        self.mid = "macbook"
        self.sid = "a1f3c2d4-0000-4000-8000-000000000001"

    def sessions(self):
        return {(s["machine_id"], s["session_id"]): s for s in self.hub.get("/api/v1/sessions")["sessions"]}

    def state_path(self, **query):
        return "/mock/session_state?" + urlencode(query)

    def websocket(self):
        sock = socket.create_connection(("127.0.0.1", self.hub.port), timeout=3)
        stream = sock.makefile("rb")
        self.addCleanup(stream.close)
        self.addCleanup(sock.close)
        key = base64.b64encode(os.urandom(16)).decode()
        sock.sendall((f"GET /api/v1/ws HTTP/1.1\r\nHost: 127.0.0.1:{self.hub.port}\r\nUpgrade: websocket\r\n"
                      f"Connection: Upgrade\r\nSec-WebSocket-Key: {key}\r\nSec-WebSocket-Version: 13\r\n\r\n").encode())
        self.assertIn(b" 101 ", stream.readline())
        while stream.readline() not in (b"\r\n", b""):
            pass

        def receive():
            head = stream.read(2)
            self.assertEqual(len(head), 2)
            length = head[1] & 0x7F
            if length == 126:
                length = int.from_bytes(stream.read(2), "big")
            elif length == 127:
                length = int.from_bytes(stream.read(8), "big")
            self.assertEqual(head[0] & 0x0F, 1)
            return json.loads(stream.read(length))
        self.assertEqual(receive()["type"], "hello")
        return receive

    def test_transition_updates_only_target_and_pushes_real_sessions(self):
        before = self.sessions()
        target = (self.mid, self.sid)
        receive = self.websocket()
        path = self.state_path(mid=self.mid, sid=self.sid, state="unread")
        changed = self.hub.get(path)["session"]
        self.assertEqual((changed["state"], changed["status"]), ("unread", "done"))
        self.assertEqual(changed["rev"], before[target]["rev"] + 1)
        self.assertNotEqual(changed["updated_at"], before[target]["updated_at"])
        self.assertEqual(changed["state_since"], changed["updated_at"])
        while True:
            frame = receive()
            if frame.get("type") != "sessions" or frame.get("machine_id") != self.mid:
                continue
            pushed = next(s for s in frame["sessions"] if s["session_id"] == self.sid)
            if pushed["rev"] == changed["rev"]:
                break
        self.assertEqual(pushed, changed)
        after = self.sessions()
        self.assertEqual(after[target], changed)
        self.assertEqual({k: s for k, s in before.items() if k != target},
                         {k: s for k, s in after.items() if k != target})
        working = self.hub.get(self.state_path(mid=self.mid, sid=self.sid, state="working"))["session"]
        self.assertEqual((working["state"], working["status"], working["rev"]),
                         ("working", "working", changed["rev"] + 1))

    def test_invalid_parameters_and_wrong_machine_do_not_mutate(self):
        before = self.sessions()
        cases = [({"mid": self.mid, "sid": self.sid}, 400),
                 ({"mid": self.mid, "sid": self.sid, "state": "invalid"}, 400),
                 ({"mid": self.mid, "sid": "missing", "state": "unread"}, 404),
                 ({"mid": "server", "sid": self.sid, "state": "unread"}, 404),
                 ({"mid": self.mid, "sid": self.sid, "state": "unread", "extra": "1"}, 400)]
        for query, expected in cases:
            with self.subTest(query=query):
                status, body = self.hub.get_status(self.state_path(**query))
                self.assertEqual(status, expected)
                self.assertIn("error", body)
                self.assertEqual(self.sessions(), before)
        status, _ = self.hub.get_status(self.state_path(mid=self.mid, sid=self.sid, state="unread") + "&state=working")
        self.assertEqual(status, 400)
        self.assertEqual(self.sessions(), before)


if __name__ == "__main__":
    unittest.main()
