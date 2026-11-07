#!/usr/bin/env python3
"""Regression tests for pixel-preserving system-icon metadata removal."""
import importlib.util
import struct
import unittest
import zlib
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("png_metadata", ROOT / "scripts/assets/strip_png_metadata.py")
png = importlib.util.module_from_spec(spec)
spec.loader.exec_module(png)


def chunk(kind, payload):
    return struct.pack(">I", len(payload)) + kind + payload + struct.pack(">I", zlib.crc32(kind + payload) & 0xFFFFFFFF)


def fixture():
    return png.SIGNATURE + b"".join([
        chunk(b"IHDR", struct.pack(">IIBBBBB", 1, 1, 8, 2, 0, 0, 0)),
        chunk(b"gAMA", struct.pack(">I", 45455)),
        chunk(b"sRGB", b"\x00"),
        chunk(b"pHYs", struct.pack(">IIB", 2835, 2835, 1)),
        chunk(b"tEXt", b"Comment\x00example metadata"),
        chunk(b"zTXt", b"Comment\x00\x00" + zlib.compress(b"example metadata")),
        chunk(b"iTXt", b"XML:com.adobe.xmp\x00\x00\x00\x00\x00<fixture>example creator</fixture>"),
        chunk(b"IDAT", zlib.compress(b"\x00\xff\x00\x00")),
        chunk(b"IEND", b""),
    ])


class PNGMetadataTests(unittest.TestCase):
    def test_retains_all_non_text_bytes_and_pixel_stream(self):
        before = fixture()
        after = png.strip_text_metadata(before)
        expected = [(kind, raw) for kind, raw in png.chunks(before) if kind not in png.TEXT_CHUNKS]
        self.assertEqual(list(png.chunks(after)), expected)
        self.assertLess(len(after), len(before))
        self.assertFalse(any(kind in png.TEXT_CHUNKS for kind, _ in png.chunks(after)))

    def test_stripping_is_idempotent(self):
        once = png.strip_text_metadata(fixture())
        self.assertEqual(png.strip_text_metadata(once), once)

    def test_rejects_invalid_or_truncated_chunks_without_writing(self):
        data = fixture()
        for invalid in [b"not a PNG", data[:-1], data + b"trailing data", data[:-12], data[:20] + bytes([data[20] ^ 1]) + data[21:]]:
            with self.subTest(length=len(invalid)):
                with self.assertRaises(ValueError):
                    png.strip_text_metadata(invalid)

    def test_bundled_system_icons_have_no_text_metadata(self):
        assets = ROOT / "apple/Persona/Resources/Assets.xcassets/Brands"
        for name in ["calendar", "contacts", "health", "maps", "photos", "reminders"]:
            with self.subTest(icon=name):
                files = sorted((assets / f"ios-app-{name}.imageset").glob("*.png"))
                self.assertTrue(files)
                for path in files:
                    self.assertFalse(any(kind in png.TEXT_CHUNKS for kind, _ in png.chunks(path.read_bytes())))


if __name__ == "__main__":
    unittest.main()
