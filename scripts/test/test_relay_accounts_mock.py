#!/usr/bin/env python3
"""The mock's quota-relay sidebar page (#4576): it lists the nav_page and answers with the fixture's Views; no port."""
import importlib.util
import json
from pathlib import Path
import unittest

ROOT = Path(__file__).parents[2]
spec = importlib.util.spec_from_file_location('relay_accounts', ROOT / 'scripts/mock/relay_accounts.py')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
FIXTURE = ROOT / 'web/src/views/extensions/testdata/relay-accounts-view.json'
PATH = '/api/v1/extensions/quota-relay/-/ui/sidebar'


class Handler:
    def __init__(self, command, path=''):
        self.command, self.path = command, path
        self.answer = None

    def send_json(self, answer, status=200):
        self.answer = answer


class RelayAccountsMockTests(unittest.TestCase):
    def setUp(self):
        self.mock = module.RelayAccountsMock(FIXTURE)

    def post(self, request):
        h = Handler('POST')
        self.assertTrue(self.mock.route(h, PATH, json.dumps(request).encode()))
        return h.answer

    def testTheSlotsListHasThePage(self):
        for placement in ('', 'nav_page'):
            h = Handler('GET', '/api/v1/extensions/-/ui' + ('?placement=' + placement if placement else ''))
            self.mock.wrap(h, '/api/v1/extensions/-/ui').send_json({'ui': []})
            self.assertEqual([u['id'] for u in h.answer['ui']], ['sidebar'])
        h = Handler('GET', '/api/v1/extensions/-/ui?placement=launcher_section')
        self.mock.wrap(h, '/api/v1/extensions/-/ui').send_json({'ui': []})
        self.assertEqual(h.answer['ui'], [])
        other = Handler('POST')
        self.assertIs(self.mock.wrap(other, '/api/v1/extensions/other/-/ui/x'), other)

    def testEachControlGetsItsView(self):
        default = self.post({})['view']
        self.assertEqual(default['state'], self.mock.views['default']['state'])
        kiro = self.post({'action': 'type', 'values': {'type': 'kiro'}, 'state': default['state']})['view']
        self.assertEqual(kiro['state'], self.mock.views['kiro']['state'])
        # Kiro stays picked through the filter and the search (the extension keeps it in its state).
        cooled = self.post({'action': 'status', 'values': {'status': 'cooldown', 'q': ''}, 'state': kiro['state']})['view']
        self.assertEqual(cooled['filter']['value'], 'cooldown')
        self.assertEqual(cooled['sections'][0]['items'][0]['value'], 'kiro')
        searched = self.post({'action': 'search', 'values': {'q': 'kiro-4*', 'status': 'all'}, 'state': kiro['state']})['view']
        self.assertEqual(searched['search']['value'], 'kiro-4*')
        self.assertEqual(self.post({'action': 'refresh'})['toast'], '已刷新')
        self.assertNotIn('value', self.mock.views['default']['search'], 'the fixture itself is left alone')

    def testAnAccountsPage(self):
        row = next(i for s in self.mock.views['default']['sections'][1:] for i in s['items'] if i['type'] == 'row')
        push = row['action']['push']
        self.assertEqual(self.post({'state': push['state']})['view'], self.mock.views['detail'])
        self.assertEqual(self.post({'action': row['action']['id']})['view'], self.mock.views['detail'])

    def testTheFirstRenderCanStartElsewhere(self):
        for start in ('kiro', 'detail'):
            self.mock = module.RelayAccountsMock(FIXTURE, start)
            first = self.post({})['view']
            self.assertEqual(first, self.mock.views[start])
            # Drawn again (refresh_after) with its own state: still that view.
            self.assertEqual(self.post({'state': first['state']})['view'], first)
        with self.assertRaises(ValueError):
            module.RelayAccountsMock(FIXTURE, 'search')

    def testOtherRoutesAreNotIntercepted(self):
        self.assertFalse(self.mock.route(Handler('POST'), '/api/v1/extensions/other/-/ui/sidebar', b'{}'))
        self.assertFalse(self.mock.route(Handler('GET'), PATH, b''))


if __name__ == '__main__':
    unittest.main()
