#!/usr/bin/env python3
"""Reject empty, failed or silently skipped Apple unit-test result bundles."""
import argparse
from pathlib import Path
import sys

# Reuse the existing strict xcresulttool test-tree reader. Only the trusted
# repository directory is added, so this helper also works under python -I.
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "app"))
from perf_report import statuses


def verify(bundle, required_suites, runner=None):
    tests, state = statuses(bundle, runner)
    if state["status"] != "ok":
        raise ValueError(state.get("error", "could not read test results"))
    bad = [f"{name}: {status}" for name, status in tests.items() if status != "Passed"]
    if bad:
        raise ValueError("non-passing cases (skips are not passes):\n" + "\n".join(bad))
    for suite in required_suites:
        if not any(suite in name.split("/") for name in tests):
            raise ValueError(f"required suite did not execute: {suite}")
    return len(tests)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("bundle", type=Path)
    parser.add_argument("required_suites", nargs="*")
    args = parser.parse_args()
    try:
        count = verify(args.bundle, args.required_suites)
    except (ValueError, OSError) as error:
        print(f"apple-ci: FAIL {args.bundle.name}: {error}", file=sys.stderr)
        return 1
    print(f"apple-ci: PASS {args.bundle.name}: {count} test cases, zero failed/skipped")
    return 0


if __name__ == "__main__":
    sys.exit(main())
