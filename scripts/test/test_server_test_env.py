"""Server-test launcher isolation with synthetic profiles, SSH, resolver and Go."""
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location("server_test_env", ROOT / "scripts/test/server_test_env.py")
ISOLATION = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(ISOLATION)


class ServerTestEnvironmentTests(unittest.TestCase):
    def test_database_url_targets_only_the_dedicated_test_database(self):
        with tempfile.TemporaryDirectory() as directory:
            config = Path(directory) / "hub.json"
            for source, expected in (
                ("postgres://user:synthetic@localhost:5432/jarvis?sslmode=disable", "postgres://user:synthetic@localhost:5432/jarvis_test?sslmode=disable"),
                ("postgresql://user:synthetic@localhost/jarvis", "postgresql://user:synthetic@localhost/jarvis_test"),
                ("postgres://user:p%40ss@[::1]:5432/jarvis?sslmode=require", "postgres://user:p%40ss@[::1]:5432/jarvis_test?sslmode=require"),
            ):
                with self.subTest(source=source):
                    config.write_text(json.dumps({"database_url": source}))
                    self.assertEqual(ISOLATION.test_database_url(config), expected)
                    self.assertNotEqual(source, expected)
            for source in (
                "postgres://user:synthetic@localhost/custom", "postgres://user:synthetic@localhost/jarvis_test",
                "postgres://user:synthetic@localhost/%6aarvis", "postgres://user:synthetic@localhost/jarvis/",
                "postgres://user:synthetic@localhost/jarvis#fragment", "https://example.com/jarvis", "postgres:///jarvis", 42, None,
            ):
                with self.subTest(source=source):
                    config.write_text(json.dumps({"database_url": source}))
                    with mock.patch.object(ISOLATION.subprocess, "run") as run:
                        with self.assertRaises(ValueError):
                            ISOLATION.run_tests(config, ["go", "test"], {})
                        run.assert_not_called()

    def test_go_test_environment_has_no_runtime_profile_and_is_cleaned_up(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            profile = root / "live-profile"
            config = root / "hub.json"
            config.write_text(json.dumps({"database_url": "postgres://user:synthetic@localhost/jarvis"}))
            environment = {
                "HOME": str(root / "real-home"), "PATH": str(profile / "bin") + os.pathsep + "/usr/bin",
                "PERSONA_HOME": str(profile), "JARVIS_HOME": str(profile),
                "PERSONA_CONFIG": str(profile / "config.json"), "JARVIS_CONFIG": str(profile / "old.json"),
                "PERSONA_TOKEN": "synthetic-live-token", "JARVIS_DATABASE_URL": "postgres://example.invalid/production",
                "PERSONA_TEST_DATABASE_URL": "postgres://example.invalid/not_test", "JARVIS_TEST_DATABASE_URL": "invalid",
                "PERSONA_OTHER_SETTING": "synthetic", "JARVIS_OTHER_SETTING": "synthetic",
                "CLAUDE_CONFIG_DIR": str(profile / "claude"), "CODEX_HOME": str(profile / "codex"),
                "XDG_CONFIG_HOME": str(profile / "xdg"), "ZDOTDIR": str(profile / "shell"),
                "BASH_ENV": str(profile / "shellrc"), "ENV": str(profile / "envrc"), "GOTOOLCHAIN": "local",
            }
            caches = {key: str(root / key.lower()) for key in ("GOCACHE", "GOMODCACHE", "GOPATH")}
            homes = []

            def run(command, **kwargs):
                child = kwargs["env"]
                if command[:2] == ["go", "env"]:
                    self.assertFalse(any(key.startswith(("PERSONA_", "JARVIS_")) for key in child))
                    return subprocess.CompletedProcess(command, 0, json.dumps(caches))
                homes.append(Path(child["HOME"]))
                self.assertTrue(homes[-1].is_dir())
                self.assertNotEqual(child["HOME"], environment["HOME"])
                self.assertTrue(Path(child["TMPDIR"]).is_dir())
                self.assertEqual(Path(child["HOME"]), Path(child["HOME"]).resolve())
                self.assertEqual({key for key in child if key.startswith(("PERSONA_", "JARVIS_"))},
                                 {"PERSONA_TEST_DATABASE_URL", "JARVIS_TEST_DATABASE_URL"})
                self.assertEqual(child["PERSONA_TEST_DATABASE_URL"], child["JARVIS_TEST_DATABASE_URL"])
                self.assertTrue(child["PERSONA_TEST_DATABASE_URL"].endswith("/jarvis_test"))
                self.assertFalse(any(str(profile) in value for value in child.values()))
                self.assertEqual(child["GOTOOLCHAIN"], "local")
                for key, value in caches.items():
                    self.assertEqual(child[key], value)
                return subprocess.CompletedProcess(command, 23)

            with mock.patch.object(ISOLATION.subprocess, "run", side_effect=run):
                self.assertEqual(ISOLATION.run_tests(config, ["go", "test", "./..."], environment), 23)
            self.assertEqual(len(homes), 1)
            self.assertFalse(homes[0].exists())

    def test_cache_failure_never_starts_the_test_command(self):
        with tempfile.TemporaryDirectory() as directory:
            config = Path(directory) / "hub.json"
            config.write_text(json.dumps({"database_url": "postgres://user:synthetic@localhost/jarvis"}))
            for result in (subprocess.CompletedProcess(["go", "env"], 1, "synthetic-private-output"),
                           subprocess.CompletedProcess(["go", "env"], 0, "not-json"),
                           subprocess.CompletedProcess(["go", "env"], 0, "{}")):
                with self.subTest(result=result.returncode, output=result.stdout):
                    with mock.patch.object(ISOLATION.subprocess, "run", return_value=result) as run:
                        with self.assertRaises(ValueError):
                            ISOLATION.run_tests(config, ["go", "test"], {"PERSONA_TOKEN": "synthetic-token"})
                        self.assertEqual(run.call_count, 1)
                        self.assertEqual(run.call_args.args[0][:2], ["go", "env"])
                        self.assertNotIn("PERSONA_TOKEN", run.call_args.kwargs["env"])

    def test_actual_shell_launcher_with_fake_remote_and_go(self):
        with tempfile.TemporaryDirectory(prefix="server-launcher-test-") as directory:
            root = Path(directory).resolve()
            checkout = root / "checkout"
            tools = root / "tools"
            remote = root / "remote"
            profile = remote / "live-profile"
            for path in (checkout / "scripts/test", tools, profile):
                path.mkdir(parents=True)
            shutil.copy2(ROOT / "scripts/test/test-server.sh", checkout / "scripts/test/test-server.sh")
            shutil.copy2(ROOT / "scripts/test/server_test_env.py", checkout / "scripts/test/server_test_env.py")
            (checkout / "scripts/jarvis_config.py").write_text("print('export JARVIS_HUB_SSH=fake-hub')\n")
            (checkout / "scripts/persona_identity.py").write_text(
                "import shlex,sys\n"
                f"root={str(profile)!r}\n"
                "print('export PERSONA_HOME='+shlex.quote(root)+' JARVIS_HOME='+shlex.quote(root))\n"
                "if '--prepare' in sys.argv: print('test -f \"$PERSONA_HOME/hub.json\"')\n")
            (profile / "hub.json").write_text(json.dumps({"database_url": "postgres://user:synthetic@localhost/jarvis"}))
            capture = root / "go-environment.json"
            cache_capture = root / "go-cache-environment.json"
            caches = {key: str(root / key.lower()) for key in ("GOCACHE", "GOMODCACHE", "GOPATH")}

            def tool(name, body):
                path = tools / name
                path.write_text("#!" + sys.executable + "\n" + body)
                path.chmod(0o755)

            tool("ssh", "import os,subprocess,sys\n"
                 f"env=os.environ.copy(); env['HOME']={str(remote)!r}\n"
                 "command=' '.join(sys.argv[2:])\n"
                 # Map the remote toolchain directory to the synthetic tool directory, not the host's real Go.
                 f"command=command.replace('/usr/local/go/bin', {str(tools)!r})\n"
                 "sys.exit(subprocess.run(['bash','-c',command],env=env).returncode)\n")
            tool("rsync", "import os,pathlib,shutil,sys\n"
                 "source=pathlib.Path(sys.argv[-2]); target=sys.argv[-1].split(':',1)[1]\n"
                 "destination=pathlib.Path(os.environ['HOME'])/target\n"
                 "if source.is_file():\n destination.parent.mkdir(parents=True,exist_ok=True); shutil.copy2(source,destination)\n"
                 "else:\n destination.mkdir(parents=True,exist_ok=True)\n")
            tool("go", "import json,os,pathlib,sys\n"
                 "if sys.argv[1:2]==['env']:\n"
                 f" pathlib.Path({str(cache_capture)!r}).write_text(json.dumps(dict(os.environ)))\n"
                 f" print(json.dumps({caches!r})); sys.exit(0)\n"
                 f"pathlib.Path({str(capture)!r}).write_text(json.dumps({{'args':sys.argv[1:],'env':dict(os.environ)}}))\n"
                 "sys.exit(int(os.environ.get('FAKE_GO_EXIT','0')))\n")
            environment = {
                "PATH": str(tools) + os.pathsep + os.environ["PATH"], "HOME": str(remote),
                "PERSONA_HOME": str(profile), "JARVIS_HOME": str(profile),
                "PERSONA_CONFIG": str(profile / "production-config.json"), "JARVIS_TOKEN": "synthetic-live-token",
                "PERSONA_DATABASE_URL": "postgres://example.invalid/production", "JARVIS_SOMETHING_NEW": "synthetic",
                "PERSONA_TEST_DATABASE_URL": "postgres://example.invalid/not_test", "JARVIS_TEST_DATABASE_URL": "invalid",
                "CLAUDE_CONFIG_DIR": str(profile / "claude"), "CODEX_HOME": str(profile / "codex"),
            }
            for args, expected, extra in (
                ([], ["test", "-count=1", "-p", "1", "./..."], {}),
                (["-run", "TestFixture", "./internal/example"], ["test", "-count=1", "-p", "1", "-run", "TestFixture", "./internal/example"], {}),
                ([], ["test", "-count=1", "-p", "1", "./..."], {"FAKE_GO_EXIT": "19"}),
            ):
                with self.subTest(args=args, extra=extra):
                    result = subprocess.run(["bash", str(checkout / "scripts/test/test-server.sh"), *args],
                                            env={**environment, **extra}, capture_output=True, text=True, timeout=30)
                    self.assertEqual(result.returncode, int(extra.get("FAKE_GO_EXIT", "0")), result.stderr)
                    recorded = json.loads(capture.read_text())
                    child = recorded["env"]
                    self.assertEqual(recorded["args"], expected)
                    self.assertEqual({key for key in child if key.startswith(("PERSONA_", "JARVIS_"))},
                                     {"PERSONA_TEST_DATABASE_URL", "JARVIS_TEST_DATABASE_URL"})
                    self.assertEqual(child["PERSONA_TEST_DATABASE_URL"], "postgres://user:synthetic@localhost/jarvis_test")
                    self.assertEqual(child["JARVIS_TEST_DATABASE_URL"], child["PERSONA_TEST_DATABASE_URL"])
                    self.assertNotEqual(child["HOME"], str(remote))
                    self.assertFalse(Path(child["HOME"]).exists())
                    self.assertFalse(any(str(profile) in value for value in child.values()))
                    cache_env = json.loads(cache_capture.read_text())
                    self.assertFalse(any(key.startswith(("PERSONA_", "JARVIS_")) for key in cache_env))
                    self.assertNotIn("synthetic-live-token", result.stdout + result.stderr)
                    self.assertNotIn("postgres://", result.stdout + result.stderr)
            capture.unlink()
            cache_capture.unlink()
            (profile / "hub.json").write_text(json.dumps({"database_url": "postgres://user:synthetic@localhost/custom"}))
            result = subprocess.run(["bash", str(checkout / "scripts/test/test-server.sh")], env=environment,
                                    capture_output=True, text=True, timeout=30)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse(capture.exists())
            self.assertFalse(cache_capture.exists())
            self.assertNotIn("postgres://", result.stdout + result.stderr)
            (profile / "hub.json").unlink()
            result = subprocess.run(["bash", str(checkout / "scripts/test/test-server.sh")], env=environment,
                                    capture_output=True, text=True, timeout=30)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse(capture.exists())


if __name__ == "__main__":
    unittest.main()
