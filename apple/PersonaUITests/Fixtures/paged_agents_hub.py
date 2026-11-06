#!/usr/bin/env python3
"""Bounded 8,000-row Apple UI fixture; never connects to a real hub or node."""
import argparse
import base64
import collections
import copy
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import struct
import sys
import threading
import time
from http.server import ThreadingHTTPServer
from urllib.parse import parse_qs, urlparse

parser = argparse.ArgumentParser()
parser.add_argument("--port", type=int, required=True)
parser.add_argument("--legacy", action="store_true")
args = parser.parse_args()
os.environ["PORT"] = str(args.port)
os.environ["MOCK_AGENTS"] = "0"
root = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(root / "scripts/mock"))
spec = importlib.util.spec_from_file_location("apple_paging_base", root / "scripts/mock/mock_hub.py")
base = importlib.util.module_from_spec(spec)
spec.loader.exec_module(base)
lock = threading.RLock()
metrics = []
sockets = []
revision = 0
changes = []
snapshots = {}

rows = []
for n in range(8000):
    state = ["needs_you", "working", "unread", "idle"][n % 4]
    row = {"machine_id": ["server", "macbook", "studio"][n % 3], "session_id": "paged-%05d" % n,
           "title": "示例 Agent %05d" % n, "cwd": "/demo/project-%02d" % (n % 20),
           "repo_root": "/demo/project-%02d" % (n % 20), "workspace_cwd": "/demo/project-%02d" % (n % 20),
           "status": "working" if state == "working" else "idle", "state": state,
           "space_id": "space-%02d" % (n % 10), "space_name": "示例项目 %02d" % (n % 10),
           "space_kind": "herdr", "space_index": n % 10, "slot_id": "slot-%05d" % n,
           "slot_name": "任务 %05d" % n, "slot_index": n, "pane_id": "pane-%05d" % n,
           "agent": "claude", "updated_at": base.ago(minutes=n / 60), "state_since": base.ago(minutes=n / 60),
           "pending_interactions": 0, "assistant": False, "started_by_persona": False,
           "waiting": False, "last_text": "示例进展：数据仅用于本地分页和滚动验证。" * 8}
    rows.append(row)
personal = [{"id": "demo:personal-%05d" % n, "source": "demo", "sessionKey": "personal-%05d" % n,
             "provider": "demo", "accountId": "example", "title": "个人 Agent %05d" % n,
             "canSend": True, "unreadCount": 1 if n % 2 else 0, "rankTimeMs": 1800000000000 - n * 1000}
            for n in range(8000)]


def token():
    return "fixture-revision-%d" % revision


def matches(row, query, is_personal):
    fields = {"machine_id": row.get("machine_id", ""), "state": ("unread" if row["unreadCount"] else "idle") if is_personal else row["state"],
              "project": row.get("repo_root", ""), "group": row.get("accountId" if is_personal else "space_id", ""),
              "source": row.get("source" if is_personal else "source_kind", ""), "provider": row.get("provider" if is_personal else "agent", "")}
    if any(query.get(k, [""])[0] and query[k][0] != value for k, value in fields.items()):
        return False
    q = query.get("q", [""])[0].lower()
    return not q or q in "\n".join(str(row.get(k, "")) for k in ("title", "cwd", "repo_root", "space_name", "slot_name", "last_text", "provider", "accountId")).lower()


