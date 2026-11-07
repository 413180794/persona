#!/usr/bin/env python3
"""Remove PNG text and EXIF comments without changing pixels or rendering metadata."""
import argparse
import struct
import zlib
from pathlib import Path

SIGNATURE = b"\x89PNG\r\n\x1a\n"
TEXT_CHUNKS = frozenset({b"tEXt", b"zTXt", b"iTXt"})


def chunks(data):
    """Yield validated (kind, raw chunk) pairs from a complete PNG."""
    if not data.startswith(SIGNATURE):
        raise ValueError("Invalid PNG signature")
    offset = len(SIGNATURE)
    first = True
    ended = False
    while offset < len(data):
        if offset + 12 > len(data):
            raise ValueError("Truncated PNG chunk")
        length = struct.unpack_from(">I", data, offset)[0]
        end = offset + 12 + length
        if end > len(data):
            raise ValueError("Truncated PNG payload")
        kind = data[offset + 4:offset + 8]
        payload = data[offset + 8:end - 4]
        checksum = struct.unpack_from(">I", data, end - 4)[0]
        if zlib.crc32(kind + payload) & 0xFFFFFFFF != checksum:
            raise ValueError("Invalid PNG chunk checksum")
        if first and (kind != b"IHDR" or length != 13):
            raise ValueError("Invalid PNG image header")
        if ended:
            raise ValueError("Unexpected data after PNG end")
        first = False
        if kind == b"IEND":
            if length != 0:
                raise ValueError("Invalid PNG end chunk")
            ended = True
        yield kind, data[offset:end]
        offset = end
    if not ended:
        raise ValueError("Missing PNG end chunk")


def strip_text_metadata(data):
    """Keep every non-text chunk byte-for-byte, including IDAT and color profiles."""
    records = list(chunks(data))
    return SIGNATURE + b"".join(raw for kind, raw in records if kind not in TEXT_CHUNKS)


def strip_exif_user_comments(payload):
    """Remove TIFF UserComment entries and bytes without relocating other fields."""
    prefix = b"Exif\x00\x00" if payload.startswith(b"Exif\x00\x00") else b""
    data = payload[len(prefix):]
    if len(data) < 8 or data[:2] not in (b"II", b"MM"):
        raise ValueError("Invalid EXIF TIFF header")
    endian = "<" if data[:2] == b"II" else ">"

    def read(fmt, offset):
        size = struct.calcsize(fmt)
        if offset < 0 or offset + size > len(data):
            raise ValueError("Invalid EXIF offset")
        return struct.unpack_from(endian + fmt, data, offset)[0]

    if read("H", 2) != 42:
        raise ValueError("Unsupported EXIF TIFF format")
    widths = {1: 1, 2: 1, 3: 2, 4: 4, 5: 8, 6: 1, 7: 1,
              8: 2, 9: 4, 10: 8, 11: 4, 12: 8, 13: 4}
    pointer_tags = {0x8769, 0x8825, 0xA005}
    tables = []
    protected = []
    comments = []
    visited = set()
    active = set()

    def visit(offset):
        if not offset:
            return
        if offset in active or len(active) >= 32:
            raise ValueError("Cyclic or excessively nested EXIF directory")
        if offset in visited:
            return
        if offset < 8:
            raise ValueError("Invalid EXIF directory offset")
        active.add(offset)
        count = read("H", offset)
        end = offset + 2 + count * 12 + 4
        if end > len(data):
            raise ValueError("Truncated EXIF directory")
        entries = []
        children = []
        for index in range(count):
            start = offset + 2 + index * 12
            tag, kind = read("H", start), read("H", start + 2)
            amount = read("I", start + 4)
            if kind not in widths:
                raise ValueError("Unsupported EXIF field type")
            size = amount * widths[kind]
            value_start = start + 8 if size <= 4 else read("I", start + 8)
            value_end = value_start + size
            if value_start < 0 or (size > 4 and value_start < 8) or value_end > len(data):
                raise ValueError("Truncated EXIF field")
            if tag == 0x9286:
                if size > 4:
                    comments.append((value_start, value_end))
            else:
                entries.append(data[start:start + 12])
                if size > 4:
                    protected.append((value_start, value_end))
            if tag in pointer_tags:
                if kind not in (4, 13) or amount != 1:
                    raise ValueError("Invalid EXIF directory pointer")
                children.append(read("I", start + 8))
        following = read("I", end - 4)
        tables.append((offset, end, count, entries, data[end - 4:end]))
        for child in children:
            visit(child)
        visit(following)
        active.remove(offset)
        visited.add(offset)

    visit(read("I", 4))
    ranges = sorted((start, end) for start, end, _, _, _ in tables)
    if any(right > next_left for (_, right), (next_left, _) in zip(ranges, ranges[1:])):
        raise ValueError("Overlapping EXIF directories")
    changed = any(len(entries) != count for _, _, count, entries, _ in tables)
    if not changed:
        return payload
    for start, end in comments:
        if start < 8 or any(start < right and left < end
                            for left, right in protected + [(a, b) for a, b, _, _, _ in tables]):
            raise ValueError("EXIF comment overlaps retained metadata")
    out = bytearray(data)
    for start, end in comments:
        out[start:end] = bytes(end - start)
    for start, end, count, entries, following in tables:
        if len(entries) == count:
            continue
        replacement = struct.pack(endian + "H", len(entries)) + b"".join(entries) + following
        out[start:end] = replacement + bytes(end - start - len(replacement))
    return prefix + bytes(out)


def strip_private_metadata(data):
    """Remove PNG text and EXIF comments, preserving pixels and other metadata."""
    records = list(chunks(data))
    kept = []
    for kind, raw in records:
        if kind in TEXT_CHUNKS:
            continue
        if kind == b"eXIf":
            payload = strip_exif_user_comments(raw[8:-4])
            raw = (struct.pack(">I", len(payload)) + kind + payload
                   + struct.pack(">I", zlib.crc32(kind + payload) & 0xFFFFFFFF))
        kept.append(raw)
    return SIGNATURE + b"".join(kept)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("files", nargs="+", type=Path)
    args = parser.parse_args()
    changed = 0
    for path in args.files:
        before = path.read_bytes()
        after = strip_private_metadata(before)
        if before != after:
            path.write_bytes(after)
            changed += 1
    print(f"PNG metadata: {changed} files updated; pixels and rendering metadata preserved")


if __name__ == "__main__":
    main()
