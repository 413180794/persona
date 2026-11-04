#!/usr/bin/env python3
"""Fail when a built app bundle holds this person's private values: anyone with the TestFlight public link can install it.

    python3 scripts/release/check_bundle_privacy.py <Jarvis.app>     # the watch app and extensions inside are scanned too
    python3 scripts/release/check_bundle_privacy.py <Mac Jarvis.app> # the Mac app (Contents/…), same rules
    python3 scripts/release/check_bundle_privacy.py --whole-short <dir>  # published jarvisd builds (scripts/release/publish.sh)

The values are scripts/privacy_check.py's (config, private-words.txt, CLAUDE.local.md, the home directory), plus private
IPv4 addresses and emails outside placeholder domains, looked for in every file's printable strings (like `strings`,
after decompressing lzfse files) and in binary plists. The bundle id and team id are in every signed build on purpose
and are removed before matching. App mode also recognizes only the exact configured public HTTPS /relay endpoint, and
--whole-short only the exact public download address (publish.site_url + /dl, the hub's update URL, docs/hub-update.md);
their host in any other context, related paths and credentials remain subject to private-value checks.
Signing data is Apple's and is skipped: embedded.mobileprovision (embedded.provisionprofile on the Mac), _CodeSignature,
and each Mach-O slice's validated embedded-signature blob (certificate names; the archive's development profile lists
device ids, which export replaces anyway). A universal (fat) binary's remaining bytes are scanned across every slice;
its signature offsets are relative to its slice, and cutting at the first would skip the rest of the file.
Values match as written or lowercased, not in any case: Swift's mangled symbol names are full of short fragments
that equal a value in another case.
The repo's own Go module path (it names the GitHub owner) is in every Go binary's imports and is removed too, as
scripts/privacy_check.py does for the sources. --whole-short: a value of four characters or fewer only counts as a
whole word, not inside a longer one (a Go binary carries Unicode tables and minified JavaScript, where short values turn
up inside ordinary words: a surname inside "touchend"); app bundles keep the strict substring match.
Prints the file and the kind of hit, never the value.
For public Go builds, narrowly scoped dependency constants are classified separately from private data. This requires
unreplaced dependencies whose build-info checksums match server/go.sum; registered personal values always win.
The seven known swift-nio-ssh algorithm/certificate names are also classified as a complete original printable field
or a complete comma-separated list; that fallback excludes URLs, surrounding context and normalized fragments.
"""
import ipaddress
import os
import re
import struct
import subprocess
import sys
from urllib.parse import urlsplit

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import jarvis_config  # noqa: E402
import privacy_check as pc  # noqa: E402

SKIP = {"embedded.mobileprovision", "embedded.provisionprofile", "CodeResources"}
PRINTABLE = re.compile(rb"[\x20-\x7e]{4,}")

# These are protocol and standards constants, not accounts or deployment addresses. Keep contexts narrow:
# SSH wire identifiers, the upstream web client's exact contact link, and complete RFC 1918 networks.
# Exact names from x/crypto/ssh, gliderssh and pkg/sftp; never exempt either domain as a whole.
GO_SSH_IDENTIFIERS = re.compile(
    r"(?<![A-Za-z0-9._%+-])(?:"
    r"(?:aes128-gcm|aes256-gcm|chacha20-poly1305|"
    r"hmac-sha2-256-etm|hmac-sha2-512-etm|"
    r"rsa-sha2-256-cert-v01|rsa-sha2-512-cert-v01|ssh-rsa-cert-v01|ssh-dss-cert-v01|ssh-ed25519-cert-v01|"
    r"ecdsa-sha2-nistp256-cert-v01|ecdsa-sha2-nistp384-cert-v01|ecdsa-sha2-nistp521-cert-v01|"
    r"sk-ssh-ed25519|sk-ssh-ed25519-cert-v01|sk-ecdsa-sha2-nistp256|sk-ecdsa-sha2-nistp256-cert-v01|"
    r"kex-strict-c-v00|kex-strict-s-v00|ping|auth-agent-req|auth-agent|"
    r"direct-streamlocal|forwarded-streamlocal|streamlocal-forward|cancel-streamlocal-forward|keepalive|"
    r"statvfs|fstatvfs|fsync|hardlink|posix-rename)@openssh[.]com|"
    r"curve25519-sha256@libssh[.]org)(?![A-Za-z0-9._%+-])"
)
# Fixed wire names from swift-nio-ssh. A whole field may be one name or a complete known name-list, never a URL.
SSH_ALGORITHM_NAMES = frozenset(name + "@" + domain for name, domain in (
    ("aes128-gcm", "openssh.com"), ("aes256-gcm", "openssh.com"), ("curve25519-sha256", "libssh.org"),
    ("ecdsa-sha2-nistp256-cert-v01", "openssh.com"), ("ecdsa-sha2-nistp384-cert-v01", "openssh.com"),
    ("ecdsa-sha2-nistp521-cert-v01", "openssh.com"), ("ssh-ed25519-cert-v01", "openssh.com"),
))
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


