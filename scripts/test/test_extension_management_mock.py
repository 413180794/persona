import importlib.util
from pathlib import Path
import unittest
import json

module_path = Path(__file__).resolve().parents[1] / 'mock' / 'extension_management.py'
spec = importlib.util.spec_from_file_location('extension_management', module_path)
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class Handler:
    def __init__(self, path, method):
        self.path, self.command = path, method
        self.answer, self.status = None, None

    def send_json(self, answer, status=200):
        self.answer, self.status = answer, status


class ExtensionManagementMockTests(unittest.TestCase):
    def setUp(self):
        self.plugins = {}
        self.mock = module.ExtensionManagementMock(self.plugins, [], 'example-market')

    def call(self, path, method='GET', body=None):
        handler = Handler(path, method)
        self.assertTrue(self.mock.route(handler, path.split('?')[0], json.dumps(body or {}).encode()))
        return handler

    def test_connector_switch_is_not_independent(self):
        result = self.call('/api/v1/extensions/sample-kit/-/contributions/connectors/source', 'PUT', {'enabled': False})
        self.assertEqual(result.status, 400)
        self.assertTrue(self.mock.active('connectors', 'source'))

    def test_disabling_a_contribution_only_removes_its_offer(self):
        self.assertEqual(len(self.call('/api/v1/extensions/-/themes').answer['themes']), 2)
        self.call('/api/v1/extensions/sample-kit/-/contributions/themes/blue', 'PUT', {'enabled': False})
        self.assertEqual([t['id'] for t in self.call('/api/v1/extensions/-/themes').answer['themes']], ['bad'])
        self.assertTrue(self.plugins['sample-kit']['enabled'])

    def test_dependents_are_not_disabled_before_confirmation(self):
        result = self.call('/api/v1/extensions/sample-kit/-/enabled', 'PUT', {'enabled': False})
        self.assertEqual(result.status, 409)
        self.assertTrue(self.plugins['sample-child']['enabled'])
        result = self.call('/api/v1/extensions/sample-kit/-/enabled', 'PUT', {'enabled': False, 'cascade': True})
        self.assertEqual(result.status, 200)
        self.assertFalse(self.plugins['sample-child']['enabled'])

    def test_partial_member_removal_keeps_unreported_member(self):
        self.call('/api/v1/extensions/sample-pack/-/?members=1', 'DELETE')
        self.assertIn('sample-pack', self.plugins)
        result = self.call('/api/v1/extensions/sample-pack/-/?members=1&cascade=1', 'DELETE')
        self.assertEqual(result.answer['removed_members'], ['sample-kit'])
        self.assertIn('sample-extra', self.plugins)
        self.assertIn('sample-child', self.plugins)

    def test_bundle_requires_each_permission_digest(self):
        self.mock.reset({'uninstalled': True})
        pv = self.call('/api/v1/extensions/-/catalog/preview', 'POST', {'extension': 'sample-pack'}).answer
        body = {'preview_id': pv['preview_id'], 'permissions_digest': pv['permissions_digest']}
        self.assertEqual(self.call('/api/v1/extensions/-/install', 'POST', body).status, 400)
        body['bundle'] = [{k: p[k] for k in ['preview_id', 'permissions_digest']} for p in pv['bundle']]
        self.assertEqual(self.call('/api/v1/extensions/-/install', 'POST', body).status, 200)


if __name__ == '__main__':
    unittest.main()
