"""Mock messaging-source routes backed by deterministic local fixtures."""
import base64
import json
import os
import re
import urllib.error
import urllib.parse
import urllib.request
import uuid

SOURCE = "lark"
EXTENSION = "larkext"
CAPABILITIES = ["accounts", "pinned", "mentions", "attachments", "attachment_replies", "threads", "topics", "reactions",
                "read_receipts", "chat_pins", "profiles", "url_previews", "posts", "edit_history", "translation"]
MODES = [{'id': 'sealed', 'title': '加密消息', 'icon': 'lock', 'description': '只有拥有密钥的成员能读到正文', 'attachments': False, 'mentions': False, 'prepare': True}]






def source():
    return {"extension": EXTENSION, "id": SOURCE, "name": "飞书", "icon": "chat", "capabilities": CAPABILITIES,
            "composer_modes": MODES, "settings_ui": "display",
            "links": [{"hosts": ["applink.feishu.cn"], "paths": ["/client/moments/detail"], "opens": "post"}]}


# ---------------------------------------------------------------------------------------------------------------------













# ---------------------------------------------------------------------------------------------------------------------
# Feishu → the protocol's shapes.

def media(url):
    """A Feishu media URL as a path under the source (media/<key>, messages/<id>/media/<key>); absolute https as it is."""
    if not url:
        return ""
    for prefix in ("/api/v1/lark/", "/api/media/"):
        if url.startswith(prefix):
            return url[len(prefix):] if prefix == "/api/v1/lark/" else "media/" + url[len(prefix):]
    return url


def _int(v):
    try:
        return int(v)
    except (TypeError, ValueError):
        return None


def conversation(c):
    kind = "topic_group" if c.get("is_topic") else "group" if c.get("is_group") else "direct"
    out = {"id": c["id"], "name": c.get("name") or "", "kind": kind, "unread": c.get("unread") or 0,
           "preview": c.get("preview") or ""}
    for k_in, k_out in (("avatar", "avatar"), ("preview_sender", "preview_sender"), ("updated_at", "updated_at"),
                        ("read_position", "read_position"), ("mention_position", "mention_position")):
        if c.get(k_in):
            out[k_out] = media(c[k_in]) if k_out == "avatar" else c[k_in]
    for k_in, k_out in (("marked_unread", "marked_unread"), ("at_me", "mentioned"), ("muted", "muted"),
                        ("pinned", "pinned"), ("is_mine", "preview_mine")):
        if c.get(k_in):
            out[k_out] = True
    return out


_BLOCKS = {"paragraph": "paragraph", "quote": "quote", "code-block": "code", "heading-1": "heading1",
           "heading-2": "heading2", "heading-3": "heading3", "heading-4": "heading3", "heading-5": "heading3",
           "heading-6": "heading3", "list-item": "list_item"}


def part(p):
    """One Feishu part as the protocol's part (or None to drop it)."""
    kind, meta, text = p.get("kind"), p.get("metadata") or {}, p.get("text") or ""
    if kind == "text":
        if meta.get("system"):
            return {"kind": "system", "text": text}
        out = {"kind": "text", "text": text}
        marks = []
        for m in meta.get("marks") or []:
            m = {"strikethrough": "strike"}.get(m, m)
            if m in ("bold", "italic", "underline", "strike") and m not in marks:
                marks.append(m)
        if meta.get("inlineCode"):
            marks.append("code")
        if marks:
            out["marks"] = marks
        blocks = meta.get("blocks") or []
        if blocks:
            inner = blocks[-1]
            if inner.get("id"):
                out["block_id"] = str(inner["id"])[:200]
            t = inner.get("type", "")
            lists = [b for b in blocks if b.get("type") in ("ordered-list", "unordered-list")]
            if t == "list-item" or lists:
                out["block"] = "list_item"
                lst = lists[-1] if lists else {}
                out["list"] = {"ordered": lst.get("type") == "ordered-list"}
                if len(lists) > 1:
                    out["list"]["level"] = len(lists) - 1
                if lst.get("start"):
                    out["list"]["start"] = lst["start"]
            elif t in _BLOCKS:
                out["block"] = _BLOCKS[t]
        return out
    if kind == "at":
        return {"kind": "mention", "text": text, **({"user_id": p["refId"]} if p.get("refId") else {})}
    if kind in ("link", "docs"):
        return {"kind": "link", "text": text or p.get("url", ""), "url": p.get("url") or ""} if p.get("url") else {"kind": "text", "text": text}
    if kind == "emotion":
        if p.get("url"):
            return {"kind": "sticker", "media": media(p["url"]), "text": text}
        return {"kind": "emoji", "key": (p.get("refId") or text or "?").upper(), "text": text or p.get("refId") or ""}
    if kind == "interactive" and meta.get("richTag") == 32:
        return {"kind": "divider"}
    if kind == "image" and p.get("url"):
        out = {"kind": "image", "media": media(p["url"])}
        for k in ("width", "height"):
            if _int(meta.get(k)) is not None:
                out[k] = _int(meta.get(k))
        return out
    if kind == "file":
        out = {"kind": "file", "name": text or "文件"}
        if p.get("url"):
            out["media"] = media(p["url"])
        if _int(meta.get("size")) is not None:
            out["size"] = _int(meta.get("size"))
        if meta.get("extension"):
            out["ext"] = str(meta["extension"])[:20]
        if meta.get("destroyed"):
            out["expired"] = True
        return out
    if kind == "media" and p.get("url"):
        if str(meta.get("mime", "")).startswith("audio/"):
            return {"kind": "audio", "media": media(p["url"]), **({"duration_ms": _int(meta["duration"])} if _int(meta.get("duration")) else {})}
        out = {"kind": "video", "media": media(p["url"])}
        if _int(meta.get("duration")):
            out["duration_ms"] = _int(meta["duration"])
        if meta.get("posterUrl"):
            out["poster"] = {"media": media(meta["posterUrl"]), **{k: _int(meta.get("poster" + k.title())) for k in ("width", "height") if _int(meta.get("poster" + k.title())) is not None}}
        return out
    if kind == "code":
        return {"kind": "code", "text": text, **({"language": str(meta.get("languageName") or meta.get("language"))[:40]} if meta.get("languageName") or meta.get("language") else {})}
    if kind == "card":
        return card(p)
    return {"kind": "text", "text": text} if text else None


