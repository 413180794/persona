"""hub-systemd.sh on a Mac hub: no systemd. It first asks the new binary to switch the running hub's edge over
(blue-green, docs/hub-restart.md "Blue-green switching"); with no edge to switch on, it restarts the launchd agent that already runs
<root>/bin/jarvisd hub."""
import os
from pathlib import Path
import plistlib
import platform
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / 'scripts/deploy/hub-systemd.sh'


def agents(home: Path) -> dict:
    """A node's agent and the hub's, both naming the legacy ~/.jarvis path. The node's sorts first, so the script
    reads it before the hub's and has to pass it over (it runs the same jarvisd, as node, not hub)."""
    return {
        'a.node': {'Label': 'a.node', 'ProgramArguments': [str(home / '.jarvis/bin/jarvisd'), 'node']},
        'my.hub': {'Label': 'my.hub', 'ProgramArguments': [str(home / '.jarvis/bin/jarvisd'), 'hub']},
    }


@unittest.skipUnless(platform.system() == 'Darwin', 'the launchd branch runs only on macOS')
class HubRestartOnMac(unittest.TestCase):
    def jarvisd(self, home: Path, rc: int):
        """The new binary: each call is logged, and `hub switch` exits rc (3: no edge here, 0: switched)."""
        p = home / '.persona/bin/jarvisd'
        p.write_text(f'#!/bin/sh\necho "jarvisd $*" >> "$HOME/calls"\nexit {rc}\n')
        p.chmod(0o755)

    def run_script(self, home: Path, jobs: dict) -> subprocess.CompletedProcess:
        (home / 'Library/LaunchAgents').mkdir(parents=True)
        for name, job in jobs.items():
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
            self.jarvisd(home, 3)  # no edge to switch on
            os.symlink(home / '.persona', home / '.jarvis')  # the agent may name the legacy path
            r = self.run_script(home, agents(home))
            self.assertEqual(r.returncode, 0, r.stderr)
            self.assertIn('restarted launchd agent my.hub', r.stdout)
            self.assertEqual((home / 'calls').read_text().splitlines(), [
                'jarvisd hub switch --timeout 8m', f'launchctl kickstart -k gui/{os.getuid()}/my.hub'])

    def test_switches_the_edge_instead_of_restarting(self):
        with tempfile.TemporaryDirectory() as d:
            home = Path(d)
            (home / '.persona/bin').mkdir(parents=True)
            self.jarvisd(home, 0)  # the running edge switched to the new binary
            os.symlink(home / '.persona', home / '.jarvis')
            r = self.run_script(home, agents(home))
            self.assertEqual(r.returncode, 0, r.stderr)
            self.assertIn('switched to the new binary', r.stdout)
            self.assertEqual((home / 'calls').read_text().splitlines(), ['jarvisd hub switch --timeout 8m'])

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
