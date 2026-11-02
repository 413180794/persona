import gzip
import hashlib
import importlib.util
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


SCRIPTS = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location("check_asset_sizes", SCRIPTS / "check_asset_sizes.py")
assets = importlib.util.module_from_spec(spec)
spec.loader.exec_module(assets)


class InstallCompressedTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="jarvis-install-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.home = self.root / "home"
        self.downloads = self.root / "downloads"
        self.tools = self.root / "tools"
        for directory in (self.home, self.downloads, self.tools):
            directory.mkdir()
        self.script("curl", '#!/bin/sh\n[ "$1" = "-fsSL" ] || exit 1\ncp "$TEST_DOWNLOADS/${2##*/}" "$4"\n')
        self.script("uname", '#!/bin/sh\ncase "$1" in -s) printf "%s\\n" "$TEST_OS";; -m) printf "%s\\n" "$TEST_ARCH";; *) exit 1;; esac\n')
        self.env = {"PATH": str(self.tools) + os.pathsep + os.defpath, "HOME": str(self.home),
                    "TEST_DOWNLOADS": str(self.downloads), "TEST_OS": "Linux", "TEST_ARCH": "x86_64",
                    "JARVIS_DL": "https://example.invalid/dl"}
        self.binary = b"#!/bin/sh\nprintf '%s\\n' test-version\n"
        self.installed = self.home / ".jarvis/bin/jarvisd"
        self.installed.parent.mkdir(parents=True)
        self.installed.write_bytes(b"previous-install")

    def script(self, name, text):
        path = self.tools / name
        path.write_text(text)
        path.chmod(0o755)

    def artifact(self, name="jarvisd-linux-amd64.gz", data=None):
        data = gzip.compress(self.binary, mtime=0) if data is None else data
        path = self.downloads / name
        path.write_bytes(data)
        (self.downloads / "SHA256SUMS").write_text(hashlib.sha256(data).hexdigest() + "  " + name + "\n")
        return path

    def run_install(self):
        return subprocess.run(["/bin/sh", str(SCRIPTS / "install.sh")], env=self.env,
                              cwd=self.root, capture_output=True, text=True, timeout=30)

    def assert_keeps_previous(self, result, message):
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(message, result.stderr)
        self.assertEqual(self.installed.read_bytes(), b"previous-install")

    def test_all_platforms_install_the_matching_compressed_binary(self):
        platforms = [("Linux", "x86_64", "linux-amd64"), ("Linux", "aarch64", "linux-arm64"),
                     ("Darwin", "x86_64", "darwin-amd64"), ("Darwin", "arm64", "darwin-arm64")]
        for os_name, arch, suffix in platforms:
            with self.subTest(platform=suffix):
                self.env.update({"TEST_OS": os_name, "TEST_ARCH": arch})
                self.artifact("jarvisd-" + suffix + ".gz")
                result = self.run_install()
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertEqual(self.installed.read_bytes(), self.binary)
                self.assertTrue(os.access(self.installed, os.X_OK))
                self.assertIn("test-version installed", result.stdout)

    def test_corrupted_download_does_not_replace_the_installation(self):
        path = self.artifact()
        path.write_bytes(path.read_bytes() + b"corrupted")
        self.assert_keeps_previous(self.run_install(), "checksum mismatch")

    def test_invalid_gzip_with_a_matching_checksum_does_not_install(self):
        self.artifact(data=b"not a gzip archive")
        self.assert_keeps_previous(self.run_install(), "could not unpack")

    def test_empty_gzip_does_not_install(self):
        self.artifact(data=gzip.compress(b"", mtime=0))
        self.assert_keeps_previous(self.run_install(), "empty binary")

    def test_missing_checksum_entry_does_not_install(self):
        self.artifact()
        (self.downloads / "SHA256SUMS").write_text("")
        self.assert_keeps_previous(self.run_install(), "is not in SHA256SUMS")

    def test_missing_download_does_not_install(self):
        self.artifact().unlink()
        self.assert_keeps_previous(self.run_install(), "could not download")


class AssetSizeTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="jarvis-assets-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)

    def file(self, name, size):
        path = self.root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        with path.open("wb") as output:
            output.truncate(size)

    def test_limit_is_inclusive_and_nested_files_are_checked(self):
        self.file("dl/at-limit.gz", assets.MAX_ASSET_BYTES)
        self.file("dl/over-limit.gz", assets.MAX_ASSET_BYTES + 1)
        self.file("index.html", 100)
        self.assertEqual(assets.oversized_assets(self.root),
                         [(Path("dl/over-limit.gz"), assets.MAX_ASSET_BYTES + 1)])

    def test_size_guard_rejects_before_deployment(self):
        self.file("dl/over-limit.gz", assets.MAX_ASSET_BYTES + 1)
        result = subprocess.run(["python3", str(SCRIPTS / "check_asset_sizes.py"), str(self.root)],
                                capture_output=True, text=True, timeout=10)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("deployment stopped before upload", result.stderr)
        self.assertIn("dl/over-limit.gz", result.stdout)

    def test_small_assets_pass(self):
        self.file("dl/binary.gz", 100)
        self.assertEqual(assets.oversized_assets(self.root), [])


if __name__ == "__main__":
    unittest.main()