def generic_text(text, dependencies, original_text=None):
    """Classify public constants only for generic IP/email detection, never private-word matching."""
    if original_text is None or text == original_text:
        if text in SSH_ALGORITHM_NAMES or ("," in text and all(name in SSH_ALGORITHM_NAMES for name in text.split(","))):
            return ""
    if "golang.org/x/crypto" in dependencies:
        text = GO_SSH_IDENTIFIERS.sub("", text)
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


def text_kinds(text, words, whole_short=False, dependencies=frozenset(), original_text=None):
    kinds = set()
    for word in words:
        if whole_short and len(word) <= 4:
            if re.search(r"(?<![A-Za-z])(%s|%s)(?![A-Za-z])" % (re.escape(word), re.escape(word.lower())), text):
                kinds.add("private value (%d chars)" % len(word))
        elif word in text or word.lower() in text:
            kinds.add("private value (%d chars)" % len(word))
    for word in pc.problems(generic_text(text, dependencies, original_text), []):
        kinds.add("private IP" if pc.IPV4.fullmatch(word) else "email")
    return kinds


THIN_HEADERS = {b"\xce\xfa\xed\xfe": ("<", 28), b"\xfe\xed\xfa\xce": (">", 28),
                b"\xcf\xfa\xed\xfe": ("<", 32), b"\xfe\xed\xfa\xcf": (">", 32)}
FAT_HEADERS = {b"\xca\xfe\xba\xbe": (">", False), b"\xbe\xba\xfe\xca": ("<", False),
               b"\xca\xfe\xba\xbf": (">", True), b"\xbf\xba\xfe\xca": ("<", True)}


def signature_blob_length(data, start, size):
    """Accept only a bounded embedded-signature SuperBlob; its padding is still scanned."""
    if size < 12:
        return None
    magic, length, count = struct.unpack_from(">III", data, start)
    if magic != 0xFADE0CC0 or not 12 <= length <= size or not 0 < count <= (length - 12) // 8:
        return None
    table_end = 12 + count * 8
    blobs = []
    for i in range(count):
        _, offset = struct.unpack_from(">II", data, start + 12 + i * 8)
        if offset < table_end or offset + 8 > length:
            return None
        blob_magic, blob_size = struct.unpack_from(">II", data, start + offset)
        if blob_magic not in (0xFADE0C01, 0xFADE0C02, 0xFADE0B01, 0xFADE7171, 0xFADE7172):
            return None
        if blob_size < 8 or offset + blob_size > length:
            return None
        blobs.append((offset, offset + blob_size))
    blobs.sort()
    if any(a[1] > b[0] for a, b in zip(blobs, blobs[1:])):
        return None
    return length


def thin_signature_ranges(data, base, size):
    """Read slice-relative load commands. None means malformed: do not exclude any bytes."""
    spec = THIN_HEADERS.get(data[base:base + 4])
    if spec is None:
        return None
    endian, header_size = spec
    if size < header_size:
        return None
    commands, command_bytes = struct.unpack_from(endian + "II", data, base + 16)
    commands_end = base + header_size + command_bytes
    if commands > command_bytes // 8 or commands_end > base + size:
        return None
    cursor = base + header_size
    ranges = []
    for _ in range(commands):
        if cursor + 8 > commands_end:
            return None
        command, command_size = struct.unpack_from(endian + "II", data, cursor)
        if command_size < 8 or command_size % 4 or cursor + command_size > commands_end:
            return None
        if command == 0x1D:  # LC_CODE_SIGNATURE
            if command_size != 16 or ranges:
                return None
            offset, signature_size = struct.unpack_from(endian + "II", data, cursor + 8)
            start = base + offset
            if start < commands_end or signature_size > base + size - start:
                return None
            length = signature_blob_length(data, start, signature_size)
            if length is None:
                return None
            ranges.append((start, start + length))
        cursor += command_size
    return ranges if cursor == commands_end else None


