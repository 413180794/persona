"""Extension notification fixture that runs only in its own mock; every action is only recorded in memory."""
import json
import threading


class ExtensionNotificationMock:
    def __init__(self):
        self.lock = threading.Lock()
        self.writes = []
        self.ready = False

    def route(self, handler, path, body):
        if path == '/mock/extension-notifications/ready' and handler.command == 'POST':
            with self.lock:
                self.ready = True
            handler.send_json({'ready': True})
            return True
        if path == '/mock/extension-notifications':
            with self.lock:
                if handler.command == 'POST':
                    self.writes = []
                    self.ready = False
                result = {'writes': list(self.writes), 'ready': self.ready}
            handler.send_json(result)
            return True
        if path != '/api/v1/extensions/notice-demo/-/ui/home' or handler.command != 'POST':
            return False
        request = json.loads(body or b'{}')
        with self.lock:
            self.writes.append(request)
        action = request.get('action')
        view = {'title': '通知扩展页面', 'state': 'after-action', 'sections': [
            {'title': '示例状态', 'items': [
                {'type': 'text', 'text': '通知操作已执行' if action else '从通知打开的页面'},
                {'type': 'text', 'text': '原始通知状态：' + request.get('state', '')},
                {'type': 'metric', 'label': '示例待处理', 'value': '12'}]}]}
        result = {'view': view}
        if action == 'reply':
            result.update(toast='已回复：' + request.get('values', {}).get('text', ''),
                          open_url='https://example.com/ignored', push={'ui': 'ignored'})
        elif action:
            result['toast'] = '示例处理完成'
        handler.send_json(result)
        return True