def card(p):
    """A Feishu card: summary cards as forward / share; an element tree as a view of its header and text (the mock's
    turn of it: the extension turns the whole tree)."""
    meta = p.get("metadata") or {}
    variant = meta.get("variant")
    if variant == "merge-forward":
        out = {"kind": "forward", "title": meta.get("title") or "聊天记录"}
        msgs = [m for m in (meta.get("messages") or []) if isinstance(m, dict)]
        if msgs:
            out["messages"] = [forwarded(m) for m in msgs[:50]]
        elif meta.get("previews"):
            out["previews"] = [{"sender_name": x.get("senderName") or "", "text": x.get("text") or ""} for x in meta["previews"][:10]]
        if _int(meta.get("messageCount")):
            out["count"] = _int(meta["messageCount"])
        return out
    if variant:
        out = {"kind": "share", "title": meta.get("title") or p.get("text") or variant}
        sub = meta.get("eyebrow") or meta.get("status") or meta.get("description")
        if sub:
            out["subtitle"] = str(sub)[:500]
        if meta.get("imageUrl"):
            out["image"] = {"media": media(meta["imageUrl"])}
        fields = [{"label": str(f.get("label", ""))[:100], "value": str(f.get("value", ""))[:2000]} for f in (meta.get("fields") or []) if isinstance(f, dict)]
        if fields:
            out["fields"] = fields[:12]
        return out
    view, words = card_view(meta, p.get("text") or "")
    return {"kind": "card", "text": words[:2000], "view": view}


_CARD_INLINE = {"plain_text", "link", "at", "at_all", "person", "person_v1", "person_list", "code_span", "emoji", "br",
                "local_datetime", "markdown_emphasis", "markdown_font", "markdown_fallback", "text_tag", "number_tag",
                "standard_icon", "custom_icon", "ud_icon"}


def _kids(n):
    return [c for c in (n.get("children") or []) if isinstance(c, dict)]


def _plain(n):
    """All the text a card node and its children show (the extension's cardNode.plain)."""
    kids = _kids(n)
    if not kids:
        return "\n" if n.get("tag") == "br" else str(n.get("text") or "")
    out = "".join(_plain(k) for k in kids)
    return out or str(n.get("text") or "")


def _columns_line(n):
    """A column_set of short columns as one line (the extension's columnsLine); "" when they are layout."""
    cols = []
    for c in _kids(n):
        words = [w for w in (_plain(k).strip() for k in _kids(c)) if w]
        line = " ".join(words)
        if len(words) > 3 or len(line) > 40 or "\n" in line:
            return ""
        if line:
            cols.append(line)
    return " · ".join(cols)


def card_view(meta, text):
    """The extension's cardView (the Lark extension's messaging_convert.go): the header as the section's title, inline runs as
    paragraphs, lists, table rows, panels and a div's fields as lines, dividers, https links and pressable buttons."""
    title = ""
    header = meta.get("header")
    if isinstance(header, dict):
        texts = [_plain(k).strip() for k in _kids(header) if k.get("tag") == "plain_text"]
        title = texts[0] if texts else ""
    if not title and not meta.get("hideHeader"):
        title = str(meta.get("title") or "")
    items, run = [], []

    def flush():
        s = "".join(run).strip()
        if s and len(items) < 60:
            items.append({"type": "text", "text": s[:2000]})
        run.clear()

    def walk(nodes):
        for n in nodes:
            tag = n.get("tag")
            if tag == "collapse_divider":
                continue
            if tag in _CARD_INLINE:
                run.append(_plain(n))
            elif tag in ("hr", "divider"):
                flush()
                items.append({"type": "divider"})
            elif tag == "button":
                flush()
                label = _plain(n).strip() or "打开"
                url, action = str(n.get("url") or ""), str(n.get("actionId") or "")
                if url.startswith("https://"):
                    items.append({"type": "link", "title": label[:2000], "url": url})
                elif action and _int(n.get("actionMethod")) == 2 and not n.get("disabled") and len(action) <= 128:
                    items.append({"type": "button", "title": label[:2000], "action": {"id": action}})
            elif tag in ("img", "image", "img_combination", "chart"):
                flush()
            elif tag == "list":
                flush()
                counters = {}
                for item in n.get("items") or []:
                    if not isinstance(item, dict):
                        continue
                    level = _int(item.get("level")) or 0
                    counters = {k: v for k, v in counters.items() if k <= level}
                    marker = ["•", "◦", "▪︎"][min(max(level, 0), 2)]
                    if item.get("type") == "ol":
                        counters[level] = counters.get(level, 0) + 1
                        marker = "%d." % counters[level]
                    line = _plain(item).strip()
                    if line:
                        run.append(marker + " " + line)
                        flush()
            elif tag == "column_set" and _columns_line(n):
                flush()
                run.append(_columns_line(n))
                flush()
            elif tag == "markdown_table_row":
                flush()
                run.append(" | ".join(_plain(c).strip() for c in _kids(n)))
                flush()
            elif tag == "collapsible_panel":
                flush()
                if isinstance(n.get("header"), dict):
                    run.append(_plain(n["header"]))
                    flush()
                walk(_kids(n))
                flush()
            elif tag == "div":
                flush()
                if _kids(n):
                    walk(_kids(n))
                else:
                    run.append(str(n.get("text") or ""))
                flush()
                for f in n.get("fields") or []:
                    if isinstance(f, dict):
                        run.append(_plain(f))
                        flush()
            else:
                flush()
                if _kids(n):
                    walk(_kids(n))
                else:
                    run.append(str(n.get("text") or ""))
                flush()

    if isinstance(meta.get("body"), dict):
        walk(_kids(meta["body"]))
    flush()
    words = "\n".join(str(f) for f in (meta.get("fallback") or []) if f) or text or title
    if not items:
        items = [{"type": "text", "text": (words or "卡片")[:2000], "style": "secondary"}]
    return {"sections": [{"title": title[:2000], "items": items}]}, words


