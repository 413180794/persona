"""quota-relay's sidebar page (#4576) drawn from the extension's exported fixture, for client screenshots only.

The fixture (the extension's exported `relay-accounts-view.json` in its own repo, copied to
`apple/PersonaTests/Fixtures/`) holds three Views made from made-up accounts: `default`, `kiro` (the Kiro type picked)
and `detail` (one account's page). Nothing is computed here: the right View is sent as it is, with the search's text and
the picked status echoed so that the app shows what was picked.
"""
import copy
import json
from urllib.parse import urlparse, parse_qs

EXTENSION = "quota-relay"
UI = "sidebar"
ENTRY = {"plugin": EXTENSION, "extension": EXTENSION, "plugin_name": "中转额度", "extension_name": "中转额度",
         "id": UI, "title": "中转账号", "placement": "nav_page", "size": "full"}
PATHS = ("/api/v1/plugins/%s/-/ui/%s" % (EXTENSION, UI), "/api/v1/extensions/%s/-/ui/%s" % (EXTENSION, UI))
LISTS = ("/api/v1/plugins/-/ui", "/api/v1/extensions/-/ui")


class _Listing:
    """The handler, with the sidebar page added to the slots it lists."""

    def __init__(self, handler, placement):
        self._h, self._placement = handler, placement

    def __getattr__(self, name):
        return getattr(self._h, name)

    def send_json(self, obj, *args, **kwargs):
        if isinstance(obj, dict) and isinstance(obj.get("ui"), list) and self._placement in ("", "nav_page"):
            obj = dict(obj, ui=obj["ui"] + [dict(ENTRY)])
        return self._h.send_json(obj, *args, **kwargs)


class RelayAccountsMock:
    def __init__(self, fixture_path, start="default"):
        # start: the View the page's first render gets (default, kiro or detail), so a screenshot needs no taps.
        self.start = start or "default"
        with open(fixture_path, encoding="utf-8") as f:
            self.views = json.load(f)
        for name in ("default", "kiro", "detail"):
            if not isinstance(self.views.get(name), dict):
                raise ValueError("fixture has no %r view" % name)
        if self.start not in ("default", "kiro", "detail"):
            raise ValueError("start must be default, kiro or detail, not %r" % self.start)

    def wrap(self, handler, path):
        """The handler to pass on: for the slots' list, one that adds the sidebar page; else the handler itself."""
        if path in LISTS and handler.command == "GET":
            placement = parse_qs(urlparse(getattr(handler, "path", "")).query).get("placement", [""])[0]
            return _Listing(handler, placement)
        return handler

    def answer(self, request):
        """The ActionResult for a render (no action) or an action, as the extension would answer it."""
        action = request.get("action") or ""
        values = request.get("values") or {}
        try:
            state = json.loads(request.get("state") or "{}")
        except ValueError:
            state = {}
        if not isinstance(state, dict):
            state = {}
        if not action and not state:
            return {"view": copy.deepcopy(self.views[self.start])}
        if action.startswith("account:") or (not action and state.get("a")):
            return {"view": copy.deepcopy(self.views["detail"])}
        kiro = values.get("type") == "kiro" if action == "type" else state.get("type") == "kiro"
        view = copy.deepcopy(self.views["kiro" if kiro else "default"])
        if action == "search" and view.get("search") is not None:
            view["search"]["value"] = values.get("q", "")
        status = values.get("status")
        if status and view.get("filter") and any(c.get("value") == status for c in view["filter"].get("choices", [])):
            view["filter"]["value"] = status
        result = {"view": view}
        if action == "refresh":
            result["toast"] = "已刷新"
        return result

    def route(self, handler, path, body):
        if path not in PATHS or handler.command != "POST":
            return False
        try:
            request = json.loads(body or b"{}")
        except ValueError:
            request = {}
        handler.send_json(self.answer(request if isinstance(request, dict) else {}))
        return True
