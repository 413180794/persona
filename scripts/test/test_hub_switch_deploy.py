"""hub-systemd.sh with systemd: the units unchanged and the hub running, it switches the hub's edge to the new binary
(blue-green, docs/hub-restart.md "Blue-green switch") instead of restarting; no edge, it restarts as before; the new binary
failing to take over puts the old one back and fails the deploy."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / 'scripts/deploy/hub-systemd.sh'


@unittest.skipUnless(shutil.which('bash') and os.name == 'posix', 'needs bash on a Unix')
class HubSwitchOnDeploy(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.home = Path(self.tmp.name)
        self.root = self.home / '.persona'
        (self.root / 'bin').mkdir(parents=True)
        (self.root / 'hub.json').write_text('{"listen": ":18660"}')
        fake = self.home / 'fakebin'
        fake.mkdir()
        # systemctl: every call logged; the hub and its socket are active.
        (fake / 'systemctl').write_text('#!/bin/sh\necho "systemctl $*" >> "$HOME/calls"\nexit 0\n')
        (fake / 'systemctl').chmod(0o755)
        self.fake = fake

    def tearDown(self):
        self.tmp.cleanup()

    def personad(self, rc: int, content: str = 'new'):
        """The new binary: `hub switch` is logged and exits rc."""
        p = self.root / 'bin/personad'
        p.write_text(f'#!/bin/sh\n# {content}\necho "personad $*" >> "$HOME/calls"\nexit {rc}\n')
        p.chmod(0o755)

    def run_script(self, **extra) -> subprocess.CompletedProcess:
        env = {**os.environ, 'HOME': str(self.home), 'XDG_CONFIG_HOME': str(self.home / '.config'),
               'PATH': f'{self.fake}:{os.environ["PATH"]}', '_PERSONA_STATE_DIR': str(self.root)}
        env.pop('PERSONA_HUB_SOCKET', None)
        env.pop('PERSONA_HUB_EDGE', None)
        env.update(extra)
        return subprocess.run(['bash', str(SCRIPT)], capture_output=True, text=True, env=env)

    def unit(self) -> str:
        return (self.home / '.config/systemd/user/personad.service').read_text()

    def test_edge_off_stays_across_deploys(self):
        self.personad(3)
        self.assertEqual(self.run_script(PERSONA_HUB_EDGE='0').returncode, 0)
        self.assertIn('Environment="PERSONA_HUB_EDGE=0"', self.unit())
        self.assertEqual(self.run_script().returncode, 0)
        self.assertIn('Environment="PERSONA_HUB_EDGE=0"', self.unit(), 'a deploy without it dropped the setting')
        self.assertEqual(self.run_script(PERSONA_HUB_EDGE='1').returncode, 0)
        from test_persona_script_identity import identity
        unit = self.home / '.config/systemd/user/personad.service'
        effective = identity._unit_environment(unit, str(self.home), ('PERSONA_HUB_EDGE', 'JARVIS_HUB_EDGE'))
        self.assertEqual(identity.env_value('HUB_EDGE', effective), '1')

    def calls(self) -> list:
        p = self.home / 'calls'
        lines = p.read_text().splitlines() if p.exists() else []
        p.unlink(missing_ok=True)
        return lines

    def first_deploy(self):
        """Writes the units (they change: a restart, no switch)."""
        self.personad(3)
        r = self.run_script()
        self.assertEqual(r.returncode, 0, r.stderr)
        calls = self.calls()
        self.assertFalse(any('hub switch' in c for c in calls), calls)

    def test_switches_when_the_units_are_unchanged(self):
        self.first_deploy()
        self.personad(0)
        (self.root / 'bin/personad.prev').write_text('#!/bin/sh\n# old\n')
        (self.root / 'bin/personad.prev').chmod(0o755)
        r = self.run_script()
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn('switched to the new binary', r.stdout)
        calls = self.calls()
        self.assertIn(f'personad hub switch --timeout 8m --prev {self.root}/bin/personad.prev', calls)
        self.assertFalse(any(c.startswith('systemctl --user restart') or c.startswith('systemctl --user stop') for c in calls), calls)

    def test_restarts_without_an_edge(self):
        self.first_deploy()
        self.personad(3)
        r = self.run_script()
        self.assertEqual(r.returncode, 0, r.stderr)
        calls = self.calls()
        self.assertTrue(any('hub switch' in c for c in calls), calls)
        self.assertIn('systemctl --user restart personad', calls)

    def test_a_new_binary_that_fails_goes_back(self):
        self.first_deploy()
        self.personad(1)
        (self.root / 'bin/personad.prev').write_text('#!/bin/sh\n# old\n')
        (self.root / 'bin/personad.prev').chmod(0o755)
        r = self.run_script()
        self.assertEqual(r.returncode, 1)
        self.assertIn('did not take over', r.stderr)
        self.assertIn('# old', (self.root / 'bin/personad').read_text())
        self.assertFalse(any(c.startswith('systemctl --user restart') for c in self.calls()))


if __name__ == '__main__':
    unittest.main()