def parts_of(raw):
    return [x for x in (part(p) for p in (raw or []) if isinstance(p, dict)) if x]


def forwarded(m):
    parts = parts_of(m.get("parts"))
    text = m.get("textPreview") or "".join(p.get("text", "") for p in parts if p.get("kind") == "text")
    out = {"id": m.get("id") or uuid.uuid4().hex, "position": "0", "sender": {"name": m.get("senderName") or ""},
           "mine": False, "parts": parts or [{"kind": "text", "text": text or ""}], "text": text or ""}
    if m.get("senderAvatarUrl"):
        out["sender"]["avatar"] = media(m["senderAvatarUrl"])
    if m.get("isRecalled"):
        out["recalled"] = True
    return out





def message(m, display):
    """Map source content without interpreting plain text as a private encoding."""
    sender = {"name": m.get("sender_name") or ""}
    if m.get("sender_id"):
        sender["id"] = m["sender_id"]
    if m.get("sender_avatar"):
        sender["avatar"] = media(m["sender_avatar"])
    if m.get("type") == "SYSTEM":
        sender["kind"] = "system"
    elif m.get("sender_type") == "bot":
        sender["kind"] = "bot"
    parts = parts_of(m.get("parts"))
    text = m.get("text") or ""
    decorations = []
    out = {"id": m["id"], "position": str(m.get("position") or "0"), "sender": sender, "mine": bool(m.get("is_mine"))}
    olk = m.get("olk")
    if isinstance(olk, dict):
        opened = olk.get("version") == 1 and olk.get("state") == "decrypted"
        cover = parts_of(olk.get("publicParts"))
        cover_text = "".join(p.get("text", "") for p in cover if p.get("kind") == "text")
        if opened:
            parts = parts_of(olk.get("bodyParts"))
            text = "".join(p.get("text", "") for p in parts if p.get("kind") == "text") or text
            out["private"], out["private_text"] = True, (cover_text or "加密消息")[:2000]
            decorations.append({"text": "加密消息", "icon": "lock"})
        else:
            parts, text = cover, cover_text or "加密消息"
            decorations.append({"text": "加密消息 · 对外展示" if cover else "加密消息", "icon": "lock"})
    if not parts and text and m.get("type") != "SYSTEM" and not olk:
        parts = [{"kind": "text", "text": text}]  # a plain message the hub keeps as text only
    if m.get("recalled") and not (display.get("anti_recall") and m.get("retained")):
        parts, text = [], "消息已撤回"
    pass
    if m.get("type") == "SYSTEM" and not parts:
        parts = [{"kind": "system", "text": text}]
    out["parts"], out["text"] = parts, text[:20000]
    for k_in, k_out in (("created_at", "created_at"), ("client_id", "client_id")):
        if m.get(k_in):
            out[k_out] = m[k_in]
    if m.get("local_state") in ("pending", "failed"):
        out["state"] = m["local_state"]
    r = m.get("reply_to")
    if isinstance(r, dict) and r.get('id'):
        summary = r.get('text') or ''
        if r.get('recalled') and (not (display.get('anti_recall') and r.get('retained'))):
            summary = '消息已撤回'
        out['reply_to'] = {'id': r['id'], 'sender_name': r.get('sender_name') or '', 'summary': summary[:2000]}
        if r.get('recalled') and display.get('anti_recall') and r.get('retained'):
            out['reply_to']['recalled'] = True  # the summary is the kept words (the extension's mark, docs「五」)
    count = m.get("thread_reply_count") or 0
    if count:
        out["thread"] = {"count": count}
    if m.get("reactions"):
        out["reactions"] = [{"key": str(x.get("key", "")).upper(), "count": x.get("count") or 0,
                             **({"mine": True} if x.get("mine") else {}), **({"users": x["users"][:20]} if x.get("users") else {})}
                            for x in m["reactions"] if x.get("key")]
    if m.get("edited"):
        out["edited"] = True
        if display.get("show_edit_history") and m.get("edit_history"):
            hist = []
            for v in m["edit_history"]:
                vp = parts_of((v.get("olk") or {}).get("publicParts") if v.get("olk") else v.get("parts"))
                ht = "".join(p.get("text", "") for p in vp if p.get("kind") == "text") or v.get("text") or ""
                hist.append({"text": ht, "parts": vp, **({"edited_at": v["edited_at"]} if _iso(v.get("edited_at")) else {})})
            out["edit_history"] = hist
    if m.get("recalled"):
        out["recalled"] = True
    if decorations:
        out["decorations"] = decorations[:3]
    for k in ("can_pin", "can_unpin"):
        if m.get(k) is False:
            out[k] = False
    if m.get("is_read"):
        out["read"] = True
    return out


