#!/usr/bin/env python3
"""Remove PNG text metadata without re-encoding pixels or rendering chunks."""
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


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("files", nargs="+", type=Path)
    args = parser.parse_args()
    changed = 0
    for path in args.files:
        before = path.read_bytes()
        after = strip_text_metadata(before)
        if before != after:
            path.write_bytes(after)
            changed += 1
    print(f"PNG metadata: {changed} files updated; rendering chunks preserved")


if __name__ == "__main__":
    main()
