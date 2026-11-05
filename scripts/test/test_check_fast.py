"""check-fast.sh runs the scripts/ unittests a change needs (#3968).

Each test builds a throwaway repository with check-fast.sh and stubs for the repository-wide checks it always picks
for a scripts/ change (the privacy scan, the extension schema test, the PR workflow test), commits a base, makes a
change on top and runs check-fast.sh --base against the base.
"""
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
PASSING = "import unittest\n\nclass T(unittest.TestCase):\n    def test_ok(self):\n        pass\n"
FAILING = "import unittest\n\nclass T(unittest.TestCase):\n    def test_broken(self):\n        self.fail('broken on purpose')\n"


@unittest.skipUnless(shutil.which("timeout") or shutil.which("gtimeout"), "check-fast.sh needs GNU timeout")
class CheckFastScriptsTests(unittest.TestCase):
    def setUp(self):
        self.repo = Path(tempfile.mkdtemp(prefix="check-fast-test-"))
        self.addCleanup(shutil.rmtree, self.repo)
        (self.repo / "scripts/test").mkdir(parents=True)
        shutil.copy(ROOT / "scripts/test/check-fast.sh", self.repo / "scripts/test/check-fast.sh")
        self.write("scripts/privacy_check.py", "print('clean')\n")
        self.write("scripts/test/test_extension_schema.py", "")
        self.write("scripts/test/pr_needs_issue_test.mjs", "")
        self.write("scripts/tools/other.py", "")
        self.write("scripts/tools/test_other.py", FAILING)  # not related to the changes below: must not run
        self.write("scripts/tools/gone.py", "")
        self.write("scripts/tools/test_gone.py", PASSING)
        self.git("init", "-q")
        self.git("add", "-A")
        self.git("-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "base")
        self.git("tag", "base")

    def write(self, path, text):
        file = self.repo / path
        file.parent.mkdir(parents=True, exist_ok=True)
        file.write_text(text)

    def git(self, *args):
        subprocess.run(["git", "-C", str(self.repo), *args], check=True, capture_output=True)

    def check_fast(self):
        run = subprocess.run(["bash", "scripts/test/check-fast.sh", "--base", "base"], cwd=self.repo,
                             capture_output=True, text=True, timeout=300)
        return run.returncode, run.stdout + run.stderr

    def test_a_changed_test_is_run_and_its_failure_fails(self):
        self.write("scripts/tools/test_new.py", FAILING)
        code, out = self.check_fast()
        self.assertEqual(1, code, out)
        self.assertIn("FAIL     scripts/tools/test_new.py", out)
        self.assertNotIn("scripts/tools/test_other.py", out)

    def test_a_changed_script_runs_its_test_from_its_directory(self):
        # sim-boot.sh is tested by test_sim_boot.py: a dash in the name is an underscore in the test's.
        # Run from scripts/tools, as CI does: a relative fixture path resolves only there.
        self.write("scripts/tools/fixture.txt", "x")
        self.write("scripts/tools/test_tool_name.py", PASSING.replace("pass", "open('fixture.txt').close()"))
        self.git("add", "scripts/tools/test_tool_name.py", "scripts/tools/fixture.txt")
        self.git("-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "test")
        self.git("tag", "-f", "base")
        self.write("scripts/tools/tool-name.sh", "echo changed\n")
        code, out = self.check_fast()
        self.assertEqual(0, code, out)
        self.assertIn("PASS     scripts/tools/test_tool_name.py", out)
        self.assertNotIn("scripts/tools/test_other.py", out)

    def test_a_shell_script_named_with_its_extension_runs_its_test(self):
        self.write("scripts/tools/db.sh", "echo changed\n")
        self.write("scripts/tools/test_db_sh.py", FAILING)
        self.git("add", "scripts/tools/test_db_sh.py")
        self.git("-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "test")
        self.git("tag", "-f", "base")
        self.write("scripts/tools/db.sh", "echo changed again\n")
        code, out = self.check_fast()
        self.assertEqual(1, code, out)
        self.assertIn("FAIL     scripts/tools/test_db_sh.py", out)

    def test_a_deleted_test_is_not_run(self):
        (self.repo / "scripts/tools/test_gone.py").unlink()
        (self.repo / "scripts/tools/gone.py").unlink()
        code, out = self.check_fast()
        self.assertEqual(0, code, out)
        self.assertNotIn("test_gone.py", out)


if __name__ == "__main__":
    unittest.main()