def _iso(v):
    return isinstance(v, str) and re.match(r"^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d", v) is not None


def page(p, display, account):
    out = {"messages": [message(m, display) for m in p.get("messages") or []], "has_older": bool(p.get("has_older"))}
    if account:
        out["account"] = account
    if p.get("has_newer"):
        out["has_newer"] = True
    if p.get("topic"):
        out["topic"] = True
    for k in ("older_cursor", "newer_cursor"):
        if p.get(k):
            out[k] = str(p[k])
    return out


def event_of(ev):
    """The WebSocket {type: lark} the mock pushes, as the extension's messaging.changed would reach the apps."""
    out = {"type": "messaging", "source": SOURCE, "conversations": list(ev.get("feed_ids") or [])}
    if ev.get("ol_account"):
        out["account"] = str(ev["ol_account"])
    if ev.get("settings_changed"):
        out["settings_changed"] = True
    return out


# ---------------------------------------------------------------------------------------------------------------------
# The routes.

class _Lark:
    """The mock's own /api/v1/lark/…, called the way the Lark extension calls its gateway."""

    def __init__(self, handler, account):
        port = handler.server.server_address[1]
        self.base = "http://127.0.0.1:%d/api/v1/lark" % port
        self.auth = handler.headers.get("Authorization", "")
        self.account = account

    def call(self, method, path, query=None, body=None, raw=None, content_type=None):
        q = dict(query or {})
        if self.account and self.account != "1":
            q.setdefault("ol_account", self.account)
        url = self.base + path + ("?" + urllib.parse.urlencode(q) if q else "")
        data = raw if raw is not None else (json.dumps(body).encode() if body is not None else None)
        req = urllib.request.Request(url, data=data, method=method)
        if self.auth:
            req.add_header("Authorization", self.auth)
        if data is not None:
            req.add_header("Content-Type", content_type or "application/json")
        try:
            with urllib.request.urlopen(req, timeout=60) as r:
                return r.status, r.read(), r.headers.get("Content-Type", "")
        except urllib.error.HTTPError as e:
            return e.code, e.read(), e.headers.get("Content-Type", "")

    def json(self, method, path, query=None, body=None):
        status, raw, _ = self.call(method, path, query, body)
        try:
            return status, json.loads(raw or b"{}")
        except ValueError:
            return status, {}


_CODES = {"lark_not_logged_in": "not_signed_in", "lark_account_changed": "account_changed", "VAULT_LOCKED": "input_required"}


def _error(handler, status, body):
    code = body.get("code") if isinstance(body, dict) else None
    err = body.get("error") if isinstance(body, dict) else None
    if isinstance(err, dict):
        code, err = code or err.get("code"), err.get("message")
    out = {"error": str(err or "飞书出错了")}
    if _CODES.get(code):
        out["code"] = _CODES[code]
    return handler.send_json(out, status)


def _display(lark):
    status, d = lark.json("GET", "/message-display")
    return d if status == 200 else {}


SEALED = {}  # token → {"account_id", "feed", "chunks", "reply_to"}


