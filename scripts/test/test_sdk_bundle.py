#!/usr/bin/env python3
"""sdk/go/bundle/sdk.tar.gz (what `personad extension new` writes extensions from) is up to date with sdk/ts and
sdk/templates, and scripts/gen/sync-sdk.sh copies a usable SDK."""

import os
import subprocess
import sys
import tempfile
import unittest

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))


class SDKBundle(unittest.TestCase):
    def test_bundle_is_up_to_date(self):
        r = subprocess.run([sys.executable, os.path.join(ROOT, "scripts", "gen", "sdk_bundle.py"), "--check"],
                           capture_output=True, text=True)
        self.assertEqual(r.returncode, 0, r.stderr)

    def test_sync_copies_both_sdks(self):
        with tempfile.TemporaryDirectory() as dest:
            r = subprocess.run(["sh", os.path.join(ROOT, "scripts", "gen", "sync-sdk.sh"), dest], capture_output=True, text=True)
            self.assertEqual(r.returncode, 0, r.stderr)
            for f in ["sdk/go/go.mod", "sdk/go/ext/ext.go", "sdk/go/wire/wire_gen.go", "sdk/go/exttest/exttest.go",
                      "sdk/ts/src/index.ts", "sdk/ts/src/wire.ts", "sdk/VERSION"]:
                self.assertTrue(os.path.exists(os.path.join(dest, f)), f)
            self.assertFalse(os.path.exists(os.path.join(dest, "sdk/go/bundle")), "the bundle is personad's, not an extension's")


if __name__ == "__main__":
    unittest.main()
