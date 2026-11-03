import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch


SCRIPTS = Path(__file__).resolve().parent.parent
spec = importlib.util.spec_from_file_location("privacy_check", SCRIPTS / "privacy_check.py")
pc = importlib.util.module_from_spec(spec)
spec.loader.exec_module(pc)

PUBLIC_URL = "https://registry.npmjs.org/sample/-/sample-1.0.0.tgz"
UNAPPROVED_URL = "https://registry.example.invalid/sample/-/sample-1.0.0.tgz"
# Split so the repository scan of this file does not flag it.
PRIVATE_IP = "10." + "20.30.40"


def lockfile(source=PUBLIC_URL, version=3, link=False):
    package = {"version": "1.0.0", "resolved": source}
    if link:
        package["link"] = True
    if version == 1:
        return {"lockfileVersion": version, "dependencies": {"sample": package}}
    return {"lockfileVersion": version, "packages": {"node_modules/sample": package}}


class NpmSourceTests(unittest.TestCase):
    def test_public_registry(self):
        for url in [PUBLIC_URL, PUBLIC_URL.replace("sample/-/sample", "@scope/sample/-/sample"),
                    PUBLIC_URL.replace("registry.npmjs.org", "REGISTRY.NPMJS.ORG:443")]:
            with self.subTest(url=url):
                self.assertTrue(pc.npm_source_allowed(url))

    def test_unapproved_remote_sources(self):
        urls = [UNAPPROVED_URL, PUBLIC_URL.replace("https:", "http:"),
                PUBLIC_URL.replace("registry.npmjs.org", "registry.npmjs.org.example.invalid"),
                PUBLIC_URL.replace("registry.npmjs.org", "registry%2enpmjs.org"),
                PUBLIC_URL.replace("registry.npmjs.org", "registry.npmjs.org:8443"),
                PUBLIC_URL.replace("registry.npmjs.org", "registry.npmjs.org:invalid"),
                PUBLIC_URL.replace("registry.npmjs.org", "user:secret-placeholder" + "@" + "registry.npmjs.org"),
                PUBLIC_URL + "?token=secret-placeholder", PUBLIC_URL + "#secret-placeholder",
                "//registry.example.invalid/sample.tgz", "https://localhost/sample.tgz",
                "git+ssh://git@code.example.invalid/sample.git", "https://[invalid/sample.tgz",
                "https://registry.npmjs.org／registry.example.invalid/sample.tgz"]
        for url in urls:
            with self.subTest(url=url):
                self.assertFalse(pc.npm_source_allowed(url))
                self.assertFalse(pc.npm_source_allowed(url, link=True))

    def test_control_characters_and_backslashes(self):
        for char in ["\n", "\r", "\t", "\0", "\x7f", "\\"]:
            with self.subTest(char=repr(char)):
                self.assertFalse(pc.npm_source_allowed(PUBLIC_URL + char))
                self.assertFalse(pc.npm_source_allowed("https://" + char + "registry.npmjs.org/sample.tgz"))

    def test_relative_file_dependencies(self):
        for source in ["file:../sample", "file:vendor/sample", "file:../space%20name"]:
            with self.subTest(source=source):
                self.assertTrue(pc.npm_source_allowed(source))

    def test_workspace_links(self):
        for source in ["packages/sample", "../sample"]:
            with self.subTest(source=source):
                self.assertFalse(pc.npm_source_allowed(source))
                self.assertTrue(pc.npm_source_allowed(source, link=True))

    def test_file_sources_cannot_hide_remote_or_absolute_paths(self):
        for source in ["file://registry.example.invalid/sample", "file:///sample", "file:/sample",
                       "file:%2f%2fregistry.example.invalid/sample", "file:%5c%5cregistry.example.invalid/sample",
                       "file:%252f%252fregistry.example.invalid/sample", "file:https://registry.example.invalid/sample",
                       "file:../sample?token=secret-placeholder", "file:../sample#secret-placeholder",
                       "//registry.example.invalid/sample", "%2f%2fregistry.example.invalid/sample",
                       "file:../bad%0aname", "file:../bad%7fname"]:
            with self.subTest(source=source):
                self.assertFalse(pc.npm_source_allowed(source, link=True))

    def test_empty_or_non_string_source(self):
        for source in ["", None, False, 1, {}, []]:
            with self.subTest(source=source):
                self.assertFalse(pc.npm_source_allowed(source))


