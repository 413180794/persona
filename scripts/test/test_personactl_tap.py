"""personactl.py tap acts only on a match a touch reaches: the app's `activate` answers "not visible" for the shell's
page behind, and the real-touch fallback asks `find` for a reachable match. Run: python3 scripts/test/test_personactl_tap.py"""
import importlib.util
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("personactl_under_test", ROOT / "scripts/app/personactl.py")
ctl = importlib.util.module_from_spec(spec)
spec.loader.exec_module(ctl)


class FakeCtl(ctl.Ctl):
    """The app's answers in order per command, no socket; `idb ui` touches recorded."""

    def __init__(self, answers):
        self.via_hub = None
        self.answers = {k: list(v) for k, v in answers.items()}
        self.calls, self.touches = [], []

    def call(self, cmd, _timeout=None, **args):
        self.calls.append((cmd, args))
        r = self.answers.get(cmd, [{"ok": True}]).pop(0) if self.answers.get(cmd) else {"ok": True}
        if not r.get("ok", True):
            raise ctl.CtlError(f"{cmd}: {r['error']}")
        return r

    def _idb(self, *args):
        self.touches.append(args)


NOT_VISIBLE = {"ok": False, "error": "not visible: 2 match, none a touch reaches (allowHidden: true acts on it anyway)"}
ROW = {"frame": [10, 100, 200, 40]}


class TapTests(unittest.TestCase):
    def test_a_match_no_touch_reaches_is_an_error_not_a_touch(self):
        c = FakeCtl({"activate": [NOT_VISIBLE, NOT_VISIBLE]})
        with self.assertRaisesRegex(ctl.CtlError, "not visible"):
            c.tap(id="agents.row")
        self.assertEqual(c.touches, [])

    def test_a_menu_item_still_fading_in_is_tried_again_once_settled(self):
        c = FakeCtl({"activate": [NOT_VISIBLE, {"ok": True, "activated": True}]})
        self.assertTrue(c.tap(label="细列表")["activated"])
        self.assertEqual([cmd for cmd, _ in c.calls], ["activate", "settle", "activate"])

    def test_the_real_touch_goes_to_a_reachable_match(self):
        c = FakeCtl({"activate": [{"ok": True, "activated": False}], "find": [{"ok": True, "element": ROW}]})
        c.tap(label="细列表", trait="button")
        self.assertEqual(c.calls[-1], ("find", {"visibleOnly": True, "label": "细列表", "trait": "button"}))
        self.assertEqual(c.touches, [("tap", 110, 120)])

    def test_no_reachable_match_for_the_real_touch_is_an_error(self):
        c = FakeCtl({"find": [{"ok": True, "element": None}]})
        with self.assertRaisesRegex(ctl.CtlError, "no match a touch reaches"):
            c.tap(hid=True, id="agents.row")
        self.assertEqual(c.touches, [])

    def test_allow_hidden_and_index_keep_the_old_lookup(self):
        for f in ({"id": "agents.row", "allowHidden": True}, {"id": "agents.row", "index": 1}):
            c = FakeCtl({"find": [{"ok": True, "element": ROW}]})
            c.tap(hid=True, **f)
            self.assertEqual(c.calls[-1], ("find", f))


if __name__ == "__main__":
    unittest.main()
