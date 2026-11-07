"""SSH publication setup fails closed without leaking proxy connections."""
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import Mock, patch

# These setup tests never establish SSH; deployment installs the real dependency.
with patch.dict(sys.modules, {'paramiko': Mock()}):
    import publish_hub_release as publisher
import promote_hub_release as promoter
from promote_hub_release import validate_next


class PublisherSetupTests(unittest.TestCase):
    def test_item_custom_labels_and_builtin_ids(self):
        fields = [{'id': 'random-host', 'label': 'host', 'value': 'example.invalid'},
                  {'id': 'random-port', 'label': 'port', 'value': '2222'},
                  {'id': 'username', 'label': 'User', 'value': 'fixture'},
                  {'id': 'password', 'label': 'Password', 'value': 'test-secret'}]
        self.assertEqual(publisher.credential_fields({'fields': fields}),
                         {'host': 'example.invalid', 'port': '2222',
                          'username': 'fixture', 'password': 'test-secret'})
        fields.append({'id': 'duplicate', 'label': 'host', 'value': 'other.invalid'})
        with self.assertRaises(ValueError):
            publisher.credential_fields({'fields': fields})

    def test_proxy_timeout_closes_every_socket(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            config = root / 'config.json'
            config.write_text(json.dumps({'credentials_command': ['credentials'],
                                         'remote_root': '/example',
                                         'ssh_proxy': 'http://proxy.invalid:8080'}))
            (root / 'latest.json').write_text(json.dumps({'build': 1}))
            sockets = [Mock() for _ in range(3)]
            for sock in sockets:
                sock.recv.side_effect = TimeoutError('fixture timeout')
            credentials = json.dumps({'host': 'example.invalid', 'username': 'fixture',
                                      'password': 'test-secret'}).encode()
            with patch('sys.argv', ['publisher', '--config', str(config), '--artifact-dir', directory]), \
                    patch.object(publisher.subprocess, 'check_output', return_value=credentials), \
                    patch.object(publisher.paramiko, 'SSHClient'), \
                    patch.object(publisher.socket, 'create_connection', side_effect=sockets), \
                    patch.object(publisher.time, 'sleep'):
                with self.assertRaises(TimeoutError):
                    publisher.main()
            for sock in sockets:
                sock.close.assert_called()

    def test_reconciliation_still_rejects_other_platforms(self):
        manifest = {'files': {'darwin-arm64': {}}, 'build': 1}
        with self.assertRaises(ValueError):
            validate_next(manifest, manifest)

    def test_rolled_back_build_is_rejected_before_publication(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            staged = root / 'staged'
            staged.mkdir()
            (root / 'hub.json').write_text('{}')
            (staged / 'latest.json').write_text(json.dumps({'build': 42}))
            state = {'auto': True, 'can_update': True,
                     'latest': {'build': 42, 'bad': True}}
            with patch.object(promoter, 'api', return_value=state) as api, \
                    patch.object(promoter, 'nodes', return_value={}):
                with self.assertRaisesRegex(RuntimeError, 'already rolled back'):
                    promoter.promote({'root': directory, 'staged': str(staged)})
            api.assert_called_once_with(root, 'hub/update')
            self.assertTrue((staged / 'latest.json').exists())
            self.assertFalse((root / 'releases/hub/latest.json').exists())


if __name__ == '__main__':
    unittest.main()
