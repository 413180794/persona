"""The optional paging fixture preserves legacy reads, filters and exact lookup."""
import importlib.util
import os
from pathlib import Path
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location("paged_sessions_mock", Path(__file__).with_name("mock_hub.py"))
hub = importlib.util.module_from_spec(spec)
spec.loader.exec_module(hub)


class PagedSessionsMockTests(unittest.TestCase):
    def setUp(self):
        env = patch.dict(os.environ, {"MOCK_SESSIONS_LIST": "1"})
        env.start()
        self.addCleanup(env.stop)

    def read(self, **query):
        body, status = hub.paged_sessions({key: [str(value)] for key, value in query.items()})
        self.assertEqual(status, 200, body)
        return body

    def test_paging_is_opt_in_and_keeps_unqueried_reads_complete(self):
        self.assertIn("sessions_list", hub.features())
        self.assertIn("sessions_lookup", hub.features())
        full = self.read()
        self.assertEqual(set(full), {"sessions"})
        with patch.dict(os.environ, {"MOCK_SESSIONS_LIST": "0"}):
            self.assertNotIn("sessions_list", hub.features())
            self.assertNotIn("sessions_lookup", hub.features())
            self.assertEqual(self.read(summary=1, limit=2), full)

    def test_paged_rows_counts_and_exact_lookup_use_the_same_collection(self):
        full = self.read()["sessions"]
        counts = self.read(counts=1)
        self.assertEqual(counts["total"], len(full))
        for dimension in ("machine", "group", "project", "state"):
            self.assertEqual(sum(counts[dimension].values()), len(full))
        first = self.read(summary=1, limit=2)
        self.assertEqual(len(first["sessions"]), 2)
        self.assertTrue(first["has_more"])
        self.assertIn("waiting", first["sessions"][0])
        second = self.read(summary=1, limit=2, cursor=first["next_cursor"])
        self.assertEqual(second["revision"], first["revision"])
        self.assertNotEqual(first["sessions"][0]["session_id"], second["sessions"][0]["session_id"])
        row = first["sessions"][0]
        exact = self.read(summary=0, limit=50, machine_id=row["machine_id"], session_id=row["session_id"])
        self.assertEqual(exact["total"], 1)
        self.assertFalse(exact["has_more"])
        self.assertNotIn("next_cursor", exact)
        self.assertNotIn("waiting", exact["sessions"][0])
        self.assertEqual(exact["sessions"][0]["session_id"], row["session_id"])
        self.assertEqual(self.read(summary=0, session_id="missing")["sessions"], [])

    def test_filter_counts_match_filtered_pages(self):
        row = self.read()["sessions"][0]
        filters = {"machine_id": row["machine_id"], "state": row["state"], "q": row["title"]}
        page = self.read(summary=1, limit=200, **filters)
        counts = self.read(counts=1, **filters)
        self.assertGreater(page["total"], 0)
        self.assertEqual(counts["total"], page["total"])
        self.assertTrue(all(s["machine_id"] == row["machine_id"] and s["state"] == row["state"] for s in page["sessions"]))

    def test_unchanged_revision_has_no_delta_and_stale_revision_rebaselines(self):
        first = self.read(summary=1, limit=2)
        delta = self.read(summary=1, since=first["revision"])
        self.assertEqual(delta["sessions"], [])
        self.assertEqual(delta["remove"], [])
        self.assertFalse(delta["has_more"])
        for query, code in (({"since": ["expired"]}, "revision_expired"), ({"cursor": ["expired:2"]}, "cursor_expired")):
            body, status = hub.paged_sessions(query)
            self.assertEqual(status, 409)
            self.assertEqual(body["code"], code)


if __name__ == "__main__":
    unittest.main()