def route(handler, method, path, query, body):
    """Serve /api/v1/messaging[/lark/…]; False when the path is not the protocol's."""
    if path == "/api/v1/messaging" and method == "GET":
        return handler.send_json({"sources": [source()]}) or True
    # The hub serves an extension's pages under both /api/v1/extensions and the older /api/v1/plugins (extension_compat).
    if any(path.startswith("/api/v1/%s/%s/-/ui/display" % (p, EXTENSION)) for p in ("extensions", "plugins")) and method == "POST":
        return settings_page(handler, body) or True
    prefix = "/api/v1/messaging/%s/" % SOURCE
    if not path.startswith(prefix):
        if path.startswith("/api/v1/messaging/"):
            return handler.send_json({"error": "no such messaging source", "code": "not_found"}, 404) or True
        return False
    rest = path[len(prefix):]
    seg = rest.split("/")
    account = (query.get("account") or [""])[0]
    lark = _Lark(handler, account)
    q1 = lambda k, d=None: (query.get(k) or [d])[0]

    if method == 'GET' and rest == 'status':
        status, s = lark.json('GET', '/status')
        disp = _display(lark)
        est, enc = lark.json('GET', '/encryption')
        modes = []
        if est == 200 and enc.get('configured'):
            modes.append('sealed')
        pass
        return handler.send_json({'configured': bool(s.get('configured', True)), 'connected': bool(s.get('connected', True)), 'signed_in': status == 200 and (not s.get('error')), **({'name': s['name']} if s.get('name') else {}), **({'error': s['error']} if s.get('error') else {}), 'modes': modes}) or True
    if method == "GET" and rest == "me":
        user = me(lark)
        if not user:
            return handler.send_json({"error": "飞书未登录", "code": "not_signed_in"}, 502) or True
        return handler.send_json({"account": account or "1", "user_id": user}) or True
    if method == "GET" and rest == "accounts":
        status, a = lark.json("GET", "/accounts")
        if status != 200:
            return _error(handler, status, a) or True
        return handler.send_json({"accounts": [{"id": str(x["id"]), "signed_in": bool(x.get("signed_in")),
                                                **{k: x[k] for k in ("name", "avatar", "tenant", "unread") if x.get(k) is not None}}
                                               for x in a.get("accounts") or []],
                                  "default": str(a.get("default") or 1)}) or True
    if method == 'GET' and rest in ('conversations', 'pinned'):
        q = {'account_bound': '1'}
        if rest == 'conversations':
            q.update(scope='chats', limit=q1('limit', '60'))
            if q1('cursor'):
                q['cursor'] = q1('cursor')
        status, c = lark.json('GET', '/' + rest, q)
        if status != 200:
            return _error(handler, status, c) or True
        out = {'conversations': [conversation(x) for x in c.get('conversations') or []]}
        if account:
            out['account'] = account
        if c.get('next_cursor'):
            out['next_cursor'] = c['next_cursor']
        return handler.send_json(out) or True
    if len(seg) == 3 and seg[0] == "conversations" and seg[2] == "messages" and method == "GET":
        q = {k: q1(k) for k in ("before", "after", "around", "limit", "topic") if q1(k)}
        status, p = lark.json("GET", "/conversations/%s/messages" % seg[1], q)
        if status != 200:
            return _error(handler, status, p) or True
        return handler.send_json(page(p, _display(lark), account)) or True
    if len(seg) == 2 and seg[0] == "messages" and method == "GET":
        status, raw = lark.json("GET", "/raw/messages/" + seg[1])
        if status != 200:
            return _error(handler, status, raw) or True
        item = raw.get("item") or {}
        _, p = lark.json("GET", "/conversations/%s/messages" % item.get("feedId"))  # the newest page: a send just made is in it
        m = next((x for x in p.get("messages") or [] if x.get("id") == seg[1]), None)
        m = m or {"id": seg[1], "position": "0", "sender_name": "", "is_mine": item.get("isMine", False), "text": "", "type": "TEXT"}
        if item.get("localState") in ("pending", "failed"):
            m = dict(m, local_state=item["localState"])
        return handler.send_json({"message": message(m, _display(lark)), **({"account": account} if account else {})}) or True
    if len(seg) == 3 and seg[0] == "messages" and seg[2] == "thread" and method == "GET":
        q = {k: q1(k) for k in ("before", "limit") if q1(k)}
        status, p = lark.json("GET", "/messages/%s/thread" % seg[1], q)
        if status != 200:
            return _error(handler, status, p) or True
        disp = _display(lark)
        out = page(p, disp, account)
        if out["messages"] and out["messages"][0]["id"] == seg[1]:
            out["root"] = out["messages"].pop(0)
        return handler.send_json(out) or True
    if len(seg) == 3 and seg[0] == "conversations" and seg[2] == "messages" and method == "POST":
        return send(handler, lark, seg[1], body) or True
    if len(seg) == 3 and seg[0] == "conversations" and seg[2] == "attachments" and method == "POST":
        return attach(handler, lark, seg[1], query, body) or True
    if len(seg) == 3 and seg[0] == "conversations" and seg[2] == "compose" and method == "POST":
        return compose(handler, lark, seg[1], body) or True
    if len(seg) == 3 and seg[0] == "conversations" and seg[2] == "read" and method == "POST":
        lark.json("POST", "/conversations/%s/read" % seg[1], body=json.loads(body or b"{}"))
        return handler.send_json({}) or True
    if len(seg) == 3 and seg[0] == "conversations" and seg[2] == "read-states" and method == "POST":
        status, r = lark.json("POST", "/conversations/%s/read-states" % seg[1], body=json.loads(body or b"{}"))
        if status != 200:
            return _error(handler, status, r) or True
        return handler.send_json({"states": [{"message_id": s["message_id"], "read": s.get("read_count", 0), "unread": s.get("unread_count", 0)}
                                             for s in r.get("states") or []]}) or True
    if len(seg) == 3 and seg[0] == "conversations" and seg[2] == "members" and method == "GET":
        status, r = lark.json("GET", "/conversations/%s/members" % seg[1], {k: q1(k) for k in ("q", "limit") if q1(k)})
        if status != 200:
            return _error(handler, status, r) or True
        return handler.send_json({"members": [{"id": x.get("user_id", ""), "name": x.get("name", ""), **({"avatar": media(x["avatar"])} if x.get("avatar") else {})}
                                              for x in r.get("members") or []]}) or True
    if len(seg) >= 3 and seg[0] == "conversations" and seg[2] == "pins":
        return pins(handler, lark, seg, method, query, body) or True
    if len(seg) == 3 and seg[0] == "messages" and seg[2] == "reactions" and method == "POST":
        # Keys are the vendor's own spelling (upper case, as the real extension passes Feishu's on); the mock's old
        # routes keep them lower case.
        req = json.loads(body or b"{}")
        if isinstance(req.get("key"), str):
            req["key"] = req["key"].lower()
        lark.json("POST", "/messages/%s/reactions" % seg[1], body=req)
        return handler.send_json({}) or True
    if len(seg) == 4 and seg[0] == "messages" and seg[2] == "actions" and method == "POST":
        lark.json("POST", "/messages/%s/card-actions/%s" % (seg[1], seg[3]), body={})
        return handler.send_json({"toast": "已提交"}) or True
    if len(seg) == 3 and seg[0] == "messages" and seg[2] == "url-preview" and method == "GET":
        status, r = lark.json("GET", "/messages/%s/url-preview" % seg[1], {"url": q1("url", "")})
        if status != 200:
            return _error(handler, status, r) or True
        cardx = r.get("card") or {}
        return handler.send_json({"url": r.get("url") or q1("url", ""), **{k: r[k] for k in ("title", "host") if r.get(k)},
                                  **({"summary": cardx["body"][:2000]} if cardx.get("body") else {}),
                                  **({"author": {"name": cardx["author"], **({"avatar": media(cardx["avatarUrl"])} if cardx.get("avatarUrl") else {})}} if cardx.get("author") else {})}) or True
    if method == "GET" and rest == "emojis":
        status, r = lark.json("GET", "/emojis")
        groups = [dict(g, keys=[str(k).upper() for k in g.get("keys") or []]) for g in r.get("groups") or []]
        return handler.send_json({"emojis": {k.upper(): {"label": v.get("label", k), **({"url": media(v["url"])} if v.get("url") else {})}
                                             for k, v in (r.get("emojis") or {}).items()},
                                  **({"groups": groups} if groups else {})}) or True
    if method == "GET" and rest == "reactions/quick":
        status, r = lark.json("GET", "/reactions/quick")
        return handler.send_json({"keys": [k.upper() for k in r.get("keys") or []]}) or True
    if len(seg) == 2 and seg[0] == "users" and method == "GET":
        status, r = lark.json("GET", "/users/" + seg[1], {"feed_id": q1("conversation")} if q1("conversation") else None)
        if status != 200:
            return _error(handler, status, r) or True
        return handler.send_json(profile(seg[1], r)) or True
    if len(seg) == 3 and seg[0] == "users" and seg[2] == "chat" and method == "POST":
        status, r = lark.json("POST", "/users/%s/chat" % seg[1], body={})
        if status != 200:
            return _error(handler, status, r) or True
        return handler.send_json({"conversation_id": r.get("feed_id", "")}) or True
    if method == "GET" and (seg[0] == "media" or (len(seg) >= 4 and seg[0] == "messages" and seg[2] == "media")):
        status, raw, ctype = lark.call("GET", "/" + rest, {k: q1(k) for k in ("size",) if q1(k)})
        handler.send_response(status)
        handler.send_header("Content-Type", ctype or "application/octet-stream")
        handler.send_header("Content-Length", str(len(raw)))
        handler.end_headers()
        handler.wfile.write(raw)
        return True
    if len(seg) == 2 and seg[0] == "posts" and method == "GET":
        status, r = lark.json("GET", "/moments/posts/" + seg[1])
        if status != 200:
            return _error(handler, status, r) or True
        return handler.send_json(post(r)) or True
    if len(seg) == 3 and seg[0] == "posts" and seg[2] == "comments" and method == "GET":
        status, r = lark.json("GET", "/moments/posts/%s/comments" % seg[1])
        if status != 200:
            return _error(handler, status, r) or True
        return handler.send_json({"comments": [{"id": c.get("id", ""), "author": {"name": c.get("userName", ""), **({"id": c["userId"]} if c.get("userId") else {})},
                                                "parts": [{"kind": "text", "text": c.get("content") or ""}], **({"hot": True} if c.get("isHot") else {})}
                                               for c in r.get("comments") or []]}) or True
    return handler.send_json({"error": "no such messaging route", "code": "not_found"}, 404) or True


