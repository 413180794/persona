#!/usr/bin/env python3
"""Fail if a built app carries placeholder purpose strings (App Review rejects them automatically).

A string catalog (InfoPlist.xcstrings) without an entry for the source language compiles the key itself into
<source>.lproj/InfoPlist.strings, e.g. "NSFaceIDUsageDescription" = "NSFaceIDUsageDescription", and that is what
English users and App Review see. Checks Info.plist and every *.lproj/InfoPlist.strings in the bundle.

  python3 scripts/release/check_bundle_strings.py path/to/Folia.app
"""
import plistlib
import subprocess
import sys
from pathlib import Path


def load_strings(path: Path) -> dict:
    out = subprocess.run(["plutil", "-convert", "xml1", "-o", "-", str(path)], capture_output=True, check=True).stdout
    return plistlib.loads(out)


def check(app: Path) -> list:
    contents = app / "Contents" if (app / "Contents").is_dir() else app  # Mac Catalyst bundles nest under Contents/
    resources = contents / "Resources" if (contents / "Resources").is_dir() else contents
    info = plistlib.loads((contents / "Info.plist").read_bytes())
    problems = []
    for k, v in info.items():
        if k.endswith("UsageDescription") and (not isinstance(v, str) or not v.strip() or v == k):
            problems.append(f"Info.plist: {k} = {v!r}")
    lprojs = sorted(resources.glob("*.lproj/InfoPlist.strings"))
    for f in lprojs:
        for k, v in load_strings(f).items():
            if not str(v).strip() or v == k:
                problems.append(f"{f.parent.name}/InfoPlist.strings: {k} = {v!r}")
    usage_keys = [k for k in info if k.endswith("UsageDescription")]
    print(f"{app.name}: {len(usage_keys)} purpose strings in Info.plist, {len(lprojs)} InfoPlist.strings files")
    return [f"{app.name} {p}" for p in problems]


def main(app: Path) -> int:
    # App extensions (share, widgets, Safari) carry their own Info.plist and InfoPlist.strings.
    bundles = [app] + sorted(app.glob("PlugIns/*.appex")) + sorted(app.glob("Contents/PlugIns/*.appex"))
    problems = [p for b in bundles for p in check(b)]
    for p in problems:
        print("  ❌", p)
    return 1 if problems else 0


if __name__ == "__main__":
    if len(sys.argv) != 2:
        sys.exit(__doc__)
    sys.exit(main(Path(sys.argv[1])))
