#!/usr/bin/env python3
"""Fail when the repository holds this person's private values (CLAUDE.md: no intranet or personal data, ever).

    python3 scripts/privacy_check.py            # tracked files (content and paths)
    python3 scripts/privacy_check.py --staged   # what `git commit` is about to record, plus its message file if given
    python3 scripts/privacy_check.py --staged --message .git/COMMIT_EDITMSG

What counts as private comes from this machine, never from the repo: the values of ~/.config/jarvis/config.json
(hosts, hub address, machine names, Apple ids, emails), ~/.config/jarvis/private-words.txt (one word per line:
internal project names, people, anything else) and CLAUDE.local.md's `code spans`. On top of that, any private IPv4
address and any email outside example.* domains. Install as a hook: scripts/hooks/pre-commit (docs/testing.md).
"""
import ipaddress, json, os, re, subprocess, sys

ROOT = subprocess.run(["git", "rev-parse", "--show-toplevel"], capture_output=True, text=True, check=True).stdout.strip()
HOME = os.path.expanduser("~")
# Values too generic to search for (a machine called "mac", an OS name).
IGNORE = {"darwin", "linux", "local", "ssh", "true", "false", "server", "localhost"}
# Files that may hold placeholders that look like the real thing.
SKIP_FILES = {"scripts/privacy_check.py"}


def config_values():
    out = set()
    try:
        cfg = json.load(open(os.path.join(HOME, ".config/jarvis/config.json")))
    except (OSError, ValueError):
        return out

    def walk(o, key=""):
        if isinstance(o, dict):
            for k, v in o.items():
                walk(v, k)
        elif isinstance(o, list):
            for v in o:
                walk(v, key)
        elif isinstance(o, str) and o:
            if key in ("os", "simulator"):
                return
            if key == "url":
                host = re.sub(r"^\w+://", "", o).split("/")[0].split(":")[0]
                out.add(host)
                return
            if key.endswith("path"):
                o = o.replace(HOME, "~")
            out.add(o)

    walk(cfg)
    return out


def word_list():
    out = set()
    try:
        for line in open(os.path.join(HOME, ".config/jarvis/private-words.txt")):
            line = line.strip()
            if line and not line.startswith("#"):
                out.add(line)
    except OSError:
        pass
    local = os.path.join(ROOT, "CLAUDE.local.md")
    if os.path.exists(local):
        for span in re.findall(r"`([^`\n]{4,})`", open(local).read()):
            if re.search(r"[.@/:]|\d", span):  # hosts, addresses, accounts, paths, ids; not prose
                out.add(span.strip())
    return out


def terms():
    vals = {v for v in config_values() | word_list() if len(v) >= 4 and v.lower() not in IGNORE}
    user = os.path.basename(HOME)
    if len(user) >= 3:
        vals.add("/Users/" + user)
        vals.add("/home/" + user)
    return sorted(vals, key=len, reverse=True)


IPV4 = re.compile(r"(?<![\d.])(\d{1,3}(?:\.\d{1,3}){3})(?![\d.])")
# Documentation ranges (RFC 5737): what redacted fixtures and docs use for addresses.
DOC_NETS = [ipaddress.ip_network(n) for n in ("192.0.2.0/24", "198.51.100.0/24", "203.0.113.0/24")]
EMAIL = re.compile(r"[A-Za-z0-9._%+-]+@([A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,})")
# Placeholders (example domains, public mail providers, noreply) are fine; so are made-up TLDs (the fixture redactor
# scrambles domains): only addresses at a real-looking domain of somebody's are flagged.
OK_EMAIL_DOMAINS = re.compile(r"(^|\.)(example(\.(com|org|net))?|test|invalid|localhost|users\.noreply\.github\.com|anthropic\.com|"
                              r"apple\.com|gmail\.com|googlemail\.com|outlook\.com|hotmail\.com|icloud\.com|yahoo\.com|"
                              r"qq\.com|163\.com|126\.com|foxmail\.com)$")
REAL_TLDS = {"com", "net", "org", "io", "cn", "dev", "app", "ai", "co", "me", "cc", "info", "so", "xyz", "top", "edu",
             "gov", "uk", "jp", "us", "de", "fr", "hk", "tw", "sg", "in", "ru", "kr", "tech", "cloud", "site", "work", "biz"}
# The public site (site/) shows a contact address on purpose: the OAuth consent screen requires one.
PUBLIC_DIRS = ("site/",)


def problems(text, words, emails=True):
    found = []
    low = text.lower()
    for w in words:
        if w.lower() in low:
            found.append(w)
    for m in IPV4.finditer(text):
        try:
            ip = ipaddress.ip_address(m.group(1))
        except ValueError:
            continue
        if ip.is_private and not ip.is_loopback and not ip.is_unspecified and not any(ip in n for n in DOC_NETS):
            found.append(m.group(1))
    for m in EMAIL.finditer(text) if emails else ():
        domain = m.group(1).lower()
        if not OK_EMAIL_DOMAINS.search(domain) and domain.rsplit(".", 1)[-1] in REAL_TLDS:
            found.append(m.group(0))
    return found


def git(*args):
    return subprocess.run(["git", "-C", ROOT, *args], capture_output=True, check=True).stdout


def main():
    staged = "--staged" in sys.argv
    message = sys.argv[sys.argv.index("--message") + 1] if "--message" in sys.argv else None
    words = terms()
    if staged:
        files = git("diff", "--cached", "--name-only", "--diff-filter=ACMR", "-z").decode().split("\0")
    else:
        files = git("ls-files", "-z").decode().split("\0")
    bad = []
    for f in filter(None, files):
        for w in problems(f, words):
            bad.append((f, "path", w))
        if f in SKIP_FILES:
            continue
        try:
            data = git("show", ":" + f) if staged else open(os.path.join(ROOT, f), "rb").read()
        except (OSError, subprocess.CalledProcessError):
            continue
        if b"\0" in data[:4096]:
            continue  # binary
        text = data.decode("utf-8", "replace")
        public = f.startswith(PUBLIC_DIRS)
        for n, line in enumerate(text.splitlines(), 1):
            for w in problems(line, words, emails=not public):
                bad.append((f, n, w))
    if message and os.path.exists(message):
        for n, line in enumerate(open(message).read().splitlines(), 1):
            if not line.startswith("#"):
                for w in problems(line, words):
                    bad.append(("commit message", n, w))
    for f, where, w in bad:
        # The value itself would end up in logs and transcripts: say which kind, not what.
        kind = "private IP" if IPV4.fullmatch(w) else "email" if "@" in w else "private value (%d chars)" % len(w)
        print("%s:%s: %s" % (f, where, kind))
    if bad:
        print("privacy_check: %d hit(s). Use placeholders (<hub 地址>, me@example.com); see CLAUDE.md." % len(bad), file=sys.stderr)
        sys.exit(1)
    print("privacy_check: clean (%d values checked)" % len(words))


if __name__ == "__main__":
    main()
