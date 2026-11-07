import os
from pathlib import Path
import plistlib
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
from test_persona_script_identity import FORMER_NAME, FORMER_PREFIX

SCRIPT = ROOT / 'scripts/deploy/node-launchd.sh'


class PersonaServiceEnvironmentTests(unittest.TestCase):
    def test_selected_profile_survives_service_manager_restart(self):
        for system in ('Linux', 'Darwin'):
            for custom in (False, True):
                with self.subTest(system=system, custom=custom), tempfile.TemporaryDirectory() as directory:
                    home = Path(directory)
                    state = home / ('profile with $ & < > % chars' if custom else '.persona')
                    (state / 'bin').mkdir(parents=True)
                    binary = state / 'bin/personad'
                    binary.write_text('#!/bin/sh\nexit 0\n')
                    binary.chmod(0o755)
                    commands = home / 'commands'
                    commands.mkdir()
                    for name, body in {
                        'uname': '#!/bin/sh\nprintf "%s\\n" "' + system + '"\n',
                        'systemctl': '#!/bin/sh\nexit 0\n',
                        'launchctl': '#!/bin/sh\nexit 0\n',
                        'loginctl': '#!/bin/sh\nprintf "Linger=yes\\n"\n',
                    }.items():
                        path = commands / name
                        path.write_text(body)
                        path.chmod(0o755)
                    env = {k: v for k, v in os.environ.items() if not k.startswith(('PERSONA_'))}
                    env.update({'HOME': str(home), 'PATH': str(commands) + ':' + os.environ['PATH']})
                    if custom:
                        env['PERSONA_HOME'] = str(state)
                        env[FORMER_PREFIX + 'HOME'] = str(home / 'ignored-former-profile')
                    result = subprocess.run(['bash', str(SCRIPT), 'install'], env=env, capture_output=True, text=True, timeout=15)
                    self.assertEqual(result.returncode, 0, result.stderr)
                    if system == 'Darwin':
                        with open(home / 'Library/LaunchAgents/com.persona.node.plist', 'rb') as file:
                            configuration = plistlib.load(file)
                        self.assertEqual(configuration['ProgramArguments'][0], str(binary))
                        self.assertEqual(configuration['EnvironmentVariables'], {'PERSONA_HOME': str(state)})
                        self.assertEqual(configuration['StandardErrorPath'], str(state / 'node-attach.log'))
                        # Not throttled like a background job (#5944).
                        self.assertEqual(configuration['ProcessType'], 'Interactive')
                    else:
                        configuration = (home / '.config/systemd/user/personad-node.service').read_text()
                        self.assertIn('Environment="PERSONA_HOME=' + str(state).replace('%', '%%') + '"', configuration)
                        self.assertNotIn(FORMER_PREFIX + 'HOME', configuration)
                        self.assertIn('ExecStart="' + str(binary).replace('$', '$$').replace('%', '%%') + '" node --attach', configuration)
                    self.assertFalse((home / 'ignored-former-profile').exists())
                    self.assertFalse((home / '.config/systemd/user' / (FORMER_NAME + 'd-node.service')).exists())
                    self.assertFalse((home / 'Library/LaunchAgents' / ('com.' + FORMER_NAME + '.node.plist')).exists())


if __name__ == '__main__':
    unittest.main()
