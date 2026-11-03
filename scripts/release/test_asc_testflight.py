"""Beta group lookup in asc_testflight.py: duplicates, flaky listings, and when a group may be created (#945)."""
import atexit
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

HERE = Path(__file__).resolve().parent
_config = tempfile.NamedTemporaryFile("w", suffix=".json", delete=False)
json.dump({"app": {"bundle_id": "com.example.app"},
           "app_store": {"app_id": "1234567890", "external": {"group": "Friends"}}}, _config)
_config.close()
atexit.register(os.unlink, _config.name)
os.environ["PERSONA_CONFIG"] = _config.name
sys.path.insert(0, str(HERE))
sys.path.insert(0, str(HERE.parent))
import asc_testflight as tf  # noqa: E402


def group(gid, name, created, internal=False):
    return {"id": gid, "attributes": {"name": name, "isInternalGroup": internal, "createdDate": created,
                                      "publicLinkEnabled": False}}


INTERNAL = group("int", "Internal", "2026-10-24T13:18:34Z", internal=True)
FRIENDS_OLD = group("old", "Friends", "2026-10-28T12:52:00Z")
FRIENDS_NEW = group("new", "Friends", "2026-11-03T14:03:00Z")


class FindGroupTests(unittest.TestCase):
    def setUp(self):
        patcher = patch.object(tf.time, "sleep")
        patcher.start()
        self.addCleanup(patcher.stop)
        self.addCleanup(setattr, tf, "CREATE_GROUPS", False)

    def test_duplicates_resolve_to_the_oldest(self):
        with patch.object(tf, "get_all", return_value=[FRIENDS_NEW, INTERNAL, FRIENDS_OLD]):
            self.assertEqual(tf.find_group("Friends", internal=False)["id"], "old")

    def test_kind_must_match(self):
        with patch.object(tf, "get_all", return_value=[group("x", "Friends", "2026-01-01", internal=True)]):
            self.assertIsNone(tf.find_group("Friends", internal=False))

    def test_empty_listing_then_complete_one(self):
        with patch.object(tf, "get_all", side_effect=[[], [INTERNAL, FRIENDS_OLD]]):
            self.assertEqual(tf.find_group("Friends", internal=False)["id"], "old")

    def test_timeout_then_listing(self):
        with patch.object(tf, "get_all", side_effect=[TimeoutError("read timed out"), [FRIENDS_OLD]]):
            self.assertEqual(tf.find_group("Friends", internal=False)["id"], "old")

    def test_missing_group_is_not_created_by_wait(self):
        with patch.object(tf, "get_all", return_value=[INTERNAL]), patch.object(tf, "request") as request:
            with self.assertRaises(tf.GroupMissing):
                tf.external_group_id()
        request.assert_not_called()

    def test_missing_internal_group_is_not_created_by_wait(self):
        with patch.object(tf, "get_all", return_value=[]), patch.object(tf, "request") as request:
            with self.assertRaises(tf.GroupMissing):
                tf.group_id()
        request.assert_not_called()

    def test_setup_creates_a_missing_group(self):
        tf.CREATE_GROUPS = True
        created = group("made", "Friends", "2026-11-04T00:00:00Z")
        with patch.object(tf, "get_all", return_value=[INTERNAL]), \
                patch.object(tf, "request", return_value={"data": created}) as request, \
                patch.object(tf, "public_link"):
            self.assertEqual(tf.external_group_id(), "made")
        self.assertEqual(request.call_args[0][:2], ("POST", "/v1/betaGroups"))

    def test_share_external_reports_a_missing_group_without_failing(self):
        with patch.object(tf, "get_all", return_value=[INTERNAL]), patch.object(tf, "request") as request:
            tf.share_external({"id": "b", "attributes": {"version": "1"}})
        request.assert_not_called()


if __name__ == "__main__":
    unittest.main()