def signature_ranges(data):
    """Validated signature ranges of every thin / universal 32- or 64-bit Mach-O slice."""
    if data[:4] in THIN_HEADERS:
        return thin_signature_ranges(data, 0, len(data)) or []
    spec = FAT_HEADERS.get(data[:4])
    if spec is None or len(data) < 8:
        return []
    endian, wide = spec
    count = struct.unpack_from(endian + "I", data, 4)[0]
    arch_size = 32 if wide else 20
    if not 0 < count <= (len(data) - 8) // arch_size:
        return []
    table_end = 8 + count * arch_size
    slices = []
    for i in range(count):
        entry = 8 + i * arch_size
        offset, size = struct.unpack_from(endian + ("QQ" if wide else "II"), data, entry + 8)
        if offset < table_end or size < 28 or offset + size > len(data):
            return []
        slices.append((offset, offset + size))
    slices.sort()
    if any(a[1] > b[0] for a, b in zip(slices, slices[1:])):
        return []
    ranges = []
    for start, end in slices:
        found = thin_signature_ranges(data, start, end - start)
        if found is None:
            return []
        ranges.extend(found)
    return ranges


def texts(path: str):
    with open(path, "rb") as source:
        data = source.read()
    if data[:3] == b"bvx":  # lzfse (App Intents metadata): compressed, values would be split across matches
        data = subprocess.run(["compression_tool", "-decode", "-a", "lzfse", "-i", path, "-o", "/dev/stdout"],
                              capture_output=True).stdout
    # Do not truncate at the first slice's signature: later slices and trailing bytes must remain visible.
    cursor = 0
    for start, end in signature_ranges(data) + [(len(data), len(data))]:
        yield from (m.group(0).decode("ascii") for m in PRINTABLE.finditer(data[cursor:start]))
        cursor = end
    if data[:6] == b"bplist":
        yield subprocess.run(["plutil", "-convert", "xml1", "-o", "-", path], capture_output=True, text=True).stdout


def remove_public_relay_url(text, configured_url, path="/relay"):
    """The app intentionally ships this exact public HTTPS endpoint, not its entire host or other URLs. path: the one
    path the endpoint may have (/relay for the push relay, /dl for the hub's update URL in a published jarvisd)."""
    if not isinstance(configured_url, str):
        return text
    url = configured_url.rstrip("/")
    try:
        parsed = urlsplit(url)
        if (parsed.scheme != "https" or not parsed.hostname or "." not in parsed.hostname
                or parsed.username or parsed.password or parsed.query or parsed.fragment
                or parsed.port is not None or parsed.path != path):
            return text
        try:
            ipaddress.ip_address(parsed.hostname)
            return text
        except ValueError:
            pass
    except ValueError:
        return text
    # Do not remove a prefix of another URL, a credential URL, or an extended path/query.
    boundary = r"[A-Za-z0-9._~:/?#@!$&()*+,;=%-]"
    return re.sub(r"(?<!" + boundary + ")" + re.escape(url) + r"(?!" + boundary + ")", "", text)


def public_download_url(cfg):
    site = jarvis_config.get(cfg, "publish.site_url", "")
    return site.rstrip("/") + "/dl" if isinstance(site, str) and site else ""


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
                original_text = t
                if not whole_short:
                    t = remove_public_relay_url(t, jarvis_config.get(cfg, "app.push_relay", ""))
                else:  # the update URL every published jarvisd carries (-X main.updateURL, scripts/release/publish.sh)
                    t = remove_public_relay_url(t, public_download_url(cfg), "/dl")
                for v in inherent:
                    t = t.replace(v, "")
                if modules:
                    t = modules.sub("", t)
                kinds.update(text_kinds(t, words, whole_short, dependencies, original_text))
            bad += [(os.path.relpath(path, root), k) for k in sorted(kinds)]
    for f, k in bad:
        print("%s: %s" % (f, k))
    if bad:
        sys.exit("check_bundle_privacy: private values in %s (scripts/release/check_bundle_privacy.py)" % root)
    print("check_bundle_privacy: clean (%d values checked)" % len(words))


if __name__ == "__main__":
    main()
