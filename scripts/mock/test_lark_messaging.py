"""The mock's messaging source (MOCK_MESSAGING=1, scripts/mock/lark_messaging.py, #2138) answers what the hub would let
through: every answer of the protocol's routes holds to schema/extension/messaging.schema.json, over the mock's Feishu
fixtures (the plain chats, MOCK_LARK's rich chat, the trainer's, brave_climb), and the extension's handling is done (words
decoded while the switch is on, a summary of words named while it is off).

    python3 scripts/mock/test_lark_messaging.py
"""
import importlib.util
import json
import sys
import unittest
from pathlib import Path
from urllib.request import Request, urlopen

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
from test_lark_mock import MockHub  # noqa: E402

_spec = importlib.util.spec_from_file_location("schema_tests", HERE.parent / "test" / "test_extension_schema.py")
_schema = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(_schema)
VALIDATOR = _schema.Validator(_schema.load_schemas())


def holds(definition, value):
    return VALIDATOR.errors({"$ref": "messaging.schema.json#/$defs/" + definition}, value, "messaging.schema.json")


class Hub(MockHub):
    def call(self, path, method="GET", body=None):
        req = Request(self.url(path), method=method, data=json.dumps(body).encode() if body is not None else None,
                      headers={"Authorization": "Bearer mock", "Content-Type": "application/json"})
        with urlopen(req, timeout=10) as r:
            return json.load(r)


class MessagingMockTests(unittest.TestCase):
    def hub(self, **env):
        hub = Hub(MOCK_MESSAGING="1", **env)
        self.addCleanup(hub.stop)
        return hub

    def check(self, hub, path, definition):
        answer = hub.call(path)
        self.assertEqual(holds(definition, answer), [], path)
        return answer

    def walk(self, hub):
        base = "/api/v1/messaging/lark/"
        self.assertEqual(holds("MessagingSources", hub.call("/api/v1/messaging")), [])
        self.check(hub, base + "status", "MessagingStatus")
        self.check(hub, base + "emojis", "MessagingEmojis")
        self.check(hub, base + "reactions/quick", "MessagingQuickReactions")
        self.check(hub, base + "pinned", "MessagingConversationPage")
        chats = self.check(hub, base + "conversations", "MessagingConversationPage")["conversations"]
        self.assertTrue(chats)
        for c in chats:
            page = self.check(hub, base + "conversations/%s/messages" % c["id"], "MessagingMessagePage")
            for m in page["messages"]:
                if m.get("thread"):
                    self.check(hub, base + "messages/%s/thread" % m["id"], "MessagingMessagePage")
        return chats

    def test_plain_chats(self):
        self.walk(self.hub())

    def test_rich_chat(self):
        hub = self.hub(MOCK_LARK="1")
        self.walk(hub)

    def test_trainer(self):
        hub = self.hub(MOCK_LARK_TRAINER="1")
        self.walk(hub)

    def test_brave_climb_is_the_extensions(self):
        hub = self.hub(MOCK_LARK_BRAVE_CLIMB="1")
        chats = self.walk(hub)
        self.assertEqual(next(c for c in chats if c["id"] == "f2")["preview"], "周五下午三点评审，带上新图标")
        page = hub.call("/api/v1/messaging/lark/conversations/f2/messages")
        texts = {m["id"]: m["text"] for m in page["messages"]}
        self.assertEqual(texts["m211"], "标签栏改粗一号")  # split over two runs
        self.assertEqual(texts["m212"], "结论：空状态先不改，下周再看")  # inside a sentence
        self.assertEqual(texts["m213"], "收到")  # the older four-character words
        self.assertIn({"text": "", "icon": "shield"}, next(m for m in page["messages"] if m["id"] == "m210")["decorations"])
        self.assertEqual(next(m for m in page["messages"] if m["id"] == "m214")["reply_to"]["summary"], "周五下午三点评审，带上新图标")
        self.assertIn("brave_climb", hub.call("/api/v1/messaging/lark/status")["modes"])
        # Off: the words stay as they came, a whole summary of them is named.
        hub.call("/api/v1/lark/message-display", "POST", {"account_id": "mock-lark-account", "key": "brave_climb_codec", "value": False, "expected": True})
        chats = hub.call("/api/v1/messaging/lark/conversations")["conversations"]
        self.assertEqual(next(c for c in chats if c["id"] == "f2")["preview"], "")
        page = hub.call("/api/v1/messaging/lark/conversations/f2/messages")
        self.assertTrue(next(m for m in page["messages"] if m["id"] == "m210")["text"].startswith("**字"))
        self.assertNotIn("brave_climb", hub.call("/api/v1/messaging/lark/status")["modes"])

    def test_send_in_a_mode(self):
        hub = self.hub(MOCK_LARK_BRAVE_CLIMB="1", MOCK_LARK_SEND_SECONDS="0")
        receipt = hub.call("/api/v1/messaging/lark/conversations/f2/messages", "POST",
                           {"client_id": "0b6d3c1e-7f2a-4a55-9d8e-2c1a4f5b6e7d", "text": "明天见", "mode": "brave_climb"})
        self.assertEqual(holds("MessagingSendReceipt", receipt), [])
        sent = hub.call("/api/v1/lark/conversations/f2/messages")["messages"][-1]
        self.assertTrue(sent["text"].startswith("**字"), sent["text"])
        mine = hub.call("/api/v1/messaging/lark/conversations/f2/messages")["messages"][-1]
        self.assertEqual(mine["text"], "明天见")


if __name__ == "__main__":
    unittest.main()
