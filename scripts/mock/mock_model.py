"""A made-up model for end-to-end tests of the hub's inbox (triage, grouping, the AI twin's judge): an Anthropic
Messages endpoint (POST /v1/messages) that answers deterministic JSON, so a real hub can be pointed at it.

    PORT=18790 python3 scripts/mock/mock_model.py
    echo fake > /tmp/mock-model.key
    manager.json: "inbox": {"triage_ai": {"base_url": "http://127.0.0.1:18790", "api_key_file": "/tmp/mock-model.key", "model": "fake"},
                            "judge_ai":  {"base_url": "http://127.0.0.1:18790", "api_key_file": "/tmp/mock-model.key", "model": "fake"}}
    (or "ai": {…} for every purpose; the key is never checked). Grouping uses the triage model (inbox.triage).

What it answers, by the system prompt (server/internal/prompts/inbox/):
- grouping (group.md, matched by its Chinese heading): every new message of the batch into the first open event the input lists,
  else into new:1; title "<Chinese mock prefix> <first new message>", tier action when the batch's text has a question mark,
  else fyi. With MOCK_MODEL_GROUP_TOOL=1 it first asks messages_before for the first message (once per batch).
- triage (triage.md, matched by its Chinese heading): action when the item has a question mark or an @-me mention (in Chinese), else fyi.
- the judge (judge.md, matched by its Chinese heading): never hands over.
- translation (tools/translate_texts.md, translate_mail.md): each text, the subject and the body with the Chinese "[translated]" tag in front.
- anything else: "ok".

Control (for a test): POST /_mode {"mode": "ok" | "fail" | "quota" | "bad"} — fail answers HTTP 500, quota a 429
"account_quota_exhausted" (Retry-After: 1), bad a grouping answer that misses a message (the hub falls back);
GET /_calls lists the requests so far as {kind, system, user}. Only made-up data; nothing leaves the machine.
"""
import json
import os
import re
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from threading import Lock

STATE = {"mode": "ok", "calls": []}
LOCK = Lock()


def kind_of(system):
    if "归到「事件」里" in system:
        return "group"
    if "替用户分拣" in system:
        return "triage"
    if "AI 分身的调度员" in system:
        return "judge"
    if "你是翻译" in system:
        return "translate_mail" if "「主题：…」" in system else "translate_texts"
    return "other"


def new_messages(user):
    m = re.search(r"^要归类的新消息：(.*)$", user, re.M)
    return [x for x in (m.group(1).split("、") if m else []) if x]


def first_open_event(user):
    m = re.search(r"^这个会话里现在的事（events 不带 query 的结果）：\n(.*)$", user, re.M)
    if not m:
        return None
    try:
        events = json.loads(m.group(1))
    except ValueError:
        return None
    for e in events or []:
        if not e.get("done"):
            return e.get("id")
    return None


def answer(kind, user, mode):
    if kind == "group":
        msgs = new_messages(user)
        if os.environ.get("MOCK_MODEL_GROUP_TOOL") == "1" and "你调了" not in user and msgs:
            return {"tool": "messages_before", "args": {"message": msgs[0], "n": 5}}
        event = first_open_event(user) or "new:1"
        if mode == "bad":
            msgs = msgs[:-1]  # one message left out: the hub's check fails
        tier = "action" if re.search(r"[?？]", user) else "fyi"
        first = msgs[0] if msgs else "?"
        return {"assign": [{"message": m, "event": event} for m in msgs],
                "events": [{"event": event, "title": "模拟：" + first, "summary": "模拟的总结", "tier": tier, "reason": "模拟"}]}
    if kind == "triage":
        tier = "action" if re.search(r"[?？]|@我", user) else "fyi"
        return {"tier": tier, "summary": "模拟的摘要", "reason": "模拟"}
    if kind == "judge":
        return {"handle": False, "reason": "模拟", "task": "", "task_id": ""}
    if kind == "translate_texts":
        try:
            texts = json.loads(user).get("texts") or []
        except ValueError:
            texts = []
        return {"texts": ["【译】" + t for t in texts]}
    if kind == "translate_mail":
        head, _, body = user.partition("\n\n")
        return {"subject": "【译】" + head.removeprefix("主题："), "text": "【译】" + body}
    return None


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def send(self, code, body, headers=()):
        raw = json.dumps(body, ensure_ascii=False).encode()
        self.send_response(code)
        self.send_header("content-type", "application/json")
        for k, v in headers:
            self.send_header(k, v)
        self.send_header("content-length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def read(self):
        n = int(self.headers.get("content-length") or 0)
        try:
            return json.loads(self.rfile.read(n) or b"{}")
        except ValueError:
            return {}

    def do_GET(self):
        if self.path == "/_calls":
            with LOCK:
                return self.send(200, {"calls": STATE["calls"]})
        self.send(404, {"error": "not found"})

    def do_POST(self):
        body = self.read()
        if self.path == "/_mode":
            with LOCK:
                STATE["mode"] = body.get("mode", "ok")
            return self.send(200, {"mode": STATE["mode"]})
        if self.path != "/v1/messages":
            return self.send(404, {"error": "not found"})
        system = body.get("system") or ""
        messages = body.get("messages") or [{}]
        user = messages[0].get("content") or ""
        kind = kind_of(system)
        with LOCK:
            STATE["calls"].append({"kind": kind, "system": system, "user": user})
            mode = STATE["mode"]
        if mode == "fail":
            return self.send(500, {"error": {"type": "api_error", "message": "mock failure"}})
        if mode == "quota":
            return self.send(429, {"error": {"type": "rate_limit_error", "code": "account_quota_exhausted",
                                             "message": "Account usage limit reached"}}, [("Retry-After", "1")])
        out = answer(kind, user, mode)
        text = "ok" if out is None else json.dumps(out, ensure_ascii=False)
        self.send(200, {"content": [{"type": "text", "text": text}], "stop_reason": "end_turn"})


def main():
    port = int(os.environ.get("PORT", "18790"))
    ThreadingHTTPServer(("127.0.0.1", port), Handler).serve_forever()


if __name__ == "__main__":
    main()
