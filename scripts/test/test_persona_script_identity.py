import importlib.util
import json
import os
from pathlib import Path
import plistlib
import shlex
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / 'scripts/persona_identity.py'
spec = importlib.util.spec_from_file_location('runtime_identity_under_test', SCRIPT)
identity = importlib.util.module_from_spec(spec)
spec.loader.exec_module(identity)


def environment(home, **extra):
    return {'HOME': str(home), 'PATH': os.environ['PATH'], 'PYTHONDONTWRITEBYTECODE': '1', **extra}


def executable(path, text):
    path.write_text(text)
    path.chmod(0o755)


class PersonaScriptIdentityTests(unittest.TestCase):
    def test_environment_precedence_including_empty_and_single_prefix(self):
        for values, expected in (({}, ''), ({'JARVIS_HOME': 'old'}, 'old'),
                                 ({'PERSONA_HOME': 'new', 'JARVIS_HOME': 'old'}, 'new'),
                                 ({'PERSONA_HOME': '', 'JARVIS_HOME': 'old'}, '')):
            self.assertEqual(identity.env_value('PERSONA_HOME', values), expected)
        self.assertEqual(identity.env_value('JARVIS_PERSONA_ID', {'PERSONA_PERSONA_ID': 'correct', 'PERSONA_ID': 'wrong'}), 'correct')

    def test_lexical_custom_roots_do_not_create_default_aliases(self):
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory)
            for value, expected in (('~', home), ('~/state with spaces', home / 'state with spaces'),
                                    ('one/../two', home / 'two'), ('/tmp/a/../b', Path('/tmp/b'))):
                self.assertEqual(identity.data_dir(home, {'PERSONA_HOME': value}), str(expected))
            custom = home / 'profile with $ " \\ % characters'
            self.assertEqual(identity.prepare_dir(home, {'PERSONA_HOME': str(custom), 'JARVIS_HOME': 'ignored'}), str(custom))
            self.assertFalse((home / '.persona').exists())
            self.assertFalse((home / '.jarvis').exists())
            (home / 'hop').symlink_to(custom, target_is_directory=True)
            self.assertEqual(identity.data_dir(home, {'PERSONA_HOME': 'hop/../second'}), str(home / 'second'))

    def test_default_query_is_pure_and_initialization_keeps_one_store(self):
        for initial in ('fresh', 'jarvis', 'persona', 'independent'):
            with self.subTest(initial=initial), tempfile.TemporaryDirectory() as directory:
                home = Path(directory)
                old, new = home / '.jarvis', home / '.persona'
                if initial in ('jarvis', 'independent'):
                    old.mkdir()
                    (old / 'old.fixture').write_text('old-data')
                if initial in ('persona', 'independent'):
                    new.mkdir()
                    (new / 'new.fixture').write_text('new-data')
                before = sorted(p.name for p in home.iterdir())
                self.assertEqual(identity.data_dir(home, {}), str(old if initial in ('jarvis', 'independent') else new))
                self.assertEqual(sorted(p.name for p in home.iterdir()), before)
                result = identity.prepare_dir(home, {})
                if initial == 'independent':
                    self.assertEqual(result, str(old))
                    self.assertFalse(os.path.samefile(old, new))
                    self.assertEqual((old / 'old.fixture').read_text(), 'old-data')
                    self.assertEqual((new / 'new.fixture').read_text(), 'new-data')
                else:
                    self.assertTrue(os.path.samefile(old, new))
                    self.assertEqual(result, str(new))
                    self.assertEqual(identity.prepare_dir(home, {}), str(new))

    def test_default_dangling_aliases_restore_only_the_counterpart(self):
        for name, target in (('.persona', './.jarvis/.'), ('.jarvis', './.persona/')):
            with self.subTest(name=name), tempfile.TemporaryDirectory() as directory:
                home = Path(directory)
                (home / name).symlink_to(target, target_is_directory=True)
                identity.prepare_dir(home, {})
                self.assertTrue(os.path.samefile(home / '.jarvis', home / '.persona'))

    def test_external_dangling_cycles_and_symlink_parent_escape_are_rejected(self):
        for kind in ('external', 'cycle', 'parent-escape', 'custom'):
            with self.subTest(kind=kind), tempfile.TemporaryDirectory() as directory:
                home = Path(directory) / 'home'
                home.mkdir()
                external = Path(directory) / 'external'
                external.mkdir()
                env = {}
                if kind == 'external':
                    (home / '.jarvis').symlink_to(external / 'missing')
                elif kind == 'cycle':
                    (home / '.jarvis').symlink_to('.persona')
                    (home / '.persona').symlink_to('.jarvis')
                elif kind == 'parent-escape':
                    (external / 'nested').mkdir()
                    (home / 'hop').symlink_to(external / 'nested')
                    (home / '.jarvis').symlink_to('hop/../.persona')
                else:
                    (home / 'custom').symlink_to(external / 'missing')
                    env['PERSONA_HOME'] = str(home / 'custom')
                with self.assertRaises(OSError):
                    identity.prepare_dir(home, env)
                self.assertFalse((external / 'missing').exists())
                self.assertFalse((external / '.persona').exists())

    def test_alias_race_rejects_independent_winning_directory(self):
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory)
            real_symlink = os.symlink
            def raced(source, target):
                Path(target).mkdir()
                real_symlink(source, target)
            with patch.object(identity.os, 'symlink', raced), self.assertRaises(OSError):
                identity.prepare_dir(home, {})
            self.assertFalse(os.path.samefile(home / '.persona', home / '.jarvis'))

    def test_service_root_survives_an_ssh_environment_without_prefixes(self):
        for name in ('jarvisd.service', 'jarvisd-node.service', 'jarvis-link.service'):
            with self.subTest(name=name), tempfile.TemporaryDirectory() as directory:
                home = Path(directory)
                root = str(home / 'profile with $ " \\ %h chars')
                unit = home / '.config/systemd/user' / name
                unit.parent.mkdir(parents=True)
                quote = lambda value: '"' + value.replace('\\', '\\\\').replace('"', '\\"').replace('%', '%%') + '"'
                unit.write_text('[Service]\nEnvironment=' + quote('JARVIS_HOME=ignored') + '\nEnvironment=' + quote('PERSONA_HOME=' + root) + '\n')
                self.assertEqual(identity.data_dir(home, identity.service_environment(home, {})), root)
                self.assertEqual(identity.service_environment(home, {'PERSONA_HOME': '', 'JARVIS_HOME': 'explicit'}), {'PERSONA_HOME': '', 'JARVIS_HOME': 'explicit'})
                unit.write_text('[Service]\nEnvironment="JARVIS_HOME=%h/profile"\n')
                self.assertEqual(identity.data_dir(home, identity.service_environment(home, {})), str(home / 'profile'))

    def test_role_selection_never_borrows_the_colocated_hubs_root(self):
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory)
            units = home / '.config/systemd/user'; units.mkdir(parents=True)
            for name, role in [('jarvisd.service', 'hub'), ('jarvisd-node.service', 'node'), ('jarvis-link.service', 'link')]:
                (units / name).write_text('[Service]\nEnvironment="PERSONA_HOME=' + str(home / role) + '"\n')
            for role in ('hub', 'node', 'link'):
                self.assertEqual(identity.data_dir(home, identity.service_environment(home, {}, role)), str(home / role))
            with self.assertRaisesRegex(OSError, 'different state roots'):
                identity.service_environment(home, {})
            binary = home / 'link/bin/jarvis-link'; binary.parent.mkdir(parents=True)
            executable(binary, '#!/bin/sh\nprintf "%s" "$PERSONA_HOME"\n')
            result = subprocess.run(['bash', str(ROOT / 'scripts/deploy/link-systemd.sh'), 'status'], env=environment(home), capture_output=True, text=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(result.stdout, str(home / 'link'))

    def test_legacy_unit_without_environment_keeps_its_default_root(self):
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory)
            legacy = home / '.jarvis'; legacy.mkdir()
            units = home / '.config/systemd/user'; units.mkdir(parents=True)
            (units / 'jarvisd-node.service').write_text('[Service]\nExecStart=%h/.jarvis/bin/jarvisd node --attach\n')
            (units / 'jarvis-link.service').write_text('[Service]\nEnvironment="PERSONA_HOME=other-link"\n')
            self.assertEqual(identity.data_dir(home, identity.service_environment(home, {}, 'node')), str(legacy))
            self.assertEqual(identity.data_dir(home, identity.service_environment(home, {}, 'link')), str(home / 'other-link'))

    def test_launchd_services_choose_the_matching_role(self):
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory)
            agents = home / 'Library/LaunchAgents'; agents.mkdir(parents=True)
            for role in ('node', 'link'):
                (agents / ('com.jarvis.' + role + '.plist')).write_bytes(plistlib.dumps({'EnvironmentVariables': {'PERSONA_HOME': role + '-profile'}}))
            for role in ('node', 'link'):
                self.assertEqual(identity.data_dir(home, identity.service_environment(home, {}, role)), str(home / (role + '-profile')))

    def test_service_roots_that_are_compatibility_aliases_are_not_ambiguous(self):
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory)
            identity.prepare_dir(home, {})
            units = home / '.config/systemd/user'; units.mkdir(parents=True)
            (units / 'jarvisd.service').write_text('[Service]\nEnvironment="PERSONA_HOME=' + str(home / '.persona') + '"\n')
            (units / 'jarvisd-node.service').write_text('[Service]\nEnvironment="JARVIS_HOME=' + str(home / '.jarvis') + '"\n')
            self.assertTrue(os.path.samefile(identity.data_dir(home, identity.service_environment(home, {})), home / '.persona'))

    def test_unit_resets_and_ordered_dropins_match_the_effective_root(self):
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory)
            unit = home / '.config/systemd/user/jarvisd.service'; unit.parent.mkdir(parents=True)
            unit.write_text('[Service]\nEnvironment="PERSONA_HOME=old"\nEnvironment=\nEnvironment="JARVIS_HOME=after-reset"\n')
            self.assertEqual(identity.data_dir(home, identity.service_environment(home, {}, 'hub')), str(home / 'after-reset'))
            dropins = unit.with_name(unit.name + '.d'); dropins.mkdir()
            (dropins / '10-root.conf').write_text('[Service]\nEnvironment="PERSONA_HOME=first"\n')
            (dropins / '20-root.conf').write_text('[Service]\nEnvironment="PERSONA_HOME=last"\n')
            self.assertEqual(identity.data_dir(home, identity.service_environment(home, {}, 'hub')), str(home / 'last'))
            (dropins / '30-empty.conf').write_text('[Service]\nEnvironment="PERSONA_HOME="\n')
            self.assertEqual(identity.data_dir(home, identity.service_environment(home, {}, 'hub')), str(home / '.persona'))
            (dropins / '40-external.conf').write_text('[Service]\nEnvironmentFile=/unread/fixture.env\n')
            with self.assertRaisesRegex(FileNotFoundError, 'unavailable'):
                identity.service_environment(home, {}, 'hub')

    def test_hub_unit_writer_and_reader_agree_with_xdg_config_home(self):
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory)
            commands = home / 'commands'; commands.mkdir()
            executable(commands / 'systemctl', '#!/bin/sh\n[ -f "$XDG_CONFIG_HOME/systemd/user/jarvisd.service" ] || exit 4\n')
            config = home / 'xdg config'
            root = home / 'hub profile'
            env = environment(home, PERSONA_HOME=str(root), XDG_CONFIG_HOME=str(config))
            env['PATH'] = str(commands) + ':' + env['PATH']
            result = subprocess.run(['bash', str(ROOT / 'scripts/deploy/hub-systemd.sh')], env=env, capture_output=True, text=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertFalse((home / '.config').exists())
            self.assertEqual(identity.data_dir(home, identity.service_environment(home, {'XDG_CONFIG_HOME': str(config)}, 'hub')), str(root))

    def test_xdg_service_and_launchd_environment_are_read_without_credentials(self):
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory)
            config = home / 'alternate config'
            unit = config / 'systemd/user/jarvis-link.service'
            unit.parent.mkdir(parents=True)
            unit.write_text('[Service]\nEnvironment="PERSONA_HOME=profile"\n')
            values = identity.service_environment(home, {'XDG_CONFIG_HOME': str(config)})
            self.assertEqual(identity.data_dir(home, values), str(home / 'profile'))
            plist = home / 'Library/LaunchAgents/com.jarvis.node.plist'
            plist.parent.mkdir(parents=True)
            plist.write_bytes(plistlib.dumps({'EnvironmentVariables': {'PERSONA_HOME': str(home / 'Mac profile')}}))
            self.assertEqual(identity.data_dir(home, identity.service_environment(home, {})), str(home / 'Mac profile'))

    def test_embedded_remote_resolver_preserves_binary_stdin_and_uses_remote_home(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            local, remote = base / 'local', base / 'remote'
            local.mkdir(); remote.mkdir()
            root = remote / 'custom remote'
            unit = remote / '.config/systemd/user/jarvisd.service'
            unit.parent.mkdir(parents=True)
            unit.write_text('[Service]\nEnvironment="PERSONA_HOME=' + str(root) + '"\n')
            result = subprocess.run([sys.executable, str(SCRIPT), '--prepare', '--prefix', '-'],
                                    env=environment(local, PERSONA_HOME=str(local / 'local profile')), capture_output=True, text=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stderr)
            command = result.stdout + '\ncat > "$PERSONA_HOME/payload.fixture"\nprintf "%s" "$JARVIS_HOME"\n'
            payload = bytes(range(256)) * 64
            run = subprocess.run(['bash', '-c', command], input=payload, env=environment(remote), capture_output=True, timeout=10)
            self.assertEqual(run.returncode, 0, run.stderr)
            self.assertEqual(run.stdout.decode(), str(root))
            self.assertEqual((root / 'payload.fixture').read_bytes(), payload)
            self.assertFalse((local / 'local profile').exists())
            self.assertFalse((remote / '.jarvis').exists())

    def test_hub_unit_retains_custom_root_and_one_service_identity(self):
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory)
            commands = home / 'commands'; commands.mkdir()
            executable(commands / 'systemctl', '#!/bin/sh\nprintf "%s\\n" "$*" >> "$HOME/calls.fixture"\n')
            root = home / 'profile $ with " \\ %h'
            env = environment(home, PERSONA_HOME=str(root), JARVIS_HOME='ignored')
            env['PATH'] = str(commands) + ':' + env['PATH']
            result = subprocess.run(['bash', str(ROOT / 'scripts/deploy/hub-systemd.sh')], env=env, capture_output=True, text=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stderr)
            unit = home / '.config/systemd/user/jarvisd.service'
            values = identity.service_environment(home, {})
            self.assertEqual(identity.data_dir(home, values), str(root))
            self.assertIn('jarvisd', (home / 'calls.fixture').read_text())
            self.assertEqual(list(unit.parent.glob('*.service')), [unit])
            line = next(line for line in unit.read_text().splitlines() if line.startswith('ExecStart='))
            self.assertEqual(shlex.split(line.split('=', 1)[1])[0].replace('$$', '$').replace('%%', '%'), str(root / 'bin/jarvisd'))

    def test_link_compatibility_wrapper_exports_the_discovered_service_root(self):
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory)
            root = home / 'link profile'
            binary = root / 'bin/jarvis-link'; binary.parent.mkdir(parents=True)
            executable(binary, '#!/bin/sh\nprintf "%s|%s|%s" "$PERSONA_HOME" "$JARVIS_HOME" "$1"\n')
            unit = home / '.config/systemd/user/jarvis-link.service'; unit.parent.mkdir(parents=True)
            unit.write_text('[Service]\nEnvironment="JARVIS_HOME=' + str(root) + '"\n')
            result = subprocess.run(['bash', str(ROOT / 'scripts/deploy/link-systemd.sh'), 'status'], env=environment(home), capture_output=True, text=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(result.stdout, f'{root}|{root}|status')

    def test_cli_keeps_explicit_token_sources_and_accepts_current_bundle_variable(self):
        spec = importlib.util.spec_from_file_location('runtime_ctl_under_test', ROOT / 'scripts/app/jarvisctl.py')
        ctl = importlib.util.module_from_spec(spec); spec.loader.exec_module(ctl)
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory)
            root = home / 'profile'; root.mkdir()
            (root / 'hub.json').write_text(json.dumps({'token': 'fixture-only-token'}))
            token = home / 'token.fixture'; token.write_text('explicit-fixture')
            with patch.dict(os.environ, environment(home, PERSONA_HOME=str(root), PERSONA_BUNDLE_ID='com.example.persona', JARVIS_BUNDLE_ID='com.example.legacy'), clear=True):
                self.assertEqual(ctl.Ctl(udid='fixture-unused').bundle, 'com.example.persona')
                self.assertEqual(ctl.hub_token(), 'fixture-only-token')
                os.environ['JARVISCTL_HUB_TOKEN_FILE'] = str(token)
                self.assertEqual(ctl.hub_token(), 'explicit-fixture')
                os.environ['JARVISCTL_HUB_TOKEN'] = 'environment-fixture'
                self.assertEqual(ctl.hub_token(), 'environment-fixture')
                os.environ['PERSONA_BUNDLE_ID'] = ''
                self.assertEqual(ctl.Ctl(udid='fixture-unused').bundle, 'com.example.jarvis')


class PersonaDeploymentScriptTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix='persona-script-deploy-')
        self.addCleanup(temporary.cleanup)
        self.base = Path(temporary.name)
        self.repo = self.base / 'repo'; self.repo.mkdir()
        (self.repo / 'server').mkdir(); (self.repo / 'web').mkdir()
        shutil.copytree(ROOT / 'scripts', self.repo / 'scripts', ignore=shutil.ignore_patterns('__pycache__'))
        (self.repo / 'web/package.json').write_text('{}')
        (self.repo / 'web/node_modules').mkdir()
        self.local = self.base / 'developer'; self.local.mkdir()
        self.remote = self.base / 'remote'; self.remote.mkdir()
        self.state = self.remote / 'deployed profile with $ " %'
        self.state.mkdir()
        (self.state / 'hub.json').write_text(json.dumps({'database_url': 'postgres://fixture@localhost/jarvis?sslmode=disable'}))
        unit = self.remote / '.config/systemd/user/jarvisd.service'; unit.parent.mkdir(parents=True)
        unit.write_text('[Service]\nEnvironment=' + shlex.quote('JARVIS_HOME=' + str(self.state)) + '\n')
        (unit.parent / 'jarvisd-node.service').write_text(unit.read_text())
        self.commands = self.base / 'commands'; self.commands.mkdir()
        executable(self.commands / 'ssh', '#!' + sys.executable + '\n' + r'''
import os, subprocess, sys
args = sys.argv[1:]
while args and args[0].startswith('-'):
    key = args.pop(0)
    if key == '-o': args.pop(0)
args.pop(0)
env = {key:value for key,value in os.environ.items() if not key.startswith(('PERSONA_', 'JARVIS_'))}
env['HOME'] = env['FIXTURE_REMOTE_HOME']
result = subprocess.run(['bash', '-c', ' '.join(args)], env=env)
sys.exit(result.returncode)
''')
        executable(self.commands / 'go', '#!' + sys.executable + '\n' + r'''
import os, pathlib, sys
if sys.argv[1] == 'generate': sys.exit(0)
if sys.argv[1] == 'build':
    p = pathlib.Path(sys.argv[sys.argv.index('-o')+1]); p.parent.mkdir(parents=True, exist_ok=True)
    p.write_text('#!/bin/sh\nprintf "fixture-binary\\n"\n'); p.chmod(0o755); sys.exit(0)
sys.exit(2)
''')
        executable(self.commands / 'npm', '#!' + sys.executable + '\nfrom pathlib import Path\np=Path("dist");p.mkdir(exist_ok=True);(p/"index.html").write_text("Persona fixture web")\n')
        executable(self.commands / 'git', '#!/bin/sh\ncase "$1" in describe) printf "fixture-version\\n";; rev-parse) printf "fixture-commit\\n";; esac\n')
        executable(self.commands / 'hostname', '#!/bin/sh\nprintf "fixture-local\\n"\n')
        executable(self.commands / 'scutil', '#!/bin/sh\nexit 1\n')
        executable(self.commands / 'systemctl', '#!/bin/sh\nprintf "%s\\n" "$*" >> "$HOME/systemctl.fixture"\n')
        executable(self.commands / 'docker', '#!/bin/sh\ncase "$1" in ps) printf "jarvis-postgres\\n";; exec) case "$3" in psql) printf "1\\n";; esac;; esac\n')
        config = self.base / 'config.json'
        config.write_text(json.dumps({'hub': {'machine': 'hub-fixture'}, 'machines': [
            {'id': 'hub-fixture', 'ssh': 'hub.example.test', 'os': 'linux', 'hostnames': ['fixture-remote']},
            {'id': 'node-fixture', 'ssh': 'node.example.test', 'os': 'darwin', 'hostnames': ['fixture-node']},
            {'id': 'via-fixture', 'ssh': 'via.example.test', 'os': 'darwin', 'via': 'hub-fixture', 'hostnames': ['fixture-via']},
        ]}))
        self.env = environment(self.local, PERSONA_CONFIG=str(config), PERSONA_HOME=str(self.local / 'do-not-copy-this-root'),
                               PERSONA_SKIP_HERDR='1', FIXTURE_REMOTE_HOME=str(self.remote))
        self.env['PATH'] = str(self.commands) + ':' + self.env['PATH']

    def run_script(self, relative, *args):
        result = subprocess.run(['bash', str(self.repo / relative), *args], env=self.env, capture_output=True, text=True, timeout=30)
        self.assertEqual(result.returncode, 0, result.stdout + '\n' + result.stderr)
        return result

    def test_actual_hub_deploy_replaces_service_binary_and_web_in_its_installed_profile(self):
        self.run_script('scripts/deploy/deploy.sh', '--admin-access', 'lan', 'hub-fixture')
        for name in ('jarvisd', 'jarvisd-telegram', 'nodes/jarvisd-linux-amd64', 'nodes/jarvisd-darwin-arm64'):
            self.assertTrue((self.state / 'bin' / name).is_file(), name)
        self.assertTrue(os.path.samefile(self.state / 'bin/jarvisd', self.state / 'bin/personad'))
        self.assertTrue(os.path.samefile(self.state / 'bin/jarvisd-telegram', self.state / 'bin/personad-telegram'))
        self.assertEqual((self.state / 'web/index.html').read_text(), 'Persona fixture web')
        self.assertEqual(identity.data_dir(self.remote, identity.service_environment(self.remote, {})), str(self.state))
        self.assertFalse((self.remote / '.jarvis').exists())
        self.assertFalse((self.local / 'do-not-copy-this-root').exists())

    def test_node_deploy_uses_the_node_profile_not_the_colocated_hub(self):
        node = self.remote / 'separate node'
        unit = self.remote / '.config/systemd/user/jarvisd-node.service'
        unit.write_text('[Service]\nEnvironment="JARVIS_HOME=' + str(node) + '"\n')
        self.run_script('scripts/deploy/deploy.sh', 'via-fixture')
        self.assertTrue((node / 'bin/jarvisd').is_file())
        self.assertFalse((self.state / 'bin/jarvisd').exists())
        self.assertEqual(identity.data_dir(self.remote, identity.service_environment(self.remote, {}, 'hub')), str(self.state))

    def test_node_deploy_over_nested_ssh_preserves_binary_stdin(self):
        self.run_script('scripts/deploy/deploy.sh', 'via-fixture')
        self.assertEqual((self.state / 'bin/jarvisd').read_text(), '#!/bin/sh\nprintf "fixture-binary\\n"\n')
        self.assertTrue(os.path.samefile(self.state / 'bin/jarvisd', self.state / 'bin/personad'))

    def test_web_only_deploy_keeps_the_selected_remote_profile(self):
        self.run_script('scripts/deploy/deploy.sh', 'web')
        self.assertEqual((self.state / 'web/index.html').read_text(), 'Persona fixture web')
        self.assertFalse((self.remote / '.persona').exists())

    def test_android_apk_publish_uses_the_remote_profile_and_config_entrypoint(self):
        apk = self.base / 'fixture.apk'; apk.write_bytes(b'anonymous-signed-apk-fixture')
        self.run_script('scripts/release/publish_android_apk.sh', str(apk), '--version-code', '1', '--version-name', '1.0', '--min-sdk', '26')
        manifest = json.loads((self.state / 'apps/android/latest.json').read_text())
        self.assertEqual(manifest['version_code'], 1)
        self.assertEqual((self.state / 'apps/android' / (manifest['sha256'] + '.apk')).read_bytes(), apk.read_bytes())


if __name__ == '__main__':
    unittest.main()
