"""PhotoSaveTests' conversation with a screenshot: GET /mock/photo puts it in and takes it out on a mock started the
usual way, and MOCK_PHOTO=1 still has it from the start (#2334)."""
import socket
import unittest
from test_mock_hub_mesh import Mock

PHOTO = "a1f3c2d4-0000-4000-8000-00000000000c"


def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


class PhotoMockTests(unittest.TestCase):
    def start(self, **env):
        self.hub = Mock(free_port(), **env)
        self.addCleanup(self.hub.stop)

    def listed(self):
        return [s["session_id"] for s in self.hub.get("/api/v1/sessions")["sessions"]].count(PHOTO)

    def items(self):
        return self.hub.get(f"/api/v1/sessions/macbook/{PHOTO}/items")["items"]

    def test_the_usual_mock_gets_the_screenshot_from_the_test(self):
        self.start()
        self.assertEqual(self.listed(), 0)
        self.assertEqual(self.items(), [])
        self.assertEqual(self.hub.get("/mock/photo?on=1"), {"ok": True, "photo": True, "was": False})
        self.assertEqual(self.listed(), 1)
        first = self.items()
        self.assertEqual([x["kind"] for x in first], ["text", "image", "text"])
        self.assertEqual(first[1]["image"]["mime"], "image/png")
        self.assertEqual(self.hub.get("/mock/photo?on=1")["was"], True)  # twice: still one conversation
        self.assertEqual(self.listed(), 1)
        self.assertEqual(self.hub.get("/mock/photo?on=0"), {"ok": True, "photo": False, "was": True})
        self.assertEqual(self.listed(), 0)
        self.assertEqual(self.items(), [])
        self.hub.get("/mock/photo?on=1")
        self.assertEqual(self.items(), first)  # the same items and seqs as the first time

    def test_mock_photo_at_start_still_has_it(self):
        self.start(MOCK_PHOTO="1")
        self.assertEqual(self.listed(), 1)
        self.assertEqual(self.hub.get("/mock/photo?on=1"), {"ok": True, "photo": True, "was": True})
        self.assertEqual(self.listed(), 1)


if __name__ == "__main__":
    unittest.main()
