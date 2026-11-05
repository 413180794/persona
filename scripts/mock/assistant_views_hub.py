#!/usr/bin/env python3
"""Loopback-only rich-view fixture using the shared assistant examples.

python3 scripts/mock/assistant_views_hub.py --port 0 --port-file /tmp/view-hub.port
The mock has no real accounts, outbound actions or production conversations.
"""
import argparse
import copy
import importlib.util
import json
import os
from pathlib import Path
import signal
import sys
from http.server import ThreadingHTTPServer
from urllib.parse import urlparse


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--port", type=int, default=0)
    parser.add_argument("--port-file", type=Path)
    parser.add_argument("--timeout", type=int, default=1800)
    args = parser.parse_args()
    if not 0 <= args.port <= 65535 or args.timeout <= 0:
        parser.error("port must be 0–65535 and timeout must be positive")

    os.environ["MOCK_TASKS"] = "1"
    path = Path(__file__).with_name("mock_hub.py")
    spec = importlib.util.spec_from_file_location("assistant_view_mock_base", path)
    base = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = base
    spec.loader.exec_module(base)
    samples = Path(__file__).resolve().parents[2] / "schema/extension/examples/assistant"
    initial = json.loads((samples / "progress.json").read_text())
    updated = json.loads((samples / "progress-updated.json").read_text())
    conversation = base.ASK1
    now = base.ago()
    original = {"id": "view_progress", "conversation": conversation, "version": 1, "view": initial,
                "created_at": now, "updated_at": now}
    latest = copy.deepcopy(original)
    feed = {"id": "feed_view_progress", "conversation": conversation, "at": now,
            "kind": "view", "ref": original["id"], "body": {"view": original}}
    base.TODOS = []
    base.TASK_FEED[conversation] = [feed]
    features = base.features
    base.features = lambda: features() + ["assistant_views"]

    def publish(view):
        event = {"type": "assistant_view", "conversation": conversation, "view": view}
        with base.FAULT_LOCK:
            targets = [client["send"] for client in base.CLIENTS.values()]
        for send in targets:
            try:
                send(event)
            except OSError:
                pass

    class Handler(base.H):
        def do_GET(self):
            route = urlparse(self.path).path
            if route == "/mock/views/status":
                with base.FAULT_LOCK:
                    connected = len(base.CLIENTS)
                return self.send_json({"connected": connected})
            if route == f"/api/v1/assistant/conversations/{conversation}/feed":
                return self.send_json({"items": [feed], "plans": {}, "views": {latest["id"]: latest}})
            return super().do_GET()

        def do_POST(self):
            nonlocal latest
            route = urlparse(self.path).path
            if route == "/mock/views/update":
                latest = dict(original, version=2, view=updated, updated_at=base.ago())
                publish(latest)
                return self.send_json({"ok": True, "version": 2})
            if route == "/mock/views/stale":
                publish(original)
                return self.send_json({"ok": True, "version": 1})
            if route == "/mock/views/reset":
                latest = copy.deepcopy(original)
                return self.send_json({"ok": True})
            return super().do_POST()

    server = ThreadingHTTPServer(("127.0.0.1", args.port), Handler)
    server.daemon_threads = True
    base.PORT = server.server_port
    base.ENDPOINTS.append(base.endpoint("127.0.0.1", server.server_port, "http", "lan"))
    if args.port_file:
        args.port_file.write_text(str(server.server_port))

    def stop(*_):
        raise KeyboardInterrupt

    signal.signal(signal.SIGTERM, stop)
    signal.signal(signal.SIGALRM, stop)
    signal.alarm(args.timeout)
    print(f"READY: assistant view mock at 127.0.0.1:{server.server_port}", flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("STOPPED: assistant view mock", flush=True)
    finally:
        server.server_close()


if __name__ == "__main__":
    main()
