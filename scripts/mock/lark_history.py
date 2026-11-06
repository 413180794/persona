"""Opt-in history pagination fixture. All accounts and messages are synthetic."""
import copy
import json
import re
import time


class HistoryFixture:
    def __init__(self, account, window, push, chats=None, ago=None):
        self.account, self.window, self.push = account, window, push
        # "Back to latest" (#1034): a chat opened at its first unread message. Every reset gives it a new id, so no test finds
        # the newest messages of an earlier one in the app's cache.
        self.chats, self.ago, self.jumps = chats if chats is not None else [], ago, 0
        # The ids must not come back after a restart either: the app keeps a chat's newest messages by its id.
        self.epoch = int(time.time())
        self.reset()

    def reset(self):
        self.jumps += 1
        self.state = dict(account=self.account, hold=False, fail=False, duplicate=False,
                          empty=False, cycle=False, legacy=False, short=False, calls=[], total=48, completed=0, latest=0, topic=False,
                          hold_latest=False, unread=True, jump_feed="fj%d%04d" % (self.epoch, self.jumps))
        self.jump_chat()

    def jump_chat(self):
        self.chats[:] = [c for c in self.chats if not c["id"].startswith("fj")]
        unread = self.state["unread"]
        c = {"id": self.state["jump_feed"], "is_mine": False, "name": "跳转测试", "is_group": True,
             "unread": 28 if unread else 0, "at_me": False, "muted": False, "preview": "历史记录 48：保留当前正在阅读的位置。"}
        if unread: c["read_position"] = "20"
        if self.ago: c["updated_at"] = self.ago(minutes=1)
        self.chats.insert(0, c)

    @staticmethod
    def message(i):
        return dict(id="history-%d" % i, position=str(i), sender_id="example", sender_name="示例成员",
                    is_mine=False, type="TEXT", text="历史记录 %d：保留当前正在阅读的位置。" % i
                    # Uneven heights, so the list's estimates for rows above are off like in a real chat.
                    + ("这一条比较长，会换成好几行显示，用来检验往上续页时高度估算不准也不会让正在读的消息跳动。" if i % 6 == 2 else ""),
                    recalled=False, reply_count=0, created_at="2026-01-01T10:00:00Z")

    def route(self, h, path, query):
        if path == "/test/lark-history/state":
            if h.command == "POST":
                change = json.loads(h.last_body or b"{}")
                if change.get("reset"): self.reset()
                self.state.update({k: v for k, v in change.items() if k in self.state and k != "jump_feed"})
                if "unread" in change: self.jump_chat()
                # A message arriving while history is read: one more at the newest end, pushed like the Lark gateway's events.
                if change.get("arrive"):
                    self.state["total"] += int(change["arrive"])
                    self.push("f1"); self.push("f2"); self.push(self.state["jump_feed"])
            h.send_json(copy.deepcopy(self.state)); return True
        if path == "/api/v1/lark/raw/accounts":
            h.send_json(dict(activeId="1", accounts=[dict(id="1", signedIn=True, userId=self.state["account"])])); return True
        match = re.fullmatch(r"/api/v1/lark/conversations/(f1|f2|fj\d+)/messages", path)
        if not match or h.command != "GET": return False
        q = {**query, "limit": ["2" if self.state["short"] else "20"]}
        before = (query.get("before") or [""])[0]
        owner = (query.get("account_id") or [""])[0]
        newest = not any((query.get(k) or [""])[0] for k in ("before", "after", "around"))
        if newest:
            deadline = time.monotonic() + 60
            while self.state["hold_latest"] and time.monotonic() < deadline: time.sleep(.03)
        if before:
            self.state["calls"].append(before)
            deadline = time.monotonic() + 20
            while self.state["hold"] and time.monotonic() < deadline: time.sleep(.03)
            if self.state["fail"]:
                h.send_json(dict(error="synthetic offline"), 503); return True
        if owner and owner != self.state["account"]:
            h.send_json(dict(code="lark_account_changed", error="飞书账号已切换"), 409); return True
        rows = [self.message(i) for i in range(1, self.state["total"] + 1)]
        page = self.window(rows, q)
        if before and self.state["duplicate"]:
            page.update(messages=[self.message(int(before))], has_older=True, older_cursor=before)
        if before and self.state["empty"]: page.update(messages=[], has_older=True)
        if before and self.state["cycle"]: page.update(older_cursor=before)
        if not self.state["legacy"]: page["account_id"] = self.state["account"]
        # A topic group: the hub says so even when the request did not, and its history stays a button.
        if self.state["topic"]: page["topic"] = True
        if before: self.state["completed"] += 1
        if newest: self.state["latest"] += 1
        h.send_json(page); return True
