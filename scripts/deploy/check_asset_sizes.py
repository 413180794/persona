#!/usr/bin/env python3
"""Check the Workers per-file limit on static assets before deploying."""
from pathlib import Path
import sys


MAX_ASSET_BYTES = 25 * 1024 * 1024


def oversized_assets(root):
    return [(path.relative_to(root), path.stat().st_size)
            for path in sorted(root.rglob("*")) if path.is_file() and path.stat().st_size > MAX_ASSET_BYTES]


def main():
    if len(sys.argv) != 2 or not Path(sys.argv[1]).is_dir():
        sys.exit("usage: check_asset_sizes.py <assets-directory>")
    oversized = oversized_assets(Path(sys.argv[1]))
    for path, size in oversized:
        print(f"{path}: {size / 1024 / 1024:.1f} MiB exceeds Workers' 25 MiB limit")
    if oversized:
        sys.exit("check_asset_sizes: deployment stopped before upload")
    print("check_asset_sizes: all assets within 25 MiB")


if __name__ == "__main__":
    main()
