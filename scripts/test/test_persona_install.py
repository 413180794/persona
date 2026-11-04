import gzip
import hashlib
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import platform
import subprocess
import tempfile
import threading
import unittest

ROOT = Path(__file__).resolve().parents[2]
INSTALL = ROOT / 'scripts/deploy/install.sh'


class PersonaInstallTests(unittest.TestCase):
    def run_install(self, home, initial='fresh', extra=None, corrupt=False, link=None):
        target = {'Darwin': 'darwin', 'Linux': 'linux'}[platform.system()] + '-' + {'arm64': 'arm64', 'aarch64': 'arm64', 'x86_64': 'amd64'}[platform.machine()]
        name = 'jarvisd-' + target
        with tempfile.TemporaryDirectory() as directory:
            downloads = Path(directory)
            data = gzip.compress(b'#!/bin/sh\nif [ "$1" = version ]; then printf "fixture-version\\n"; fi\n', mtime=0)
            (downloads / (name + '.gz')).write_bytes(data)
            digest = '0' * 64 if corrupt else hashlib.sha256(data).hexdigest()
            sums = digest + '  ' + name + '.gz\n'
            if link is not None:  # the connector's download, a script that does what `link` says
                link_data = gzip.compress(link.encode(), mtime=0)
                (downloads / ('jarvis-link-' + target + '.gz')).write_bytes(link_data)
                sums += hashlib.sha256(link_data).hexdigest() + '  jarvis-link-' + target + '.gz\n'
            (downloads / 'SHA256SUMS').write_text(sums)
            class Handler(SimpleHTTPRequestHandler):
                def __init__(self, *args, **kwargs):
                    super().__init__(*args, directory=str(downloads), **kwargs)
                def log_message(self, *args):
                    pass
            server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
            thread = threading.Thread(target=server.serve_forever, daemon=True)
            thread.start()
            try:
                env = {key: value for key, value in os.environ.items() if not key.startswith(('PERSONA_', 'JARVIS_'))}
                env.update({'HOME': str(home), 'PERSONA_DL': f'http://127.0.0.1:{server.server_port}',
                            'JARVIS_DL': 'https://invalid.example.test', 'PERSONA_SKIP_HERDR': '1',
                            'PERSONA_ROLE': 'hub', 'PERSONA_ADMIN_ACCESS': 'lan'})
                env.update(extra or {})
                return subprocess.run(['sh', str(INSTALL)], env=env, capture_output=True, text=True, timeout=20)
            finally:
                server.shutdown()
                server.server_close()
                thread.join(timeout=5)

    def test_new_install_and_repeated_upgrade_keep_old_commands_working(self):
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory)
            for _ in range(2):
                result = self.run_install(home)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertTrue((home / '.persona').is_symlink())
                self.assertFalse((home / '.jarvis').is_symlink())
                self.assertEqual((home / '.persona').resolve(), (home / '.jarvis').resolve())
                self.assertEqual((home / '.persona/bin/personad').resolve(), (home / '.jarvis/bin/jarvisd').resolve())

    def test_node_install_adds_persona_link_and_runs_the_real_connector(self):
        # #1159: persona-link is an alias of the jarvis-link file; the installer runs the real file (the service and
        # the unit keep the old name) and only asks it to say the new name.
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory)
            log = home / 'link-calls'
            link = ('#!/bin/sh\nif [ "$1" = version ]; then printf "link-version\\n"; exit 0; fi\n'
                    'printf "%s|%s|%s\\n" "$(basename "$0")" "${PERSONA_LINK_NAME:-}" "$*" >> "' + str(log) + '"\n')
            for _ in range(2):
                result = self.run_install(home, link=link, extra={'PERSONA_ROLE': 'node', 'JARVIS_PAIR': 'jarvis://link?url=https%3A%2F%2Fhub.example.com&token=jp_x'})
                self.assertEqual(result.returncode, 0, result.stderr)
                bin_dir = home / '.persona/bin'
                self.assertTrue((bin_dir / 'jarvis-link').is_file())
                self.assertFalse((bin_dir / 'jarvis-link').is_symlink())
                self.assertEqual((bin_dir / 'persona-link').resolve(), (bin_dir / 'jarvis-link').resolve())
                self.assertIn('persona-link link-version installed', result.stdout)
            calls = log.read_text().splitlines()
            self.assertTrue(calls)
            for call in calls:
                invoked, shown, _ = call.split('|', 2)
                self.assertEqual((invoked, shown), ('jarvis-link', 'persona-link'))

    def test_legacy_state_and_external_symlink_are_not_moved(self):
        for external in (False, True):
            with self.subTest(external=external), tempfile.TemporaryDirectory() as directory:
                home = Path(directory)
                state = home / 'external' if external else home / '.jarvis'
                state.mkdir(mode=0o700)
                if external:
                    (home / '.jarvis').symlink_to('external')
                (state / 'hub.json').write_text('{"token":"fixture-token"}')
                result = self.run_install(home)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual((home / '.persona').resolve(), state.resolve())
                self.assertEqual(json.loads((home / '.persona/hub.json').read_text())['token'], 'fixture-token')

    def test_custom_profiles_and_current_environment_precedence(self):
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory)
            result = self.run_install(home, extra={'JARVIS_HOME': str(home / 'old-profile'), 'PERSONA_HOME': str(home / 'new-profile')})
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertTrue((home / 'new-profile/bin/personad').exists())
            self.assertFalse((home / 'old-profile').exists())
            self.assertFalse((home / '.persona').exists())
            self.assertFalse((home / '.jarvis').exists())

    def test_bad_download_does_not_create_or_modify_state(self):
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory)
            result = self.run_install(home, corrupt=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn('checksum mismatch', result.stderr)
            self.assertFalse((home / '.jarvis').exists())
            self.assertFalse((home / '.persona').exists())

    def test_dangling_external_state_is_not_replaced_by_an_empty_store(self):
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory)
            (home / '.jarvis').symlink_to('missing-external')
            result = self.run_install(home)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn('restore its target', result.stderr)
            self.assertEqual(os.readlink(home / '.jarvis'), 'missing-external')
            self.assertFalse((home / '.persona').exists())


if __name__ == '__main__':
    unittest.main()
