from pathlib import Path
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]
ENTRYPOINT = ROOT / "deploy/docker/entrypoint.sh"


class DockerEntrypointTests(unittest.TestCase):
    def run_entrypoint(self, desktop, access="", cidrs="", exit_code=0):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            commands = {
                "id": '#!/bin/sh\ncase "$1" in -u) echo 1000 ;; -un) echo jarvis ;; *) exit 1 ;; esac\n',
                "getent": '#!/bin/sh\n[ "$1 $2" = "hosts host.docker.internal" ] || exit 2\nexit ' + ("0" if desktop else "1") + '\n',
                "jarvisd": '#!/bin/sh\nprintf "%s\\n" "$@"\nexit ' + str(exit_code) + '\n',
            }
            for name, content in commands.items():
                path = root / name
                path.write_text(content)
                path.chmod(0o755)
            result = subprocess.run(
                ["/bin/sh", str(ENTRYPOINT), "hub", "run", "one argument"],
                env={"PATH": str(root), "HOME": str(root), "JARVIS_ADMIN_ACCESS": access,
                     "JARVIS_ADMIN_CIDRS": cidrs},
                capture_output=True, text=True, timeout=5,
            )
            self.assertEqual(result.returncode, exit_code, result.stderr)
            self.assertEqual(result.stdout.splitlines(), ["hub", "run", "one argument"])
            self.assertEqual(len(list(root.iterdir())), len(commands))
            return result.stderr

    def test_desktop_api_guidance_is_independent_of_admin_access(self):
        for access in ("", "lan", "loopback", "cidrs", "any"):
            with self.subTest(access=access):
                logs = self.run_entrypoint(True, access, "192.0.2.1/32")
                for expected in ("gvisor", "tls_required", "security.trusted_cidrs", "netstat -tn",
                                 "docs/docker.md", "127.0.0.1"):
                    self.assertIn(expected, logs)
                self.assertNotIn("every browser reaches the hub from the bridge's gateway", logs)

    def test_linux_does_not_get_desktop_guidance(self):
        self.assertNotIn("Docker Desktop", self.run_entrypoint(False))

    def test_admin_reminders_are_preserved(self):
        self.assertIn("JARVIS_ADMIN_ACCESS is not set", self.run_entrypoint(False))
        self.assertIn("without JARVIS_ADMIN_CIDRS", self.run_entrypoint(True, "cidrs"))
        self.assertIn("is not loopback, lan, cidrs or any", self.run_entrypoint(False, "invalid"))

    def test_hub_exit_status_is_preserved(self):
        self.run_entrypoint(True, "any", exit_code=23)


if __name__ == "__main__":
    unittest.main()
