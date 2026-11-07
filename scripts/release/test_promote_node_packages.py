import base64
import contextlib
import hashlib
import io
import copy
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import node_artifacts
import promote_hub_release as promote
from test_node_artifacts import BUILD, health, native_result, release


class PromotionNodePackageTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        base = Path(self.temporary.name)
        self.root, self.staged = base / 'runtime', base / 'staged'
        (self.root / 'bin').mkdir(parents=True)
        (self.root / 'hub.json').write_text(json.dumps({'auto_update_nodes': False}))
        self.node_manifest = release(self.staged)
        self.hub_manifest = copy.deepcopy(self.node_manifest)
        self.hub_manifest['files'] = {'linux-amd64': self.hub_manifest['files']['linux-amd64']}
        self.hub_manifest['ancestors'] = base64.b64encode(bytes(6)).decode()
        (self.staged / 'latest.json').write_text(json.dumps(self.hub_manifest))
        old_staged = base / 'old-staged'
        old = release(old_staged, '0' * 40, BUILD - 1)
        with patch.object(node_artifacts.subprocess, 'check_output', return_value=native_result(old)):
            node_artifacts.publish_nodes(self.root, old_staged, old, health(old))
        self.old_pointer = (self.root / 'bin/nodes').resolve()
        self.latest = self.root / 'releases/hub/latest.json'
        self.latest.parent.mkdir(parents=True)
        self.old_latest = json.dumps({'commit': old['commit'], 'build': old['build']}).encode()
        self.latest.write_bytes(self.old_latest)
        binary = self.root / 'bin/personad'
        archive = self.staged / self.hub_manifest['files']['linux-amd64']['name']
        import gzip
        binary.write_bytes(gzip.decompress(archive.read_bytes()))
        self.serving_hash = self.hub_manifest['files']['linux-amd64']['binary_sha256']
        self.calls = []
        self.events = []
        self.inventory = {'machines': [{'id': 'node', 'kind': 'link', 'online': True,
                                       'node_version': 'unchanged', 'deploying': False}]}

    def api(self, root, path, method='GET'):
        self.assertEqual(root, self.root)
        self.calls.append((path, method))
        self.events.append(path)
        self.assertNotIn('/deploy', path)
        if path == 'machines':
            return copy.deepcopy(self.inventory)
        if path == 'hub/update':
            return {'auto': True, 'can_update': True, 'state': 'updated', 'confirmed': True,
                    'latest': {'build': BUILD - 1}}
        if path == 'hub/update/check':
            return {'latest': {'bad': False}}
        if path == 'health':
            return health(self.hub_manifest)
        if path == 'hub/update/history':
            return {'history': []}
        raise AssertionError('Unexpected API path: ' + path)

    def native(self, argv, **kwargs):
        if argv[-1] == 'node-update-check':
            return native_result(self.node_manifest)
        self.assertIn('--status', argv)
        return 'slot a serving version, pid 1234\n'

    def execute(self, enabled=True):
        actual_digest = promote.digest
        def digest(path):
            if str(path) == '/proc/1234/exe':
                return self.serving_hash
            return actual_digest(path)
        with patch.object(promote, 'api', side_effect=self.api), \
                patch.object(promote, 'digest', side_effect=digest), \
                patch.object(promote.subprocess, 'check_output', side_effect=self.native), \
                contextlib.redirect_stdout(io.StringIO()):
            payload = {'root': str(self.root), 'staged': str(self.staged)}
            if enabled is not None:
                payload['publish_nodes'] = enabled
            promote.promote(payload)

    def assert_unchanged_and_no_update_requested(self):
        self.assertEqual(self.latest.read_bytes(), self.old_latest)
        self.assertEqual((self.root / 'bin/nodes').resolve(), self.old_pointer)
        self.assertFalse(any(method == 'POST' for _, method in self.calls))

    def test_prepare_failure_preserves_hub_latest_and_node_pointer(self):
        with patch.object(promote, 'prepare_nodes', side_effect=ValueError('Bad Darwin package')):
            with self.assertRaisesRegex(ValueError, 'Bad Darwin'):
                self.execute()
        self.assert_unchanged_and_no_update_requested()
        self.assertTrue((self.staged / 'latest.json').exists())

    def test_real_bad_foreign_archive_fails_before_hub_update(self):
        artifact = self.node_manifest['files']['darwin-arm64']
        (self.staged / artifact['name']).write_bytes(b'bad foreign archive')
        with self.assertRaises(ValueError):
            self.execute()
        self.assert_unchanged_and_no_update_requested()
        self.assertFalse((self.root / 'releases/nodes' / self.node_manifest['version']).exists())

    def test_reupload_after_preflight_failure_cannot_mutate_published_artifact(self):
        name = self.hub_manifest['files']['linux-amd64']['name']
        published = self.root / 'releases/hub' / name
        with patch.object(promote, 'prepare_nodes', side_effect=ValueError('Bad Darwin package')):
            with self.assertRaises(ValueError):
                self.execute()
        original_hash = hashlib.sha256(published.read_bytes()).hexdigest()
        original_inode = published.stat().st_ino
        source = self.staged / name
        self.assertNotEqual(source.stat().st_ino, original_inode)
        source.write_bytes(b'a partial reupload at the staging path')
        self.assertEqual(hashlib.sha256(published.read_bytes()).hexdigest(), original_hash)
        self.assertEqual(published.stat().st_ino, original_inode)
        self.assert_unchanged_and_no_update_requested()

    def test_serving_process_mismatch_preserves_old_node_pointer(self):
        self.serving_hash = 'f' * 64
        with self.assertRaisesRegex(RuntimeError, 'Serving process checksum mismatch'):
            self.execute()
        self.assertEqual((self.root / 'bin/nodes').resolve(), self.old_pointer)
        self.assertEqual([path for path, method in self.calls if method == 'POST'], ['hub/update/check'])

    def test_missing_opted_in_manifest_cannot_publish_hub(self):
        (self.staged / 'node-manifest.json').unlink()
        with self.assertRaisesRegex(ValueError, 'required'):
            self.execute()
        self.assert_unchanged_and_no_update_requested()

    def test_default_off_ignores_nodes_and_never_calls_prepare_or_activate(self):
        (self.staged / 'node-manifest.json').write_text('invalid ignored metadata')
        with patch.object(promote, 'prepare_nodes') as prepare, \
                patch.object(promote, 'activate_nodes') as activate, \
                patch.object(promote, 'node_manifest') as metadata:
            self.execute(enabled=None)
        prepare.assert_not_called()
        activate.assert_not_called()
        metadata.assert_not_called()
        self.assertEqual((self.root / 'bin/nodes').resolve(), self.old_pointer)
        self.assertEqual([path for path, method in self.calls if method == 'POST'], ['hub/update/check'])
        self.assertEqual(json.loads(self.latest.read_text())['commit'], self.hub_manifest['commit'])

    def test_matching_packages_activate_only_after_confirmed_hub_without_deploy(self):
        actual_activate = promote.activate_nodes
        def activate(*args):
            self.assertIn('health', self.events)
            self.assertIn(('hub/update/check', 'POST'), self.calls)
            self.assertEqual((self.root / 'bin/nodes').resolve(), self.old_pointer)
            return actual_activate(*args)
        with patch.object(promote, 'activate_nodes', side_effect=activate) as activation:
            self.execute()
        activation.assert_called_once()
        self.assertEqual((self.root / 'bin/nodes').resolve().name, self.node_manifest['version'])
        self.assertTrue(node_artifacts.managed_nodes(self.root))
        self.assertEqual([path for path, method in self.calls if method == 'POST'], ['hub/update/check'])
        self.assertEqual(json.loads((self.root / 'hub.json').read_text())['auto_update_nodes'], False)

    def test_unmanaged_pointer_blocks_all_publication_before_any_api(self):
        (self.root / 'bin/nodes').unlink()
        (self.root / 'bin/nodes').mkdir()
        with self.assertRaisesRegex(RuntimeError, 'Unmanaged'):
            self.execute()
        self.assertEqual(self.calls, [])
        self.assertEqual(self.latest.read_bytes(), self.old_latest)


if __name__ == '__main__':
    unittest.main()
