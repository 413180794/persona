"""clitest.py keeps what a failed run showed (snapshot, tree, state, why) in CLITEST_OUT/<time>/<case>-<run>/, and still
finishes when the app cannot answer. Run: python3 scripts/test/test_clitest_dump.py"""
import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("clitest_under_test", ROOT / "scripts/app/cli-tests/clitest.py")
clitest = importlib.util.module_from_spec(spec)
spec.loader.exec_module(clitest)


class FakeCtl:
    """The app's answers, no socket. Once `crashed`, every call fails as a dead app's does."""

    def __init__(self):
        self.via_hub = None
        self.crashed = False

    def call(self, cmd, _timeout=None, **args):
        if self.crashed:
            raise OSError("Connection refused")
        if cmd == "snapshot":
            Path(args["path"]).write_bytes(b"\x89PNG fixture")
            return {"ok": True, "path": args["path"]}
        if cmd == "tree":
            return {"ok": True, "elements": [{"id": "agents.row", "label": "完成未看、platform", "visible": False}]}
        return {"ok": True, "appState": "active", "tab": "chats"}


def passes(t):
    pass


def row_is_gone(t):
    raise clitest.Fail("the row is gone")


def app_crashes(t):
    t.c.crashed = True
    t.c.call("find")


class DumpTests(unittest.TestCase):
    def run_cases(self, cases):
        out = tempfile.TemporaryDirectory()
        self.addCleanup(out.cleanup)
        with patch.object(clitest, "Ctl", FakeCtl), patch.dict(os.environ, {"CLITEST_OUT": out.name}), \
                contextlib.redirect_stdout(io.StringIO()) as printed:
            ok = clitest.run(cases, setup=lambda t: None, runs=1, launch=lambda c: None)
        runs = list(Path(out.name).iterdir())
        self.assertLessEqual(len(runs), 1)  # this run's own <time> directory, made at the first failure
        return ok, runs[0] if runs else None, printed.getvalue()

    def test_a_failed_run_leaves_the_screen_and_the_reason(self):
        ok, out, printed = self.run_cases([passes, row_is_gone])
        self.assertFalse(ok)
        self.assertEqual(sorted(p.name for p in out.iterdir()), ["row_is_gone-1"])
        d = out / "row_is_gone-1"
        self.assertEqual(sorted(p.name for p in d.iterdir()), ["snapshot.png", "state.json", "tree.json", "why.txt"])
        self.assertIn("the row is gone", (d / "why.txt").read_text())
        self.assertIn("row_is_gone", (d / "why.txt").read_text())  # the traceback: where the script stopped
        self.assertEqual(json.loads((d / "tree.json").read_text())[0]["id"], "agents.row")
        self.assertIn(str(d), printed)

    def test_an_app_that_cannot_answer_still_leaves_the_reason(self):
        ok, out, _ = self.run_cases([app_crashes])
        d = out / "app_crashes-1"
        self.assertFalse(ok)
        self.assertEqual(sorted(p.name for p in d.iterdir()), ["dump-errors.txt", "why.txt"])
        self.assertIn("Connection refused", (d / "why.txt").read_text())
        self.assertEqual(len((d / "dump-errors.txt").read_text().splitlines()), 3)

    def test_passing_runs_leave_nothing(self):
        ok, out, _ = self.run_cases([passes])
        self.assertTrue(ok)
        self.assertIsNone(out)


if __name__ == "__main__":
    unittest.main()
