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
For public Go builds, narrowly scoped dependency constants are classified separately from private data. This requires
unreplaced dependencies whose build-info checksums match server/go.sum; registered personal values always win.
"""
import ipaddress
import os
import re
import subprocess
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import jarvis_config  # noqa: E402
import privacy_check as pc  # noqa: E402

SKIP = {"embedded.mobileprovision", "CodeResources"}
PRINTABLE = re.compile(rb"[\x20-\x7e]{4,}")


# These are protocol and standards constants, not accounts or deployment addresses. Keep contexts narrow:
# OpenSSH's global request name, the upstream web client's exact contact link, and complete RFC 1918 networks.
SSH_REQUEST = re.compile(r"(?<![A-Za-z0-9._%+-])ping@openssh[.]com(?![A-Za-z0-9._%+-])")
TAILSCALE_CONTACT = re.compile(r'<a href="mailto:support\+webclient@tailscale[.]com" class="link">contact us</a>')
RFC1918_NETWORKS = tuple(ipaddress.ip_network((address, prefix)) for address, prefix in (
    (0x0A000000, 8), (0xAC100000, 12), (0xC0A80000, 16),
))
# RFC 3927 link-local address used by AWS, Google Cloud and Azure for instance metadata.
CLOUD_METADATA_IP = ipaddress.ip_address(0xA9FEA9FE)


def go_dependencies(path, sums_path=None):
    """Only classify constants from unreplaced modules recorded in both this Go executable and go.sum."""
    sums_path = sums_path or os.path.join(pc.ROOT, "server", "go.sum")
    try:
        with open(sums_path) as sums:
            trusted = {tuple(line.split()) for line in sums if len(line.split()) == 3}
        result = subprocess.run(["go", "version", "-m", path], capture_output=True, text=True, timeout=10)
    except (OSError, subprocess.TimeoutExpired):
        return set()
    if result.returncode:
        return set()
    dependencies = set()
    previous = None
    public_build = False
    for line in result.stdout.splitlines():
        fields = line.split()
        if fields == ["build", "-tags=public"]:
            public_build = True
        if fields[:1] == ["dep"] and len(fields) == 4:
            previous = fields[1]
            if tuple(fields[1:]) in trusted:
                dependencies.add(previous)
        elif fields[:1] == ["=>"]:
            dependencies.discard(previous)
        elif fields[:1] in (["mod"], ["dep"]):
            previous = None
    return dependencies if public_build else set()


def generic_text(text, dependencies):
    """Remove proven public constant occurrences only for generic IP/email detection, never private-word matching."""
    if "golang.org/x/crypto" in dependencies:
        text = SSH_REQUEST.sub("", text)
    if "github.com/tailscale/web-client-prebuilt" in dependencies:
        text = TAILSCALE_CONTACT.sub("", text)
    if "tailscale.com" not in dependencies:
        return text

    def public_address(match):
        address = match.group(1)
        try:
            ip = ipaddress.ip_address(address)
        except ValueError:
            return match.group(0)
        suffix = text[match.end():]
        for network in RFC1918_NETWORKS:
            if ip != network.network_address:
                continue
            if re.match(r"/%d(?!\d)" % network.prefixlen, suffix):
                return ""
            # ipnlocal's private and link-local CIDRs are consecutive Go string constants.
            link_local = str(ipaddress.ip_network((0xA9FE0000, 16)))
            if network.prefixlen == 16 and suffix.startswith("/16" + link_local):
                return ""
        if ip == CLOUD_METADATA_IP:
            # Go concatenates string constants in rodata, so the HTTP literal can touch adjacent strings.
            if text[max(0, match.start() - 7):match.start()] == "http://":
                return ""
            # cloudenv's adjacent metadata/resolver constants provide a specific non-URL context.
            resolver = str(ipaddress.ip_address(int(CLOUD_METADATA_IP) - 1))
            if suffix.startswith(resolver + "Metadata-Flavor"):
                return ""
        return match.group(0)

    return pc.IPV4.sub(public_address, text)


def text_kinds(text, words, whole_short=False, dependencies=frozenset()):
    kinds = set()
    for word in words:
        if whole_short and len(word) <= 4:
            if re.search(r"(?<![A-Za-z])(%s|%s)(?![A-Za-z])" % (re.escape(word), re.escape(word.lower())), text):
                kinds.add("private value (%d chars)" % len(word))
        elif word in text or word.lower() in text:
            kinds.add("private value (%d chars)" % len(word))
    for word in pc.problems(generic_text(text, dependencies), []):
        kinds.add("private IP" if pc.IPV4.fullmatch(word) else "email")
    return kinds


def signature_offset(path: str):
    """Where a Mach-O's code signature starts (LC_CODE_SIGNATURE dataoff), or None for other files."""
    out = subprocess.run(["otool", "-l", path], capture_output=True, text=True).stdout
    m = re.search(r"cmd LC_CODE_SIGNATURE\s+cmdsize \d+\s+dataoff (\d+)", out)
    return int(m.group(1)) if m else None


def texts(path: str):
    with open(path, "rb") as source:
        data = source.read()
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
            dependencies = go_dependencies(path) if whole_short else set()
            for t in texts(path):
                for v in inherent:
                    t = t.replace(v, "")
                if modules:
                    t = modules.sub("", t)
                kinds.update(text_kinds(t, words, whole_short, dependencies))
            bad += [(os.path.relpath(path, root), k) for k in sorted(kinds)]
    for f, k in bad:
        print("%s: %s" % (f, k))
    if bad:
        sys.exit("check_bundle_privacy: private values in %s (scripts/check_bundle_privacy.py)" % root)
    print("check_bundle_privacy: clean (%d values checked)" % len(words))


if __name__ == "__main__":
    main()