class Handler(base.H):
    def do_GET(self):
        global revision
        parsed = urlparse(self.path)
        query = parse_qs(parsed.query)
        path = parsed.path
        if path == "/mock/paging/reset-metrics":
            with lock:
                metrics.clear()
            return self.send_json({"ok": True})
        if path == "/mock/paging/metrics":
            with lock:
                return self.send_json({"rows": len(rows), "personal_rows": len(personal), "requests": copy.deepcopy(metrics), "revision": token()})
        if path == "/mock/paging/change":
            with lock:
                revision += 1
                index = int(query.get("row", ["0"])[0])
                rows[index]["title"] = "已更新 Agent %05d" % index
                changes.append(copy.deepcopy(rows[index]))
                for send in list(sockets):
                    try:
                        send({"type": "sessions_list", "revision": token()})
                    except OSError:
                        pass
            return self.send_json({"ok": True})
        if path in ("/api/v1/sessions", "/api/v1/agents/sessions"):
            start = time.perf_counter()
            is_personal = path.endswith("agents/sessions")
            with lock:
                collection = personal if is_personal else rows
                bounded = any(key in query for key in ("summary", "limit", "cursor", "since", "state", "project", "group", "counts"))
                if args.legacy or not bounded:
                    result = {"sessions": collection}
                else:
                    current = [row for row in collection if matches(row, query, is_personal)]
                    if query.get("counts", [""])[0] == "1":
                        result = {"total": len(current), "revision": token()}
                        for key, field in [("machine", "machine_id"), ("project", "repo_root"), ("group", "accountId" if is_personal else "space_id"), ("state", "state")]:
                            result[key] = dict(collections.Counter(("unread" if r["unreadCount"] else "idle") if is_personal and key == "state" else r.get(field, "") for r in current))
                    else:
                        limit = int(query.get("limit", ["50"])[0])
                        cursor = query.get("cursor", [None])[0]
                        if cursor:
                            snapshot, offset = cursor.rsplit(":", 1)
                            frozen, frozen_revision, frozen_total = snapshots[snapshot]
                            offset = int(offset)
                        else:
                            offset = 0
                            frozen = [r for r in changes if matches(r, query, is_personal)] if "since" in query else current
                            frozen = copy.deepcopy(frozen)
                            frozen_revision, frozen_total = token(), len(current)
                            snapshot = str(len(snapshots))
                            snapshots[snapshot] = (frozen, frozen_revision, frozen_total)
                        end = min(offset + limit, len(frozen))
                        result = {"sessions": frozen[offset:end], "remove": [], "has_more": end < len(frozen),
                                  "revision": frozen_revision, "total": frozen_total}
                        if result["has_more"]:
                            result["next_cursor"] = snapshot + ":" + str(end)
                payload = json.dumps(result, ensure_ascii=False).encode()
                metrics.append({"path": path, "query": {k: v[0] for k, v in query.items()}, "bytes": len(payload),
                                "rows": len(result.get("sessions", [])), "ms": round((time.perf_counter() - start) * 1000, 3)})
                return self.send_json(result)
        if path == "/api/v1/agents":
            return self.send_json({"sources": [{"source": "demo", "enabled": True, "accounts": [{"accountId": "example", "provider": "demo", "sessionCount": 8000}]}]})
        if path in ("/api/v1/pins", "/api/v1/mutes"):
            return self.send_json({"spaces": [], "sessions": [], "allowed": []})
        if path == "/api/v1/interactions":
            return self.send_json({"interactions": []})
        return super().do_GET()

    def websocket(self):
        if args.legacy:
            return super().websocket()
        key = self.headers.get("Sec-WebSocket-Key", "")
        accept = base64.b64encode(hashlib.sha1((key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").encode()).digest()).decode()
        self.send_response(101)
        self.send_header("Upgrade", "websocket")
        self.send_header("Connection", "Upgrade")
        self.send_header("Sec-WebSocket-Accept", accept)
        self.end_headers()
        write_lock = threading.Lock()

        def frame(op, data):
            n = len(data)
            header = bytes([0x80 | op]) + (bytes([n]) if n < 126 else bytes([126]) + struct.pack(">H", n) if n < 65536 else bytes([127]) + struct.pack(">Q", n))
            with write_lock:
                self.wfile.write(header + data)
                self.wfile.flush()

        def send(obj):
            frame(1, json.dumps(obj, ensure_ascii=False, separators=(",", ":")).encode())

        with lock:
            sockets.append(send)
        send({"type": "hello", "features": base.features() + ["sessions_list"], "protocol": 1, "min_app_build": 0,
              "server_version": "fixture", "channel": "development", "build": 1, **base.identity()})
        send({"type": "machines", "machines": base.out_machines()})
        send({"type": "sessions_list", "revision": token()})
        try:
            while True:
                header = self.rfile.read(2)
                if len(header) < 2:
                    break
                op, n = header[0] & 15, header[1] & 127
                if n == 126:
                    n = struct.unpack(">H", self.rfile.read(2))[0]
                elif n == 127:
                    n = struct.unpack(">Q", self.rfile.read(8))[0]
                mask = self.rfile.read(4) if header[1] & 128 else bytes(4)
                body = bytes(value ^ mask[i % 4] for i, value in enumerate(self.rfile.read(n)))
                if op == 8:
                    break
                if op == 9:
                    frame(10, body)
                elif op == 1:
                    message = json.loads(body)
                    if message.get("type") == "subscribe":
                        send({"type": "items", "machine_id": message.get("machine_id"), "session_id": message.get("session_id"), "items": [], "backlog": True, "has_more": False})
        except (OSError, ValueError):
            pass
        finally:
            with lock:
                sockets.remove(send)
            self.close_connection = True


if __name__ == "__main__":
    print("Apple paging fixture: 8000 runtime + 8000 personal rows, port=%d" % args.port, flush=True)
    ThreadingHTTPServer(("127.0.0.1", args.port), Handler).serve_forever()
