"""hub-systemd.sh on a Mac hub: no systemd, so it restarts the launchd agent that already runs <root>/bin/jarvisd hub."""
import os
from pathlib import Path
import plistlib
import platform
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / 'scripts/deploy/hub-systemd.sh'


@unittest.skipUnless(platform.system() == 'Darwin', 'the launchd branch runs only on macOS')
class HubRestartOnMac(unittest.TestCase):
    def run_script(self, home: Path, agents: dict) -> subprocess.CompletedProcess:
        (home / 'Library/LaunchAgents').mkdir(parents=True)
        for name, job in agents.items():
            with open(home / 'Library/LaunchAgents' / f'{name}.plist', 'wb') as f:
                plistlib.dump(job, f)
        fake = home / 'fakebin'
        fake.mkdir()
        (fake / 'launchctl').write_text('#!/bin/sh\necho "launchctl $*" >> "$HOME/calls"\n')
        (fake / 'launchctl').chmod(0o755)
        env = {**os.environ, 'HOME': str(home), 'PATH': f'{fake}:{os.environ["PATH"]}',
               '_PERSONA_STATE_DIR': str(home / '.persona')}
        return subprocess.run(['/bin/bash', str(SCRIPT)], capture_output=True, text=True, env=env)

    def test_restarts_the_agent_running_this_roots_hub(self):
        with tempfile.TemporaryDirectory() as d:
            home = Path(d)
            (home / '.persona/bin').mkdir(parents=True)
            (home / '.persona/bin/jarvisd').write_text('')
            os.symlink(home / '.persona', home / '.jarvis')  # the agent may name the legacy path
            r = self.run_script(home, {
                'other.node': {'Label': 'other.node', 'ProgramArguments': [str(home / '.jarvis/bin/jarvisd'), 'node']},
                'my.hub': {'Label': 'my.hub', 'ProgramArguments': [str(home / '.jarvis/bin/jarvisd'), 'hub']},
            })
            self.assertEqual(r.returncode, 0, r.stderr)
            self.assertIn('restarted launchd agent my.hub', r.stdout)
            self.assertEqual((home / 'calls').read_text().strip(), f'launchctl kickstart -k gui/{os.getuid()}/my.hub')

    def test_no_agent_is_an_error_not_a_silent_success(self):
        with tempfile.TemporaryDirectory() as d:
            home = Path(d)
            (home / '.persona/bin').mkdir(parents=True)
            r = self.run_script(home, {})
            self.assertEqual(r.returncode, 1)
            self.assertIn('no launchd agent', r.stderr)
            self.assertFalse((home / 'calls').exists())


if __name__ == '__main__':
    unittest.main()
