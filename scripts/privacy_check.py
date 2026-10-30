#!/usr/bin/env python3
"""Fail when the repository holds this person's private values (CLAUDE.md: no intranet or personal data, ever).

    python3 scripts/privacy_check.py            # tracked files (content and paths)
    python3 scripts/privacy_check.py --staged   # what `git commit` is about to record, plus its message file if given
    python3 scripts/privacy_check.py --staged --message .git/COMMIT_EDITMSG

What counts as private comes from this machine, never from the repo: the values of ~/.config/jarvis/config.json
(hosts, hub address, machine names, Apple ids, emails), ~/.config/jarvis/private-words.txt (one word per line:
internal project names, people, anything else) and CLAUDE.local.md's `code spans`. On top of that, any private IPv4
address and any email outside example.* domains. Install as a hook: scripts/hooks/pre-commit (docs/testing.md).

Not private: values config.example.json ships as defaults, the repo's own Go module path (it names the GitHub owner),
CLAUDE.local.md spans that only name repo files, commands or config keys, and in site/ the contact address and domain
the App Store and the OAuth consent screen publish anyway.
"""
import ipaddress, json, os, re, subprocess, sys

ROOT = subprocess.run(["git", "rev-parse", "--show-toplevel"], capture_output=True, text=True, check=True).stdout.strip()
HOME = os.path.expanduser("~")
# Values too generic to search for (a machine called "mac", an OS name).
IGNORE = {"darwin", "linux", "local", "ssh", "true", "false", "server", "localhost"}
# Files that may hold placeholders that look like the real thing.
SKIP_FILES = {"scripts/privacy_check.py"}
# Config keys whose values are published on purpose (App Store contact, the site's address): fine in PUBLIC_DIRS.
PUBLIC_KEYS = {"feedback_email", "privacy_url"}


def strings(o, key=""):
    """(key, value) for every string in a JSON value; a list item takes its list's key."""
    if isinstance(o, dict):
        for k, v in o.items():
            yield from strings(v, k)
    elif isinstance(o, list):
        for v in o:
            yield from strings(v, key)
    elif isinstance(o, str) and o:
        yield key, o


def example_values():
    try:
        return {v for _, v in strings(json.load(open(os.path.join(ROOT, "config.example.json"))))}
    except (OSError, ValueError):
        return set()


def config_values():
    """The config's values, and the subset that is published on purpose (PUBLIC_KEYS)."""
    out, public = set(), set()
    try:
        cfg = json.load(open(os.path.join(HOME, ".config/jarvis/config.json")))
    except (OSError, ValueError):
        return out, public
    defaults = example_values()
    for key, o in strings(cfg):
        if key in ("os", "simulator") or o in defaults:
            continue
        if key == "url" or key.endswith("_url"):
            o = re.sub(r"^\w+://", "", o).split("/")[0].split(":")[0]
        elif key.endswith("path"):
            o = o.replace(HOME, "~")
        out.add(o)
        if key in PUBLIC_KEYS:
            public.add(o)
    return out, public


# A token that looks like somebody's value: a long id mixing letters and digits (key ids, team ids, hostnames like
# n12-345), a run of six or more digits (app ids), or a host under a real TLD. Repo paths, commands and config keys
# (`docs/config.md`, `python3 scripts/x.py`, `google.proxy`) have none.
ID_LIKE = re.compile(r"(?=[\w-]*\d)(?=[\w-]*[A-Za-z])[\w-]{8,}|\d{6,}")


def private_looking(span):
    if ID_LIKE.search(span) or "@" in span or IPV4.search(span):
        return True
    for host in re.findall(r"[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)+", span):
        if host.rsplit(".", 1)[-1].lower() in REAL_TLDS and not host.lower().endswith((".md", ".py", ".sh", ".json")):
            return True
    return False


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
            if private_looking(span):  # hosts, addresses, accounts, ids; not prose, repo paths or commands
                out.add(span.strip())
    return out


def terms():
    """Words to look for, longest first, and those of them allowed in PUBLIC_DIRS."""
    config, public = config_values()
    vals = {v for v in config | word_list() if len(v) >= 4 and v.lower() not in IGNORE}
    user = os.path.basename(HOME)
    if len(user) >= 3:
        vals.add("/Users/" + user)
        vals.add("/home/" + user)
    return sorted(vals, key=len, reverse=True), public


def module_paths():
    """The repo's own Go module paths (github.com/<owner>/...): every import carries them."""
    out = []
    for f in git("ls-files", "-z", "--", "*go.mod").decode().split("\0"):
        if f:
            m = re.search(r"^module\s+(\S+)", open(os.path.join(ROOT, f)).read(), re.M)
            if m:
                out.append(m.group(1).split("/")[0:3])
    return re.compile("|".join(re.escape("/".join(p)) for p in out), re.I) if out else None


IPV4 = re.compile(r"(?<![\d.])(\d{1,3}(?:\.\d{1,3}){3})(?![\d.])")
# Documentation ranges (RFC 5737): what redacted fixtures and docs use for addresses.
DOC_NETS = [ipaddress.ip_network(n) for n in ("192.0.2.0/24", "198.51.100.0/24", "203.0.113.0/24")]
EMAIL = re.compile(r"[A-Za-z0-9._%+-]+@([A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,})")
# Placeholders (example domains, public mail providers, noreply) are fine; so are made-up TLDs (the fixture redactor
# scrambles domains): only addresses at a real-looking domain of somebody's are flagged.
OK_EMAIL_DOMAINS = re.compile(r"(^|\.)(example(\.(com|org|net))?|test|invalid|localhost|users\.noreply\.github\.com|anthropic\.com|"
                              r"apple\.com|gmail\.com|googlemail\.com|outlook\.com|hotmail\.com|icloud\.com|yahoo\.com|"
                              r"qq\.com|163\.com|126\.com|foxmail\.com|"
                              r"b\.com)$")  # a@b.com: the placeholder in lark-connector's skill (assistantskills/)
REAL_TLDS = {"com", "net", "org", "io", "cn", "dev", "app", "ai", "co", "me", "cc", "info", "so", "xyz", "top", "edu",
             "gov", "uk", "jp", "us", "de", "fr", "hk", "tw", "sg", "in", "ru", "kr", "tech", "cloud", "site", "work", "biz"}
# The public site (site/) shows a contact address on purpose: the OAuth consent screen requires one.
PUBLIC_DIRS = ("site/",)


def problems(text, words, emails=True):
    found = []
    low = text.lower()
    for w in words:
        # Short values (a name of 4–5 letters) only as a word of their own: inside identifiers ("inboxItem") they
        # matched by accident and flagged whole files.
        if len(w) < 6 and w.isalnum():
            if re.search(r"(?<![a-z0-9])" + re.escape(w.lower()) + r"(?![a-z0-9])", low):
                found.append(w)
        elif w.lower() in low:
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
    words, public_words = terms()
    modules = module_paths()
    if staged:
        files = git("diff", "--cached", "--name-only", "--diff-filter=ACMR", "-z").decode().split("\0")
    else:
        files = git("ls-files", "-z").decode().split("\0")
    bad = []
    for f in filter(None, files):
        public = f.startswith(PUBLIC_DIRS)
        allowed = [w for w in words if w not in public_words] if public else words
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
        if modules:
            text = modules.sub("", text)
        for n, line in enumerate(text.splitlines(), 1):
            for w in problems(line, allowed, emails=not public):
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
