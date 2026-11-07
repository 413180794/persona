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


def exif_fixture(endian="<", comment=b"ASCII\x00\x00\x00fixture-user-comment"):
    order = b"II" if endian == "<" else b"MM"
    pack = lambda fmt, *values: struct.pack(endian + fmt, *values)
    child_offset = 8 + 2 + 2 * 12 + 4
    payload_offset = child_offset + 2 + 4 * 12 + 4
    orientation = pack("HHI", 0x112, 3, 1) + pack("H", 6) + bytes(2)
    pointer = pack("HHII", 0x8769, 4, 1, child_offset)
    entries = [
        pack("HHI", 0x9286, 7, len(comment)) + (comment.ljust(4, b"\x00") if len(comment) <= 4 else pack("I", payload_offset)),
        pack("HHI", 0xA001, 3, 1) + pack("H", 1) + bytes(2),
        pack("HHII", 0xA002, 4, 1, 1),
        pack("HHII", 0xA003, 4, 1, 1),
    ]
    return (order + pack("HI", 42, 8) + pack("H", 2) + orientation + pointer
            + pack("I", 0) + pack("H", 4) + b"".join(entries) + pack("I", 0) + (comment if len(comment) > 4 else b""))


def exif_entries(data, endian):
    root = struct.unpack_from(endian + "I", data, 4)[0]
    count = struct.unpack_from(endian + "H", data, root)[0]
    pointer = next(struct.unpack_from(endian + "I", data, root + 2 + i * 12 + 8)[0]
                   for i in range(count)
                   if struct.unpack_from(endian + "H", data, root + 2 + i * 12)[0] == 0x8769)
    amount = struct.unpack_from(endian + "H", data, pointer)[0]
    return [data[pointer + 2 + i * 12:pointer + 2 + (i + 1) * 12] for i in range(amount)]


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

    def test_removes_nested_exif_comments_without_relocating_other_tags(self):
        for endian in ["<", ">"]:
            with self.subTest(endian=endian):
                before = exif_fixture(endian)
                after = png.strip_exif_user_comments(before)
                expected = [entry for entry in exif_entries(before, endian)
                            if struct.unpack_from(endian + "H", entry)[0] != 0x9286]
                self.assertEqual(exif_entries(after, endian), expected)
                self.assertEqual(after[:38], before[:38])
                self.assertEqual(len(after), len(before))
                self.assertNotIn(b"fixture-user-comment", after)
                self.assertEqual(png.strip_exif_user_comments(after), after)

    def test_removes_inline_exif_comments(self):
        for comment in [b"", b"a", b"abc", b"abcd"]:
            with self.subTest(size=len(comment)):
                after = png.strip_exif_user_comments(exif_fixture(comment=comment))
                self.assertFalse(any(struct.unpack_from("<H", entry)[0] == 0x9286
                                     for entry in exif_entries(after, "<")))

    def test_private_metadata_strip_preserves_every_other_png_chunk(self):
        original = fixture()
        records = list(png.chunks(original))
        before = png.SIGNATURE + b"".join(
            (chunk(b"eXIf", exif_fixture()) if kind == b"IDAT" else b"") + raw
            for kind, raw in records)
        after = png.strip_private_metadata(before)
        self.assertEqual([(kind, raw) for kind, raw in png.chunks(after) if kind != b"eXIf"],
                         [(kind, raw) for kind, raw in records if kind not in png.TEXT_CHUNKS])
        exif = next(raw[8:-4] for kind, raw in png.chunks(after) if kind == b"eXIf")
        self.assertNotIn(b"fixture-user-comment", exif)
        self.assertEqual(png.strip_private_metadata(after), after)

    def test_rejects_invalid_exif_before_writing(self):
        valid = exif_fixture()
        invalid = [b"", b"not TIFF", valid[:-1], valid[:2] + b"\x2b\x00" + valid[4:],
                   valid[:4] + struct.pack("<I", 2) + valid[8:]]
        cyclic = bytearray(valid)
        struct.pack_into("<I", cyclic, 30, 8)
        invalid.append(bytes(cyclic))
        dangling = bytearray(valid)
        struct.pack_into("<I", dangling, 48, len(valid) + 1)
        invalid.append(bytes(dangling))
        overlapping = b"II" + struct.pack("<HIH", 42, 8, 1) + struct.pack("<HHII", 0x8769, 4, 1, 22) + bytes(6)
        invalid.append(overlapping)
        for data in invalid:
            with self.subTest(size=len(data)):
                with self.assertRaises(ValueError):
                    png.strip_exif_user_comments(data)

    def test_bundled_system_icons_have_no_text_metadata(self):
        assets = ROOT / "apple/Persona/Resources/Assets.xcassets/Brands"
        for name in ["calendar", "contacts", "health", "maps", "photos", "reminders"]:
            with self.subTest(icon=name):
                files = sorted((assets / f"ios-app-{name}.imageset").glob("*.png"))
                self.assertTrue(files)
                for path in files:
                    records = list(png.chunks(path.read_bytes()))
                    self.assertFalse(any(kind in png.TEXT_CHUNKS for kind, _ in records))
                    for kind, raw in records:
                        if kind == b"eXIf":
                            self.assertEqual(png.strip_exif_user_comments(raw[8:-4]), raw[8:-4])


if __name__ == "__main__":
    unittest.main()
