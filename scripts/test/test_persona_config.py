import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('config_under_test', ROOT / 'scripts/jarvis_config.py')
config = importlib.util.module_from_spec(spec)
spec.loader.exec_module(config)


class PersonaConfigTests(unittest.TestCase):
    def test_old_and_new_config_paths_share_the_file(self):
        for initial in ('jarvis', 'persona'):
            with self.subTest(initial=initial), tempfile.TemporaryDirectory() as directory:
                with patch.dict(os.environ, {'HOME': directory}, clear=True):
                    path = Path(directory) / '.config' / initial / 'config.json'
                    path.parent.mkdir(parents=True)
                    path.write_text('{"app":{"bundle_id":"com.example.assistant"}}')
                    self.assertEqual(config.load()['app']['bundle_id'], 'com.example.assistant')
                    self.assertEqual((Path(directory) / '.config/persona/config.json').resolve(),
                                     (Path(directory) / '.config/jarvis/config.json').resolve())
                    self.assertEqual(config.path(), str(Path(directory) / '.config/persona/config.json'))

    def test_current_config_priority_and_independent_profiles(self):
        with tempfile.TemporaryDirectory() as directory, patch.dict(os.environ, {'HOME': directory}, clear=True):
            self.assertEqual(config.path(), str(Path(directory) / '.config/persona/config.json'))
            old, new = Path(directory) / 'old.json', Path(directory) / 'new.json'
            old.write_text('{"which":"old"}')
            new.write_text('{"which":"new"}')
            os.environ['JARVIS_CONFIG'] = str(old)
            self.assertEqual(config.load()['which'], 'old')
            os.environ['PERSONA_CONFIG'] = str(new)
            self.assertEqual(config.load()['which'], 'new')
            self.assertFalse((Path(directory) / '.config').exists())

    def test_both_configs_remain_intact(self):
        with tempfile.TemporaryDirectory() as directory, patch.dict(os.environ, {'HOME': directory}, clear=True):
            for name in ('persona', 'jarvis'):
                file = Path(directory) / '.config' / name / 'config.json'
                file.parent.mkdir(parents=True)
                file.write_text(json.dumps({'name': name}))
            self.assertEqual(config.load()['name'], 'persona')
            self.assertEqual(json.loads((Path(directory) / '.config/jarvis/config.json').read_text())['name'], 'jarvis')

    def test_generated_environment_keeps_both_names_and_never_prints_other_secrets(self):
        with tempfile.TemporaryDirectory() as directory:
            file = Path(directory) / 'config.json'
            file.write_text('{"hub":{"url":"http://hub.example.com"}}')
            env = {'HOME': directory, 'PATH': os.environ['PATH'], 'JARVIS_CONFIG': str(file),
                   'PERSONA_HUB_URL': 'http://previous.example.com', 'PERSONA_PRIVATE_TOKEN': 'test-private-value'}
            result = subprocess.run([sys.executable, str(ROOT / 'scripts/jarvis_config.py'), 'env'],
                                    env=env, capture_output=True, text=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertNotIn('test-private-value', result.stdout)
            shell = subprocess.run(['bash', '-c', result.stdout + '\nprintf "%s|%s|%s" "$JARVIS_HUB_URL" "$PERSONA_HUB_URL" "$JARVIS_PRIVATE_TOKEN"'],
                                   env=env, capture_output=True, text=True, timeout=10)
            self.assertEqual(shell.returncode, 0, shell.stderr)
            self.assertEqual(shell.stdout, 'http://hub.example.com|http://hub.example.com|test-private-value')


class MetaWearablesConfigTests(unittest.TestCase):
    def test_env_enables_the_meta_wearables_include_only_when_configured(self):
        cases = {
            'enabled': ({'app': {'meta_wearables': {'enabled': True}}}, 'YES'),
            'disabled': ({'app': {'meta_wearables': {'enabled': False}}}, 'NO'),
            'missing section': ({'app': {'bundle_id': 'com.example.assistant'}}, 'NO'),
            'missing app': ({}, 'NO'),
            'not a boolean': ({'app': {'meta_wearables': {'enabled': 'yes'}}}, 'NO'),
        }
        for name, (cfg, expected) in cases.items():
            with self.subTest(case=name):
                generated = config.env(cfg)
                self.assertEqual(generated['JARVIS_META_WEARABLES'], expected)
                self.assertEqual(generated['PERSONA_META_WEARABLES'], expected)

    def test_xcconfig_writes_meta_app_id_and_client_token(self):
        cases = {
            'configured': ({'app': {'meta_wearables': {'enabled': True, 'app_id': '1234567890',
                                                       'client_token': 'AR|token|value'}}},
                           'PERSONA_META_APP_ID = 1234567890', 'PERSONA_META_CLIENT_TOKEN = AR|token|value'),
            'developer mode': ({'app': {'meta_wearables': {'enabled': False, 'app_id': '', 'client_token': ''}}},
                               'PERSONA_META_APP_ID = 0', 'PERSONA_META_CLIENT_TOKEN = '),
            'missing section': ({'app': {'bundle_id': 'com.example.assistant'}},
                                'PERSONA_META_APP_ID = 0', 'PERSONA_META_CLIENT_TOKEN = '),
        }
        for name, (cfg, app_line, token_line) in cases.items():
            with self.subTest(case=name), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                (root / 'apple').mkdir()
                with patch.object(config, 'ROOT', str(root)):
                    config.xcconfig(cfg)
                lines = (root / 'apple' / 'Config.xcconfig').read_text().splitlines()
                self.assertIn(app_line, lines)
                self.assertIn(token_line, lines)


if __name__ == '__main__':
    unittest.main()
