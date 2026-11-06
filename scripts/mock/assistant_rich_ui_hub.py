#!/usr/bin/env python3
"""Loopback-only assistant rich UI fixture; all choices and messages are synthetic."""
import argparse
import copy
import html
import importlib.util
import json
import os
from pathlib import Path
import signal
import sys
import threading
from http.server import ThreadingHTTPServer
from urllib.parse import unquote, urlparse


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--port", type=int, default=0)
    parser.add_argument("--port-file", type=Path, required=True)
    parser.add_argument("--timeout", type=int, default=1800)
    parser.add_argument("--watch", action="store_true", help="show only the synthetic assistant session for openWatchItem=session")
    args = parser.parse_args()
    os.environ["MOCK_TASKS"] = "1"
    source = Path(__file__).with_name("mock_hub.py")
    spec = importlib.util.spec_from_file_location("rich_ui_fixture_base", source)
    base = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = base
    spec.loader.exec_module(base)
    samples = Path(__file__).resolve().parents[2] / "schema/extension/examples/assistant"
    conversation = base.ASK1
    if args.watch:
        base.SESSIONS.clear()
        base.ASSISTANT[:] = [copy.deepcopy(session) for session in base.ASSISTANT if session["session_id"] == conversation]
        for session in base.ASSISTANT:
            session.update(state="working", status="working", quiet=False)
    lock = threading.RLock()
    state = {}
    base.TODOS = []
    old_features = base.features
    base.features = lambda: old_features() + ["assistant_views", "assistant_html_bridge"]

    def publish(event):
        with base.FAULT_LOCK:
            targets = [client["send"] for client in base.CLIENTS.values()]
        for send in targets:
            try:
                send(event)
            except OSError:
                pass

    def select_sample(name, inline=False):
        view = json.loads((samples / f"{name}.json").read_text())
        if name == "form":
            view["sections"][0]["items"][-1]["action"]["confirm"] = "确认安排这项任务？"
        now = base.ago(minutes=1)
        key = f"view_{name}"
        envelope = {"id": key, "conversation": conversation, "version": 1, "view": view,
                    "created_at": now, "updated_at": now}
        messages = []
        if inline:
            message = base.it("assistant", "text", 1, text="请选择：\n```persona-view\n" + json.dumps(view, ensure_ascii=False) + "\n```")
            message["seq"] = 1
            messages = [message]
            key = message["id"] + "/0"
            envelope["id"] = key
        feed = [] if inline else [{"id": f"feed_{name}", "conversation": conversation, "at": now,
                                  "kind": "view", "ref": key, "body": {"view": copy.deepcopy(envelope)}}]
        state.clear()
        state.update(sample=name, key=key, envelope=envelope, feed=feed, actions={}, posts=0, inline=inline)
        base.ITEMS[conversation] = messages
        base.TASK_FEED[conversation] = feed

    def page_view(page):
        root = state["envelope"]["view"]
        return root.get("pages", {}).get(page) if page else root

    def actions(view):
        for section in view.get("sections", []):
            for component in section.get("items", []):
                if component.get("action"):
                    yield component["action"]
                if component.get("button"):
                    yield component["button"]["action"]

    def chosen(action, values, client_id, page=None, kind="action", text=None):
        key = state["key"]
        record = {"key": key, "action": action, "page": page, "values": values, "client_msg_id": client_id,
                  "status": "sent", "revision": 1, "kind": kind, "version": state["envelope"]["version"], "at": base.ago()}
        if text is not None:
            record["text"] = text
        state["actions"][key] = record
        publish({"type": "assistant_view_action", "conversation": conversation, "action": record})
        view = page_view(page)
        reply = text if kind == "html" else next(a["reply"] for a in actions(view) if a["id"] == action)
        payload = {"values": values}
        if kind == "html":
            payload["html"] = True
        encoded = json.dumps(payload, ensure_ascii=False).replace("<", "\\u003c")
        body = reply + "".join(f"\n{name}：{value}" for name, value in sorted(values.items()))
        body += f'\n<persona-action view="{html.escape(key, quote=True)}" action="{html.escape(action, quote=True)}">{encoded}</persona-action>'
        previous = base.ITEMS[conversation][-1]["seq"] if base.ITEMS[conversation] else 0
        message = base.it("user", "text", 0, text=body, client_msg_id=client_id)
        message["seq"] = previous + 1
        base.ITEMS[conversation].append(message)
        publish({"type": "items", "machine_id": "server", "session_id": conversation, "items": [message], **base.span([message], previous)})
        return record

    class Handler(base.H):
        def log_message(self, *_):
            base.LOG.write(f"{self.command} {urlparse(self.path).path}\n")
            base.LOG.flush()

        def do_GET(self):
            route = urlparse(self.path).path
            with lock:
                if route == "/mock/rich/status":
                    with base.FAULT_LOCK:
                        connected = len(base.CLIENTS)
                    return self.send_json({"connected": connected, "posts": state["posts"], "answers": len(state["actions"]),
                                           "sample": state["sample"], "version": state["envelope"]["version"]})
                if route == f"/api/v1/assistant/conversations/{conversation}/feed":
                    return self.send_json({"items": state["feed"], "plans": {},
                                           "views": {} if state["inline"] else {state["key"]: state["envelope"]},
                                           "view_actions": state["actions"], "view_blocks": [state["key"]] if state["inline"] else []})
            return super().do_GET()

        def do_POST(self):
            route = urlparse(self.path).path
            with lock:
                if route.startswith("/mock/rich/sample/"):
                    name = route.rsplit("/", 1)[-1]
                    if name not in {"decision", "compare", "progress", "form", "chart", "html"}:
                        return self.send_json({"error": "unknown sample"}, 400)
                    options = json.loads(self.body() or b"{}")
                    select_sample(name, inline=options.get("inline", False))
                    return self.send_json({"ok": True})
                if route == "/mock/rich/empty":
                    self.body()
                    select_sample("decision")
                    state["feed"] = []
                    base.TASK_FEED[conversation] = []
                    return self.send_json({"ok": True})
                if route == "/mock/rich/update-silent":
                    self.body()
                    state["envelope"]["version"] += 1
                    state["envelope"]["view"]["title"] += " · 已更新"
                    return self.send_json({"ok": True})
                if route == "/mock/rich/remote-choice":
                    self.body()
                    chosen("choose-b", {}, "other-device")
                    return self.send_json({"ok": True})
                prefix = f"/api/v1/assistant/conversations/{conversation}/views/"
                if route.startswith(prefix):
                    key, _, endpoint = route[len(prefix):].rpartition("/")
                    if unquote(key) != state["key"]:
                        return self.send_json({"error": "unknown card"}, 404)
                    body = json.loads(self.body() or b"{}")
                    state["posts"] += 1
                    old = state["actions"].get(state["key"])
                    if old and not (endpoint == "html-actions" and body.get("kind") == "open"):
                        same = old["client_msg_id"] == body.get("client_msg_id")
                        return self.send_json({"action": old} if same else {"error": "already answered", "code": "already_answered", "action": old}, 200 if same else 409)
                    if body.get("expected_version") != state["envelope"]["version"]:
                        return self.send_json({"error": "card changed", "code": "view_changed", "version": state["envelope"]["version"], "view": state["envelope"]}, 409)
                    page = body.get("page")
                    view = page_view(page)
                    if endpoint == "actions":
                        action = next((a for a in actions(view) if a["id"] == body.get("action")), None)
                        if not action or "reply" in body or (action.get("confirm") and body.get("confirmed") is not True):
                            return self.send_json({"error": "invalid action or confirmation", "code": "bad_request"}, 400)
                        record = chosen(action["id"], body.get("values", {}), body["client_msg_id"], page)
                        return self.send_json({"action": record})
                    if endpoint == "html-actions":
                        component = next((c for section in view.get("sections", []) for c in section.get("items", [])
                                          if c.get("type") == "html" and c.get("key") == body.get("component_key")), None)
                        if not component or body.get("confirmed") is not True:
                            return self.send_json({"error": "invalid HTML confirmation", "code": "bad_request"}, 400)
                        if body.get("kind") == "open":
                            return self.send_json({"open": body["url"], "version": state["envelope"]["version"]})
                        return self.send_json({"action": chosen(component["key"], {}, body["client_msg_id"], page, "html", body["text"])})
            return super().do_POST()

    select_sample("decision")
    server = ThreadingHTTPServer(("127.0.0.1", args.port), Handler)
    server.daemon_threads = True
    base.PORT = server.server_port
    base.ENDPOINTS.append(base.endpoint("127.0.0.1", server.server_port, "http", "lan"))
    args.port_file.write_text(str(server.server_port))
    def stop(*_):
        raise KeyboardInterrupt
    signal.signal(signal.SIGTERM, stop)
    signal.signal(signal.SIGALRM, stop)
    signal.alarm(args.timeout)
    print(f"READY: rich UI fixture at 127.0.0.1:{server.server_port}", flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("STOPPED: rich UI fixture", flush=True)
    finally:
        server.server_close()


if __name__ == "__main__":
    main()