def me(lark):
    """Who the account is: the mock's account-bound lists say (the pins fixture changes it), else the gateway's accounts
    (the history fixture, MOCK_LARK_HISTORY, changes those: an account switch while a page is on its way)."""
    if not os.environ.get("MOCK_LARK_HISTORY"):
        _, bound = lark.json("GET", "/pinned", {"account_bound": "1"})
        if bound.get("account_id"):
            return bound["account_id"]
    status, a = lark.json("GET", "/raw/accounts")
    active = next((x for x in a.get("accounts") or [] if x.get("id") == a.get("activeId") or x.get("active")), None)
    return (active or {}).get("userId") if status == 200 else None


def profile(uid, r):
    out = {"id": uid, "name": r.get("displayName") or r.get("enName") or "", "kind": "bot" if r.get("userType") == "bot" else "user", "can_chat": True}
    if r.get("avatarUrl"):
        out["avatar"] = media(r["avatarUrl"])
    if r.get("coverUrl"):
        out["cover"] = media(r["coverUrl"])
    sub = " · ".join(x for x in (r.get("alias"), r.get("enName")) if x)
    if sub:
        out["subtitle"] = sub[:500]
    if (r.get("customStatus") or {}).get("title"):
        out["status"] = r["customStatus"]["title"][:200]
    if r.get("description"):
        out["description"] = r["description"][:2000]
    fields = []
    for f in r.get("fields") or []:
        if f.get("key") in ("B-ALIAS", "B-ALIAS-AND-MEMO", "B-CHATGROUPNICKNAME", "B-MORE") or not f.get("label"):
            continue
        x = {"label": f["label"][:100], "value": str(f.get("value") or "")[:2000]}
        url = f.get("url") or ""
        m = re.match(r"^lark://client/profile\?uid=(\w+)", url)
        if m:
            x["person"] = m.group(1)
        elif url.startswith("https://"):
            x["url"] = url
        fields.append(x)
    if fields:
        out["fields"] = fields[:30]
    return out


