"""Opt-in synthetic Pin-message scenarios; never uses an upstream account."""
import copy
import json
import re


class ChatPinsFixture:
    def __init__(self, messages, threads, window, account):
        self.messages, self.threads, self.window = messages, threads, window
        self.account = account
        self.reset()

    def reset(self):
        self.state = dict(account=self.account, offline=False, error="", fail_page=False,
                          locate_fail=False, unknown_write=False, reads=0, writes=0)
        self.rows = []
        self.messages["f2"] = [self.message(200 + i, "评审记录 %d：这个方案需要保留。" % (i + 1)) for i in range(4)]
        self.populate(8)

    def message(self, pos, text, **kw):
        out = dict(id="m%d" % pos, position=str(pos), sender_id="pin-reader", sender_name="示例成员",
                   is_mine=False, type="TEXT", text=text, recalled=False, reply_count=0,
                   can_pin=True, can_unpin=True, created_at="2026-01-01T10:00:00Z")
        out.update(kw)
        return out

    def populate(self, count):
        self.rows = []
        for i in range(count):
            m = copy.deepcopy(self.messages["f2"][i % 4])
            if i == 1:
                m.update(text="公开展示", olk=dict(version=1, state="decrypted",
                    publicParts=[dict(kind="text", text="公开展示")], bodyParts=[dict(kind="text", text="仅持有密钥的人可见的示例正文")]))
            elif i == 2:
                m.update(recalled=True, text="撤回后不应显示的示例正文", can_pin=False)
            elif i == 3:
                m.update(can_unpin=False, text="只读 Pin：没有移除权限")
            elif i == 4:
                m = None
            elif i == 5:
                m = self.message(250, "话题中的被 Pin 回复", thread_id="m200", thread_kind="reply")
                self.threads["m200"] = [m]
            elif i > 5:
                m = self.message(400 + i, "收藏的消息 %d" % (i + 1))
            self.rows.append(dict(id=str(1000 + i), message_id=m["id"] if m else "missing", message=m,
                                 created_at="2026-01-01T10:00:00Z"))

    def route(self, h, p, q=None, broadcast=lambda: None):
        q = q or {}
        state_path = "/test/lark-chat-pins/state"
        match = re.fullmatch(r"/api/v1/lark/conversations/([^/]+)/(pins|pin-messages/([^/]+))", p)
        if p != state_path and not match:
            return False
        def reply(body, status=200):
            h.send_json(body, status)
            return True
        def fail(code, text, status=502):
            return reply(dict(code=code, error=text), status)
        if p == state_path:
            if h.command == "POST":
                req = json.loads(h.last_body or b"{}")
                if req.get("reset"): self.reset()
                for key in self.state:
                    if key in req: self.state[key] = req[key]
                if "count" in req: self.populate(req["count"])
                if req.get("live", True): broadcast()
            return reply(dict(self.state, count=len(self.rows)))
        if self.state["offline"]: return fail("lark_pins_unavailable", "暂时无法加载 Pin 消息，请稍后重试", 503)
        if self.state["error"] == "hub": return reply(dict(error="not found"), 404)
        if self.state["error"] == "upstream": return fail("lark_gateway_upgrade_required", "升级飞书网关后可使用 Pin 消息。请在 Hub 连接的飞书网关中检查更新。", 426)
        if self.state["error"] == "forbidden": return fail("lark_pin_forbidden", "没有权限查看或修改这条 Pin", 403)
        body = json.loads(h.last_body or b"{}") if h.command == "POST" else {}
        owner = body.get("account_id") or q.get("account_id", [self.state["account"]])[0]
        if owner != self.state["account"]: return fail("lark_account_changed", "飞书账号已切换，请重新打开会话", 409)
        if h.command == "POST":
            self.state["writes"] += 1
            if body.get("remove"):
                self.rows = [p for p in self.rows if p["id"] != body.get("pin_id")]
            else:
                m = next(m for m in self.messages["f2"] if m["id"] == body.get("message_id"))
                self.rows.insert(0, dict(id="added", message_id=m["id"], message=m))
            if self.state["unknown_write"]: return fail("lark_pin_result_unknown", "尚未确认操作结果，请重新加载 Pin 列表查看")
            return reply(dict(account_id=owner, pinned=not body.get("remove")))
        if match[3]:
            if self.state["locate_fail"]: return fail("lark_pin_no_position", "暂时无法定位这条消息，当前聊天位置已保留", 409)
            m = next((p["message"] for p in self.rows if p["message_id"] == match[3]), None)
            if m is None: return fail("lark_pin_not_found", "原消息已不可用，请重新加载 Pin 列表", 404)
            messages = self.messages["f2"]
            root = None
            if m.get("thread_id"):
                root = messages[0]
                messages = self.threads["m200"]
            target = dict(message=m, page=self.window(messages, {}))
            if root: target["root"] = root
            return reply(dict(account_id=owner, target=target))
        self.state["reads"] += 1
        cursor = int(q.get("cursor", ["0"])[0] or 0)
        if cursor and self.state["fail_page"]: return fail("lark_pins_unavailable", "暂时无法加载 Pin 消息，请稍后重试")
        limit = 4  # Exercise pagination without forcing dozens of swipes.
        return reply(dict(account_id=owner, items=self.rows[cursor:cursor+limit], total=len(self.rows),
                          next_cursor=str(cursor+limit) if cursor+limit < len(self.rows) else ""))
