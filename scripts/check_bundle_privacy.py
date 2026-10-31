#!/usr/bin/env python3
"""Fail when a built app bundle holds this person's private values: anyone with the TestFlight public link can install it.

    python3 scripts/check_bundle_privacy.py <Jarvis.app>     # the watch app and extensions inside are scanned too
    python3 scripts/check_bundle_privacy.py --whole-short <dir>  # published jarvisd builds (scripts/publish.sh)

The values are scripts/privacy_check.py's (config, private-words.txt, CLAUDE.local.md, the home directory), plus private
IPv4 addresses and emails outside placeholder domains, looked for in every file's printable strings (like `strings`,
after decompressing lzfse files) and in binary plists. The bundle id and team id are in every signed build on purpose
and are removed before matching.
Signing data is Apple's and is skipped: embedded.mobileprovision, _CodeSignature, and the code signature at the end of
each Mach-O (certificate names; the archive's development profile lists device ids, which export replaces anyway).
Values match as written or lowercased, not in any case: Swift's mangled symbol names are full of short fragments
that equal a value in another case.
The repo's own Go module path (it names the GitHub owner) is in every Go binary's imports and is removed too, as
scripts/privacy_check.py does for the sources. --whole-short: a value of four characters or fewer only counts as a
whole word, not inside a longer one (a Go binary carries Unicode tables and minified JavaScript, where short values turn
up inside ordinary words: a surname inside "touchend"); app bundles keep the strict substring match.
Prints the file and the kind of hit, never the value.
"""
import os
import re
import subprocess
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import jarvis_config  # noqa: E402
import privacy_check as pc  # noqa: E402

SKIP = {"embedded.mobileprovision", "CodeResources"}
PRINTABLE = re.compile(rb"[\x20-\x7e]{4,}")


def signature_offset(path: str):
    """Where a Mach-O's code signature starts (LC_CODE_SIGNATURE dataoff), or None for other files."""
    out = subprocess.run(["otool", "-l", path], capture_output=True, text=True).stdout
    m = re.search(r"cmd LC_CODE_SIGNATURE\s+cmdsize \d+\s+dataoff (\d+)", out)
    return int(m.group(1)) if m else None


def texts(path: str):
    data = open(path, "rb").read()
    if data[:3] == b"bvx":  # lzfse (App Intents metadata): compressed, values would be split across matches
        data = subprocess.run(["compression_tool", "-decode", "-a", "lzfse", "-i", path, "-o", "/dev/stdout"],
                              capture_output=True).stdout
    if data[:4] in (b"\xcf\xfa\xed\xfe", b"\xca\xfe\xba\xbe"):  # Mach-O, fat
        off = signature_offset(path)
        if off:
            data = data[:off]
    yield from (m.group(0).decode("ascii") for m in PRINTABLE.finditer(data))
    if data[:6] == b"bplist":
        yield subprocess.run(["plutil", "-convert", "xml1", "-o", "-", path], capture_output=True, text=True).stdout


def main():
    args = sys.argv[1:]
    whole_short = "--whole-short" in args
    args = [a for a in args if a != "--whole-short"]
    if len(args) != 1 or not os.path.isdir(args[0]):
        sys.exit(__doc__)
    root = args[0]
    modules = pc.module_paths()
    cfg = jarvis_config.load()
    inherent = [v for v in (jarvis_config.get(cfg, "app.bundle_id", ""), jarvis_config.get(cfg, "app.team_id", "")) if v]
    words, _ = pc.terms()
    words = [w for w in words if w not in inherent]
    bad = []
    for dirpath, dirnames, files in os.walk(root):
        dirnames[:] = [d for d in dirnames if d != "_CodeSignature"]
        for f in files:
            if f in SKIP:
                continue
            path = os.path.join(dirpath, f)
            kinds = set()
            for t in texts(path):
                for v in inherent:
                    t = t.replace(v, "")
                if modules:
                    t = modules.sub("", t)
                for w in words:
                    if whole_short and len(w) <= 4:
                        if re.search(r"(?<![A-Za-z])(%s|%s)(?![A-Za-z])" % (re.escape(w), re.escape(w.lower())), t):
                            kinds.add("private value (%d chars)" % len(w))
                    elif w in t or w.lower() in t:
                        kinds.add("private value (%d chars)" % len(w))
                for w in pc.problems(t, []):  # IPs and emails only
                    kinds.add("private IP" if pc.IPV4.fullmatch(w) else "email")
            bad += [(os.path.relpath(path, root), k) for k in sorted(kinds)]
    for f, k in bad:
        print("%s: %s" % (f, k))
    if bad:
        sys.exit("check_bundle_privacy: private values in %s (scripts/check_bundle_privacy.py)" % root)
    print("check_bundle_privacy: clean (%d values checked)" % len(words))


if __name__ == "__main__":
    main()
