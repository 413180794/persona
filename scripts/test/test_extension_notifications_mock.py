#!/usr/bin/env python3
"""Regression tests for the extension notification mock; no port is opened, no notification is sent, no production data is read."""
import importlib.util
import json
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('extension_notifications', Path(__file__).parents[1] / 'mock/extension_notifications.py')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class Handler:
    def __init__(self, command):
        self.command = command
        self.answer = None

    def send_json(self, answer):
        self.answer = answer


class ExtensionNotificationMockTests(unittest.TestCase):
    def setUp(self):
        self.mock = module.ExtensionNotificationMock()

    def testBackgroundReplyRecordsStateAndMixedResults(self):
        handler = Handler('POST')
        request = {'action': 'reply', 'state': 'notice-state', 'values': {'text': '收到'}}
        self.assertTrue(self.mock.route(handler, '/api/v1/extensions/notice-demo/-/ui/home', json.dumps(request).encode()))
        self.assertEqual(self.mock.writes, [request])
        self.assertEqual(handler.answer['toast'], '已回复：收到')
        self.assertIn('open_url', handler.answer)
        self.assertIn('push', handler.answer)

    def testDrawDoesNotInventAnActionAndResetClearsRecords(self):
        handler = Handler('POST')
        self.mock.route(handler, '/api/v1/extensions/notice-demo/-/ui/home', b'{"state":"notice-state"}')
        self.assertNotIn('action', self.mock.writes[0])
        self.assertEqual(handler.answer['view']['sections'][0]['items'][0]['text'], '从通知打开的页面')
        self.mock.route(handler, '/mock/extension-notifications', b'{}')
        self.assertEqual(handler.answer['writes'], [])

    def testReadinessIsResetBetweenNotificationRuns(self):
        handler = Handler('POST')
        self.mock.route(handler, '/mock/extension-notifications/ready', b'')
        self.assertTrue(handler.answer['ready'])
        self.mock.route(Handler('POST'), '/mock/extension-notifications', b'{}')
        reader = Handler('GET')
        self.mock.route(reader, '/mock/extension-notifications', b'')
        self.assertFalse(reader.answer['ready'])

    def testOtherRoutesAreNotIntercepted(self):
        self.assertFalse(self.mock.route(Handler('POST'), '/api/v1/extensions/other/-/ui/home', b'{}'))
        self.assertFalse(self.mock.route(Handler('GET'), '/api/v1/extensions/notice-demo/-/ui/home', b''))


if __name__ == '__main__':
    unittest.main()
