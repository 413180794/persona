"""wechat-connector_shots.findings: what may and may not be visible in the README's WeChat pictures."""
import unittest

import wechat-connector_shots as shots


class FindingsTest(unittest.TestCase):
    def test_reserved_examples_pass(self):
        text = "wxid_example_a 示例项目群 10000000001@chatroom https://wechat-connector.example.com me@example.com 127.0.0.1"
        self.assertEqual(shots.findings(text), [])

    def test_other_ids_and_hosts_are_found(self):
        found = shots.findings("wxid_abc123 和 23456789012@chatroom 在 https://wechat.internal.corp/c/1")
        self.assertIn("wxid_abc123", found)
        self.assertIn("23456789012@chatroom", found)
        self.assertIn("wechat.internal.corp", found)

    def test_private_address_is_found(self):
        address = ".".join(('10', '1', '2', '3'))
        self.assertIn(address, shots.findings("hub 在 " + address))


if __name__ == "__main__":
    unittest.main()