def post(r):
    segs = (r.get("content") or {}).get("segments") or []
    parts = []
    for s in segs:
        if s.get("kind") == "emotion":
            parts.append({"kind": "emoji", "key": (s.get("emojiKey") or s.get("key") or "?").upper(), "text": s.get("text") or ""})
        elif s.get("text"):
            parts.append({"kind": "text", "text": s["text"]})
    if not parts and (r.get("content") or {}).get("text"):
        parts = [{"kind": "text", "text": r["content"]["text"]}]
    out = {"id": str(r.get("id", "")), "author": {"name": r.get("userName") or "", **({"id": r["userId"]} if r.get("userId") else {}),
                                                  **({"avatar": media(r["userAvatarUrl"])} if r.get("userAvatarUrl") else {})},
           "parts": parts}
    imgs = [{"media": media(i["url"]), **{k: i[k] for k in ("width", "height") if isinstance(i.get(k), int)}} for i in r.get("images") or [] if i.get("url")]
    if imgs:
        out["images"] = imgs[:30]
    if r.get("reactionGroups"):
        out["reactions"] = [{"key": str(g.get("type", "")).upper(), "count": g.get("count") or 0, **({"mine": True} if g.get("selfInvolved") else {})}
                            for g in r["reactionGroups"] if g.get("type")]
    if isinstance(r.get("commentCount"), int):
        out["comment_count"] = r["commentCount"]
    if r.get("categoryNames"):
        out["categories"] = [str(c)[:100] for c in r["categoryNames"][:10]]
    return out


def send(handler, lark, feed, body):
    req = json.loads(body or b"{}")
    text, mode = req.get("text") or "", req.get("mode") or ""
    if req.get("expect_user"):
        user = me(lark)
        if user and user != req["expect_user"]:
            return handler.send_json({"error": "飞书账号已切换，消息没有发出", "code": "account_changed"}, 409)
    if mode == "sealed":
        prepared = SEALED.pop(req.get("prepared") or "", None)
        if not prepared or prepared["feed"] != feed:
            return handler.send_json({"error": "加密内容已过期，请重新确认", "code": "input_required"}, 409)
        last = {}
        for i, chunk in enumerate(prepared["chunks"]):
            cid = req["client_id"] if i == len(prepared["chunks"]) - 1 else str(uuid.uuid5(uuid.NAMESPACE_URL, req["client_id"] + ":" + str(i)))
            status, last = lark.json("POST", "/conversations/%s/encrypted-messages" % feed,
                                     body={"account_id": prepared["account_id"], "ciphertext": chunk, "client_id": cid,
                                           **({"reply_id": req["reply_to"]} if req.get("reply_to") else {})})
            if status != 200:
                return _error(handler, status, last)
        return handler.send_json({k: last[k] for k in ("message_id",) if last.get(k)})
    pass
    out = {"text": text, "client_id": req["client_id"]}
    if req.get("mentions"):
        out["mentions"] = req["mentions"]
    if req.get("reply_to"):
        out["thread"] = bool(req.get("thread"))
        status, r = lark.json("POST", "/messages/%s/replies" % req["reply_to"], body=out)
    else:
        status, r = lark.json("POST", "/conversations/%s/messages" % feed, body=out)
    if status != 200:
        return _error(handler, status, r)
    return handler.send_json({k: r[k] for k in ("message_id",) if r.get(k)})


def attach(handler, lark, feed, query, body):
    q1 = lambda k: (query.get(k) or [None])[0]
    if q1("kind") == "image":
        payload = {"client_id": q1("client_id"), "name": q1("name") or "image", "mime": q1("mime") or "image/png",
                   "data": base64.b64encode(body or b"").decode()}
        if q1("reply_to"):
            payload["reply_to"] = q1("reply_to")
            if q1("thread") in ("1", "true"):
                payload["thread"] = True
        status, r = lark.json("POST", "/conversations/%s/images" % feed, body=payload)
    else:
        q = {k: q1(k) for k in ("client_id", "name", "mime") if q1(k)}
        if q1("reply_to"):
            q["reply_to"] = q1("reply_to")
            if q1("thread") in ("1", "true"):
                q["thread_root"] = q1("reply_to")
        status, raw, _ = lark.call("POST", "/conversations/%s/files" % feed, q, raw=body or b"", content_type="application/octet-stream")
        try:
            r = json.loads(raw or b"{}")
        except ValueError:
            r = {}
    if status != 200:
        return _error(handler, status, r)
    return handler.send_json({k: r[k] for k in ("message_id",) if r.get(k)})


