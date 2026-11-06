#!/usr/bin/env python3
"""Issue #5925's isolated, synthetic hub fixture; the shared mock stays unchanged."""
import importlib.util
import json
import os
from pathlib import Path
import signal
from http.server import ThreadingHTTPServer
from urllib.parse import urlparse

ROOT = Path(__file__).resolve().parents[3]
spec = importlib.util.spec_from_file_location("persona_model_mock", ROOT / "scripts/mock/mock_hub.py")
mock = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mock)

PARENT = {"revision": 11, "override": {"provider": "launcher.claude", "model": "agents-default", "effort": "medium"}}
mock.MODEL_BINDINGS["agent.new"] = PARENT
STATE = {"revision": 0, "override": {}}
WRITES = []
BASE_CATALOG = mock.model_configuration
mock.LAUNCHER_MODELS["demo"] = {"agent": "claude", "models": [
    {"id": "example-model", "name": "示例模型", "efforts": ["low", "medium", "high"]}
]}


def catalog(include_uses=False):
    result = BASE_CATALOG(include_uses=include_uses)
    result["providers"] += [
        {"id": "launcher.demo", "name": "示例启动器", "kind": "launcher", "model": "example-model"},
        {"id": "api.messages", "name": "示例 Messages API", "kind": "api", "api": "messages", "model": "api-default"},
    ]
    parent = next(p for p in result["purposes"] if p["id"] == "agent.new")
    binding = dict(STATE["override"])
    effective = dict(parent["effective"])
    if binding.get("provider"):
        provider = next(p for p in result["providers"] if p["id"] == binding["provider"])
        effective = {"provider": provider["id"], "model": provider.get("model", "")}
    effective.update(binding)
    purpose = {"id": "lark.agent", "category_id": "agent.new", "parent": "agent.new", "name": "飞书 /agent",
               "group": "Agents", "kind": "launcher", "override": binding, "effective": effective,
               "revision": STATE["revision"], "fields": ["provider", "model", "effort"],
               "provider_ids": ["launcher.claude", "launcher.demo", "api.messages"]}
    if not binding:
        purpose["inherited_from"] = "Agents"
    result["purposes"] = [p for p in result["purposes"] if p["id"] != "lark.agent"] + [purpose]
    return result


mock.model_configuration = catalog


class Handler(mock.H):
    def do_GET(self):
        path = urlparse(self.path).path
        if path == "/api/v1/model-config/providers/launcher.demo/models":
            return self.send_json(mock.LAUNCHER_MODELS["demo"])
        if path == "/mock/lark-agent":
            with mock.MODEL_CONFIG_LOCK:
                return self.send_json({"writes": WRITES, "parent": mock.MODEL_BINDINGS["agent.new"], "state": STATE})
        return super().do_GET()

    def do_POST(self):
        if urlparse(self.path).path == "/mock/lark-agent/reset":
            self.body()
            with mock.MODEL_CONFIG_LOCK:
                STATE.update(revision=0, override={})
                WRITES.clear()
            return self.send_json({"ok": True})
        return super().do_POST()

    def do_PATCH(self):
        if urlparse(self.path).path != "/api/v1/model-config/purposes/lark.agent":
            return super().do_PATCH()
        body = json.loads(self.body() or b"{}")
        with mock.MODEL_CONFIG_LOCK:
            if body.get("revision") != STATE["revision"]:
                return self.send_json({"error": "配置已在别处修改"}, 409)
            if set(body) - {"revision", "provider", "model", "effort"}:
                return self.send_json({"error": "unsupported field"}, 400)
            binding = dict(STATE["override"])
            if "provider" in body and body["provider"] != binding.get("provider"):
                binding.pop("model", None)
                binding.pop("effort", None)
            for field in ("provider", "model", "effort"):
                if field not in body:
                    continue
                if body[field] is None:
                    binding.pop(field, None)
                else:
                    binding[field] = body[field]
            WRITES.append(body)
            STATE.update(revision=STATE["revision"] + 1, override=binding)
            result = catalog()
            purpose = next(p for p in result["purposes"] if p["id"] == "lark.agent")
            return self.send_json({"purpose": purpose, "providers": result["providers"]})


if __name__ == "__main__":
    port = int(os.environ.get("PORT", "18760"))
    with ThreadingHTTPServer(("127.0.0.1", port), Handler) as server:
        server.daemon_threads = True
        mock.PORT = server.server_port
        mock.ENDPOINTS.append(mock.endpoint("127.0.0.1", server.server_port, "http", "lan"))
        signal.signal(signal.SIGTERM, lambda *_: (_ for _ in ()).throw(KeyboardInterrupt()))
        print("lark-agent fixture ready on port %d" % server.server_port, flush=True)
        try:
            server.serve_forever()
        except KeyboardInterrupt:
            pass
