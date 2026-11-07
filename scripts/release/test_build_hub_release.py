"""Interrupted build leftovers cannot become extra node platforms on retry."""
import gzip
import hashlib
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import build_hub_release as builder


class PackageReleaseTests(unittest.TestCase):
    def test_retry_manifest_only_sees_this_builds_canonical_archives(self):
        with tempfile.TemporaryDirectory() as directory:
            out = Path(directory)
            binaries = out / 'bin'
            binaries.mkdir()
            platforms = ['linux-amd64', 'darwin-arm64']
            for platform in platforms:
                (binaries / ('personad-' + platform)).write_bytes(platform.encode())
            stale = out / ('personad-linux-amd64-' + 'a' * 64 + '.gz')
            stale.write_bytes(b'interrupted old build')

            def generate(args, root):
                source = Path(args[args.index('--dir') + 1])
                self.assertEqual({p.name for p in source.iterdir()},
                                 {'personad-' + p + '.gz' for p in platforms})
                files = {}
                for platform in platforms:
                    name = 'personad-' + platform + '.gz'
                    data = (source / name).read_bytes()
                    files[platform] = {'name': name, 'size': len(data),
                        'sha256': hashlib.sha256(data).hexdigest(),
                        'binary_sha256': hashlib.sha256(gzip.decompress(data)).hexdigest()}
                (source / 'manifest.json').write_text(json.dumps({'files': files}))

            with patch.object(builder, 'run', generate):
                manifest = builder.package_release(binaries / 'personad-linux-amd64',
                    binaries, out, platforms, 'fixture', '202611070001', 'a' * 40, out)
            self.assertEqual(set(manifest['files']), set(platforms))
            self.assertEqual(stale.read_bytes(), b'interrupted old build')
            self.assertFalse(list(out.glob('.packages-*')))
            for platform, artifact in manifest['files'].items():
                self.assertEqual(gzip.decompress((out / artifact['name']).read_bytes()), platform.encode())


if __name__ == '__main__':
    unittest.main()