def compose(handler, lark, feed, body):
    req = json.loads(body or b"{}")
    if req.get("mode") != "sealed":
        return handler.send_json({"ready": True, "token": ""})
    values = req.get("values") or {}
    status, st = lark.json("GET", "/encryption")
    if status != 200 or not st.get("configured"):
        return handler.send_json({"error": "请先在飞书网关的网页创建密钥库", "code": "unsupported"}, 409 if status == 200 else status)
    account_id = st.get("accountId", "")
    if st.get("locked"):
        if values.get("password"):
            status, st = lark.json("POST", "/encryption/unlock", body={"account_id": account_id, "password": values["password"]})
            if status != 200:
                return handler.send_json({"view": unlock_view("密码不正确，请重试")})
        else:
            return handler.send_json({"view": unlock_view()})
    cover = values.get("cover")
    if not cover:
        _, covers = lark.json("POST", "/conversations/%s/encryption/covers" % feed, body={"account_id": account_id})
        lines = covers.get("lines") or []
        field = ({"type": "field", "name": "cover", "label": "对外展示的内容", "kind": "choice",
                  "choices": [{"value": line[:2000]} for line in lines[:30]], "value": lines[0][:2000]} if lines else
                 {"type": "field", "name": "cover", "label": "对外展示的内容", "kind": "text"})
        return handler.send_json({"view": {"title": "对外展示", "sections": [{"footer": "选一段历史消息，作为无法解密时看到的内容。", "items":
            [field, {"type": "button", "title": "用这段", "action": {"id": "cover", "submit": True}}]}]}})
    status, prep = lark.json("POST", "/conversations/%s/encryption/prepare" % feed,
                             body={"account_id": account_id, "text": req.get("text") or "", "cover": cover})
    if status != 200:
        return _error(handler, status, prep)
    token = uuid.uuid4().hex
    SEALED[token] = {"account_id": account_id, "feed": feed, "chunks": [m["text"] for m in prep.get("messages") or []]}
    return handler.send_json({"ready": True, "token": token})


def unlock_view(problem=""):
    items = [{"type": "field", "name": "password", "label": "密钥库密码", "kind": "secret"},
             {"type": "button", "title": "解锁", "action": {"id": "unlock", "submit": True}}]
    if problem:
        items.insert(0, {"type": "text", "text": problem, "style": "secondary"})
    return {"title": "解锁密钥库", "sections": [{"items": items}]}


def pins(handler, lark, seg, method, query, body):
    q1 = lambda k: (query.get(k) or [None])[0]
    _, ident = lark.json("GET", "/message-display")
    account_id = ident.get("account_id", "")
    if method == "GET" and len(seg) == 3:
        q = {k: q1(k) for k in ("cursor", "limit") if q1(k)}
        q["account_id"] = account_id
        status, r = lark.json("GET", "/conversations/%s/pins" % seg[1], q)
        if status != 200:
            return _error(handler, status, r)
        disp = _display(lark)
        items = []
        for it in r.get("items") or []:
            x = {"id": it["id"], "message_id": it["message_id"]}
            if it.get("message"):
                x["message"] = message(it["message"], disp)
            for k in ("operator_name", "created_at"):
                if it.get(k):
                    x[k] = it[k]
            items.append(x)
        return handler.send_json({"items": items, **({"next_cursor": r["next_cursor"]} if r.get("next_cursor") else {}),
                                  **({"total": r["total"]} if isinstance(r.get("total"), int) else {})})
    if method == "GET" and len(seg) == 4:
        status, r = lark.json("GET", "/conversations/%s/pin-messages/%s" % (seg[1], seg[3]), {"account_id": account_id, "topic": q1("topic") or "0"})
        if status != 200:
            return _error(handler, status, r)
        disp, t = _display(lark), r.get("target") or {}
        out = {"message": message(t["message"], disp), "page": page(t.get("page") or {}, disp, "")}
        if t.get("root"):
            out["root"] = message(t["root"], disp)
        return handler.send_json({"target": out})
    if method == "POST" and len(seg) == 3:
        req = json.loads(body or b"{}")
        status, r = lark.json("POST", "/conversations/%s/pins" % seg[1], body={"account_id": account_id, **req})
        if status != 200:
            return _error(handler, status, r)
        return handler.send_json({"pinned": bool(r.get("pinned"))})
    return handler.send_json({"error": "no such messaging route", "code": "not_found"}, 404)


def settings_page(handler, body):
    """The extension's display settings page (extension_page `display`): the switches as fields, saved on "Save"."""
    req = json.loads(body or b"{}")
    lark = _Lark(handler, "")
    status, d = lark.json("GET", "/message-display")
    if status != 200:
        return handler.send_json({"view": {"title": "风灵月影", "sections": [{"items": [{"type": "empty", "title": "暂时无法读取设置"}]}]}})
    toast = None
    if req.get("action") and req["action"].startswith("toggle:"):
        key = req["action"].split(":", 1)[1]
        if key in d and key != "account_id":
            s2, d2 = lark.json("POST", "/message-display", body={"account_id": d["account_id"], "key": key, "value": not d[key], "expected": d[key]})
            if s2 == 200:
                d = d2
            else:
                toast = "设置已变化，请重新读取"
    rows = [('anti_recall', '防撤回', '查看飞书网关已留存的撤回内容，保留「已撤回」标记。'), ('show_edit_history', '编辑历史', '点击消息下方的「已编辑」，查看已留存的旧版本。')]
    items = []
    for key, title, sub in rows:
        if key in d:
            items.append({"type": "row", "key": key, "title": title, "subtitle": sub, "value": "开" if d[key] else "关",
                          "action": {"id": "toggle:" + key}})
    out = {"view": {"title": "风灵月影", "sections": [{"footer": "与飞书网关的网页共用当前飞书账号的设置。", "items": items}]}}
    if toast:
        out["toast"] = toast
    return handler.send_json(out)
