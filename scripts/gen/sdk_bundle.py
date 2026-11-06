#!/usr/bin/env python3
"""Packs the TypeScript SDK and the extension templates into sdk/go/bundle/sdk.tar.gz.

`personad extension new` writes a new extension from them (sdk/go embeds the bundle: go:embed cannot reach outside
its module, and personad builds against sdk/go). The tar is byte for byte the same for the same files (sorted,
no times, no owners), so --check can tell whether it is up to date.

    python3 scripts/gen/sdk_bundle.py           # write it
    python3 scripts/gen/sdk_bundle.py --check   # exit 1 when it is out of date
"""

import gzip
import io
import os
import sys
import tarfile

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
OUT = os.path.join(ROOT, "sdk", "go", "bundle", "sdk.tar.gz")
# What goes in, as paths in the tar: ts/… (the TypeScript SDK) and templates/<name>/….
SOURCES = [("ts", os.path.join(ROOT, "sdk", "ts")), ("templates", os.path.join(ROOT, "sdk", "templates"))]
SKIP_DIRS = {"node_modules", "bin", "dist", ".git"}


def files():
    for prefix, top in SOURCES:
        for dirpath, dirnames, filenames in os.walk(top):
            dirnames[:] = sorted(d for d in dirnames if d not in SKIP_DIRS)
            for name in sorted(filenames):
                if name in {".DS_Store", "bun.lock", "bun.lockb"}:
                    continue
                path = os.path.join(dirpath, name)
                yield prefix + "/" + os.path.relpath(path, top).replace(os.sep, "/"), path
    examples = os.path.join(ROOT, "schema", "extension", "examples")
    for name in sorted(os.listdir(examples)):
        if name == "Manifest.custom-contributions.json" or (name.startswith("UIStream") and name.endswith(".json")):
            yield "ts/fixtures/" + name, os.path.join(examples, name)


def build():
    raw = io.BytesIO()
    with tarfile.open(fileobj=raw, mode="w", format=tarfile.PAX_FORMAT) as tar:
        for name, path in sorted(files()):
            with open(path, "rb") as f:
                data = f.read()
            info = tarfile.TarInfo(name)
            info.size, info.mode, info.mtime = len(data), 0o644, 0
            info.uid = info.gid = 0
            info.uname = info.gname = ""
            tar.addfile(info, io.BytesIO(data))
    out = io.BytesIO()
    with gzip.GzipFile(fileobj=out, mode="wb", mtime=0, compresslevel=9) as gz:
        gz.write(raw.getvalue())
    return out.getvalue()


def main():
    data = build()
    if "--check" in sys.argv[1:]:
        try:
            with open(OUT, "rb") as f:
                current = f.read()
        except FileNotFoundError:
            current = b""
        if current != data:
            print("sdk/go/bundle/sdk.tar.gz is out of date: run python3 scripts/gen/sdk_bundle.py", file=sys.stderr)
            sys.exit(1)
        print("sdk bundle: up to date")
        return
    os.makedirs(os.path.dirname(OUT), exist_ok=True)
    with open(OUT, "wb") as f:
        f.write(data)
    print(f"wrote {os.path.relpath(OUT, ROOT)} ({len(data)} bytes)")


if __name__ == "__main__":
    main()