class LoopbackTests(unittest.TestCase):
    def test_loopback_values(self):
        for value in ["127.0.0.1", "127.1.2.3", "::1", "[::1]", "0:0:0:0:0:0:0:1"]:
            with self.subTest(value=value):
                self.assertTrue(pc.loopback(value))
        for value in [PRIVATE_IP, "192.0.2.1", "0.0.0.0", "127.0.0.1:18660", "hub-host", ""]:
            with self.subTest(value=value):
                self.assertFalse(pc.loopback(value))

    def test_config_loopback_is_not_a_word(self):
        with tempfile.TemporaryDirectory(prefix="privacy-check-test-") as temp:
            home = Path(temp)
            config = home / ".config/jarvis/config.json"
            config.parent.mkdir(parents=True)
            config.write_text(json.dumps({"hub": {"url": "http://127.0.0.1:18660"}, "lan_host": PRIVATE_IP,
                                          "local_ipv6": "[::1]"}))
            with patch.object(pc, "HOME", str(home)), patch.object(pc, "ROOT", str(home)):
                words, _ = pc.terms()
        self.assertNotIn("127.0.0.1", words)
        self.assertNotIn("[::1]", words)
        self.assertIn(PRIVATE_IP, words)
        self.assertEqual(pc.problems("hub at http://127.0.0.1:18660 or [::1]", words), [])
        self.assertIn(PRIVATE_IP, pc.problems("other machine at " + PRIVATE_IP, words))


class NpmLockTests(unittest.TestCase):
    def test_public_lockfiles(self):
        for version in [1, 2, 3]:
            with self.subTest(version=version):
                self.assertEqual(pc.npm_lock_problems(json.dumps(lockfile(version=version))), [])

    def test_unapproved_lockfiles(self):
        for version in [1, 2, 3]:
            with self.subTest(version=version):
                self.assertEqual(pc.npm_lock_problems(json.dumps(lockfile(UNAPPROVED_URL, version))),
                                 [(1, "unapproved npm source")])

    def test_nested_dependencies_and_both_v2_sections(self):
        nested = lockfile(version=1)
        nested["dependencies"]["sample"]["dependencies"] = {"nested": {"resolved": UNAPPROVED_URL}}
        self.assertEqual(len(pc.npm_lock_problems(json.dumps(nested))), 1)
        combined = lockfile(UNAPPROVED_URL, version=2)
        combined["dependencies"] = nested["dependencies"]
        self.assertEqual(len(pc.npm_lock_problems(json.dumps(combined))), 2)

    def test_file_and_workspace_locks(self):
        self.assertEqual(pc.npm_lock_problems(json.dumps(lockfile("file:../sample"))), [])
        self.assertEqual(pc.npm_lock_problems(json.dumps(lockfile("packages/sample", link=True))), [])
        self.assertTrue(pc.npm_lock_problems(json.dumps(lockfile("packages/sample"))))

    def test_source_line_numbers(self):
        text = json.dumps(lockfile(UNAPPROVED_URL), indent=2)
        expected = next(n for n, line in enumerate(text.splitlines(), 1) if '"resolved"' in line)
        self.assertEqual(pc.npm_lock_problems(text), [(expected, "unapproved npm source")])

    def test_escaped_keys_and_urls(self):
        text = json.dumps(lockfile(UNAPPROVED_URL))
        text = text.replace('"resolved"', '"\\u0072esolved"')
        text = text.replace("registry.example.invalid", "registry\\u002eexample\\u002einvalid")
        self.assertEqual(pc.npm_lock_problems(text), [(1, "unapproved npm source")])

    def test_strings_that_look_like_keys_do_not_change_locations(self):
        data = lockfile(UNAPPROVED_URL)
        data["description"] = '"resolved": "placeholder"'
        self.assertEqual(len(pc.npm_lock_problems(json.dumps(data))), 1)

    def test_malformed_json_and_root(self):
        for text in ["{", "[]", "null", json.dumps(lockfile()) + "\0", '{"resolved": "unterminated}',
                     '{"unused":NaN}', '{"unused":Infinity}', '{"unused":-Infinity}']:
            with self.subTest(text=text):
                self.assertEqual(pc.npm_lock_problems(text), [(1, "invalid npm lockfile")])

    def test_duplicate_keys(self):
        for text in ['{"resolved": "' + UNAPPROVED_URL + '", "resolved": "' + PUBLIC_URL + '"}',
                     '{"name":"first","name":"second"}', '{"name":"first","\\u006eame":"second"}']:
            with self.subTest(text=text):
                self.assertEqual(pc.npm_lock_problems(text), [(1, "invalid npm lockfile")])

    def test_non_string_resolved_fields(self):
        for value in [None, False, 1, {}, []]:
            with self.subTest(value=value):
                self.assertEqual(pc.npm_lock_problems(json.dumps(lockfile(value))),
                                 [(1, "invalid npm resolved field")])

    def test_nested_non_string_resolution_does_not_hide_source(self):
        text = json.dumps(lockfile({"resolved": UNAPPROVED_URL}))
        self.assertEqual(pc.npm_lock_problems(text),
                         [(1, "invalid npm resolved field"), (1, "unapproved npm source")])

    def test_recursion_failure_fails_closed(self):
        text = json.dumps(lockfile())
        with patch.object(pc.json, "loads", side_effect=RecursionError):
            self.assertEqual(pc.npm_lock_problems(text), [(1, "invalid npm lockfile")])
        with patch.object(pc, "npm_resolutions", side_effect=RecursionError):
            self.assertEqual(pc.npm_lock_problems(text), [(1, "invalid npm lockfile")])


class PrivacyCheckIntegrationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="privacy-check-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.repo = self.root / "repo"
        self.home = self.root / "home"
        self.repo.mkdir()
        self.home.mkdir()
        self.env = {k: v for k, v in os.environ.items() if not k.startswith("GIT_") and k not in {"PYTHONPATH", "PYTHONHOME"}}
        self.env.update({"HOME": str(self.home), "XDG_CONFIG_HOME": str(self.home / ".config"),
                         "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": os.devnull,
                         "GIT_AUTHOR_NAME": "Test User", "GIT_AUTHOR_EMAIL": "test@example.com",
                         "GIT_COMMITTER_NAME": "Test User", "GIT_COMMITTER_EMAIL": "test@example.com"})
        self.git("init", "-q")
        self.git("config", "core.hooksPath", "scripts/hooks")
        self.git("config", "commit.gpgSign", "false")
        self.git("config", "core.autocrlf", "false")
        (self.repo / "scripts").mkdir()
        shutil.copy2(SCRIPTS / "privacy_check.py", self.repo / "scripts/privacy_check.py")
        shutil.copytree(SCRIPTS / "hooks", self.repo / "scripts/hooks")
        (self.repo / "config.example.json").write_text("{}\n")
        self.git("add", "scripts", "config.example.json")
        self.lock = self.repo / "web/package-lock.json"
        self.lock.parent.mkdir()

    def run_command(self, *args):
        return subprocess.run(args, cwd=self.repo, env=self.env, capture_output=True, text=True, timeout=30)

    def git(self, *args):
        result = self.run_command("git", *args)
        if result.returncode:
            self.fail(result.stdout + result.stderr)
        return result

    def write_lock(self, source=PUBLIC_URL):
        self.lock.write_text(json.dumps(lockfile(source), indent=2) + "\n")

    def scan(self, *args):
        return self.run_command(sys.executable, str(self.repo / "scripts/privacy_check.py"), *args)

    def assert_blocked(self, result, kind="unapproved npm source"):
        self.assertNotEqual(result.returncode, 0)
        output = result.stdout + result.stderr
        self.assertIn(kind, output)
        self.assertNotIn(UNAPPROVED_URL, output)
        self.assertNotIn("registry.example.invalid", output)

    def test_full_scan_blocks_without_private_configuration(self):
        self.write_lock(UNAPPROVED_URL)
        self.git("add", "web/package-lock.json")
        self.assertFalse((self.home / ".config/jarvis/config.json").exists())
        self.assertFalse((self.home / ".config/jarvis/private-words.txt").exists())
        self.assert_blocked(self.scan())

    def test_public_full_scan_passes(self):
        self.write_lock()
        self.git("add", "web/package-lock.json")
        result = self.scan()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_config_loopback_is_not_flagged_but_private_ip_is(self):
        config = self.home / ".config/jarvis/config.json"
        config.parent.mkdir(parents=True)
        config.write_text(json.dumps({"hub": {"url": "http://127.0.0.1:18660"}, "lan_host": PRIVATE_IP}))
        (self.repo / "notes.md").write_text("The hub listens on 127.0.0.1:18660.\n")
        self.git("add", "notes.md")
        result = self.scan()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        (self.repo / "hosts.md").write_text("The other machine is " + PRIVATE_IP + ".\n")
        self.git("add", "hosts.md")
        result = self.scan()
        self.assertNotEqual(result.returncode, 0)
        output = result.stdout + result.stderr
        self.assertIn("hosts.md:1: private IP", output)
        self.assertNotIn("notes.md", output)
        self.assertNotIn(PRIVATE_IP, output)

    def test_staged_unsafe_worktree_safe(self):
        self.write_lock(UNAPPROVED_URL)
        self.git("add", "web/package-lock.json")
        self.write_lock()
        self.assert_blocked(self.scan("--staged"))
        result = self.scan()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_staged_safe_worktree_unsafe(self):
        self.write_lock()
        self.git("add", "web/package-lock.json")
        self.write_lock(UNAPPROVED_URL)
        result = self.scan("--staged")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assert_blocked(self.scan())

    def test_nul_lockfiles_are_not_skipped_as_binary(self):
        self.write_lock()
        with self.lock.open("ab") as file:
            file.write(b"\0")
        self.git("add", "web/package-lock.json")
        self.assert_blocked(self.scan(), "invalid npm lockfile")
        self.assert_blocked(self.scan("--staged"), "invalid npm lockfile")

    def test_invalid_utf8_lockfile_is_rejected(self):
        self.write_lock()
        data = self.lock.read_bytes().replace(b'"version": "1.0.0"', b'"version": "\xff"')
        self.lock.write_bytes(data)
        self.git("add", "web/package-lock.json")
        self.assert_blocked(self.scan(), "invalid npm lockfile")
        self.assert_blocked(self.scan("--staged"), "invalid npm lockfile")

    def test_shrinkwrap_in_another_directory_is_checked(self):
        self.lock = self.repo / "packages/sample/npm-shrinkwrap.json"
        self.lock.parent.mkdir(parents=True)
        self.write_lock(UNAPPROVED_URL)
        self.git("add", "packages")
        self.assert_blocked(self.scan())
        self.assert_blocked(self.scan("--staged"))

    def test_pre_commit_blocks_index_despite_safe_worktree(self):
        self.write_lock(UNAPPROVED_URL)
        self.git("add", "web/package-lock.json")
        self.write_lock()
        result = self.run_command("git", "commit", "-m", "Test dependency source")
        self.assert_blocked(result)
        self.assertNotEqual(self.run_command("git", "rev-parse", "--verify", "HEAD").returncode, 0)

    def test_pre_commit_and_commit_msg_allow_public_lockfile(self):
        self.write_lock()
        self.git("add", "web/package-lock.json")
        self.git("commit", "-m", "Test public dependency source")

    def test_commit_msg_also_checks_staged_lockfile(self):
        self.write_lock(UNAPPROVED_URL)
        self.git("add", "web/package-lock.json")
        self.write_lock()
        message = self.repo / ".git/test-message"
        message.write_text("Test dependency source\n")
        result = self.run_command(str(self.repo / "scripts/hooks/commit-msg"), str(message))
        self.assert_blocked(result)

    def test_commit_msg_keeps_private_word_check_and_redaction(self):
        self.write_lock()
        self.git("add", "web/package-lock.json")
        words = self.home / ".config/jarvis/private-words.txt"
        words.parent.mkdir(parents=True)
        marker = "private-marker-example"
        words.write_text(marker + "\n")
        result = self.run_command("git", "commit", "-m", "Test " + marker)
        self.assertNotEqual(result.returncode, 0)
        output = result.stdout + result.stderr
        self.assertIn("commit message:1: private value", output)
        self.assertNotIn(marker, output)

    @unittest.skipUnless(shutil.which("npm"), "npm unavailable")
    def test_web_prebuild_blocks_unapproved_lockfile(self):
        shutil.copy2(SCRIPTS.parent / "web/package.json", self.repo / "web/package.json")
        self.write_lock(UNAPPROVED_URL)
        self.git("add", "web")
        result = self.run_command("npm", "--prefix", "web", "run", "prebuild")
        self.assert_blocked(result)


class URLTermTests(unittest.TestCase):
    def test_a_shared_host_keeps_the_path(self):
        self.assertEqual(pc.url_term("https://docs.google.com/document/d/abc123/edit?usp=sharing"),
                         "docs.google.com/document/d/abc123/edit")
        self.assertEqual(pc.url_term("https://docs.google.com/"), "")

    def test_an_own_host_is_the_host(self):
        self.assertEqual(pc.url_term("https://hub.example.test:18660/path"), "hub.example.test")
        self.assertEqual(pc.url_term("someone.github.io/privacy"), "someone.github.io")


if __name__ == "__main__":
    unittest.main()
