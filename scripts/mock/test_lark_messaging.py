"""Protocol regression tests for the local mock messaging source."""
import importlib.util
import json
import sys
import unittest
from pathlib import Path
from urllib.request import Request, urlopen

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
from test_lark_mock import MockHub  # noqa: E402
import lark_messaging  # noqa: E402

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





    def test_topic_cards_keep_their_body(self):
        # A card is drawn from its view as the extension turns it (cardView): a release note's list, its columns.
        hub = self.hub(MOCK_LARK_TOPIC="1")
        page = self.check(hub, "/api/v1/messaging/lark/conversations/f4/messages?topic=1", "MessagingMessagePage")
        cards = {m["id"]: m["parts"][0]["view"]["sections"][0] for m in page["messages"] if m["parts"] and m["parts"][0]["kind"] == "card"}
        notes = [i["text"] for i in cards["m402"]["items"] if i["type"] == "text"]
        self.assertEqual(cards["m402"]["title"], "v2.4 发版说明")
        self.assertIn("• 安装包小了 8 MB", notes)
        self.assertIn("新功能 15 · 修复 7 · 其他 2", notes)
        self.assertEqual([i["text"] for i in cards["m403"]["items"]], ["崩溃率 0.02% · 活跃 1.2 万 · 反馈 3"])

    def test_card_view_matches_the_extension(self):
        # The input and lines of the Lark extension's TestMessagingCardViewKeepsListsTablesPanelsFields.
        pt = lambda s: {"tag": "plain_text", "text": s}  # noqa: E731
        md = lambda s: {"tag": "markdown", "children": [pt(s)]}  # noqa: E731
        item = lambda typ, level, s: {"type": typ, "level": level, "children": [pt(s)]}  # noqa: E731
        meta = {"header": {"tag": "card_header", "children": [pt("v2.4 发版说明")]}, "body": {"tag": "body", "children": [
            md("本次更新 3 项"),
            {"tag": "list", "items": [item("ul", 0, "深色模式"), item("ul", 1, "图表配色"), item("ol", 0, "相册"), item("ol", 0, "搜索")]},
            {"tag": "markdown_table", "children": [{"tag": "markdown_table_row", "children": [
                {"tag": "markdown_table_cell", "children": [pt("新功能")]}, {"tag": "markdown_table_cell", "children": [pt("15")]}]}]},
            {"tag": "collapsible_panel", "header": md("更多"), "children": [md("折叠的内容")]},
            {"tag": "div", "children": [pt("负责人")], "fields": [{"short": True, "children": [pt("Mia")]}]},
            {"tag": "column_set", "children": [{"tag": "column", "children": [md("崩溃率"), md("0.02%")]},
                                               {"tag": "column", "children": [md("活跃"), md("1.2 万")]}]}]}}
        view, words = lark_messaging.card_view(meta, "v2.4 发版说明")
        self.assertEqual(words, "v2.4 发版说明")
        self.assertEqual([i["text"] for i in view["sections"][0]["items"]],
                         ["本次更新 3 项", "• 深色模式", "◦ 图表配色", "1. 相册", "2. 搜索", "新功能 | 15", "更多", "折叠的内容", "负责人", "Mia",
                          "崩溃率 0.02% · 活跃 1.2 万"])


if __name__ == "__main__":
    unittest.main()
