#!/usr/bin/env python3
"""Localization checks for the Apple apps (docs/ios.md「界面语言」, docs/i18n.md).

  apple_l10n.py check                 every string in the catalogs has en and ja, with the same placeholders, and
                                      every Chinese literal in the app's sources is in a catalog or marked
                                      `// i18n-ignore` (fast, no build; run by scripts/test/test_apple_l10n.py)
  apple_l10n.py strict <DerivedData>  after a build with SWIFT_EMIT_LOC_STRINGS=YES: every Chinese literal the
                                      compiler did not extract as localizable is marked `// i18n-ignore`
  apple_l10n.py sync <DerivedData>    adds the strings the compiler extracted to the catalogs (xcstringstool sync),
                                      the step Xcode's IDE does on build and xcodebuild does not

Markers: `// i18n-ignore` at the end of the literal's line or alone on the line above it; `// i18n-ignore-file:
<reason>` in a file's first 20 lines. For text that is not UI: prompts and messages for the hub / agents, protocol
values, matching hub or terminal text, logs, previews, DEBUG-only pages.
"""

import glob
import json
import os
import re
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import swift_literals  # noqa: E402

ROOT = os.path.normpath(os.path.join(HERE, "..", ".."))
APPLE = os.path.join(ROOT, "apple")
LANGS = ["en", "ja"]
SOURCE = "zh-Hans"
# The app targets' sources (not the tests, not the separate SwiftPM packages / test apps).
SOURCE_DIRS = ["Persona", "Shared", "Mac", "Vision", "PersonaWatch", "PersonaWidgets", "LiveActivity", "PersonaNotify"]
CATALOGS = [
    "Shared/Localization/Localizable.xcstrings",
    "Persona/Intents/AppShortcuts.xcstrings",
    "Persona/Resources/InfoPlist.xcstrings",
    "Mac/Resources/InfoPlist.xcstrings",
    "Vision/Resources/InfoPlist.xcstrings",
    "PersonaWatch/InfoPlist.xcstrings",
]
# The targets whose extracted strings go into the catalogs (sync / strict).
TARGETS = ["Persona", "PersonaMac", "PersonaVision", "PersonaWatch", "PersonaWidgets", "PersonaNotify"]

SPEC = re.compile(r"%(?:(\d+)\$)?(#@\w+@|[-+ 0#]*\d*(?:\.\d+)?(?:ll|l|h|hh|z|t|j|q|L)?[@dDiuUxXoOfeEgGcCsSpaA])")
CJK = swift_literals.CJK


def specs(text):
    """The format arguments a value uses: {argument number: specifier}, `%%` skipped."""
    text = text.replace("%%", "")
    out, n = {}, 0
    for m in SPEC.finditer(text):
        if m.group(2).startswith("#@"):
            continue
        n += 1
        idx = int(m.group(1)) if m.group(1) else n
        spec = m.group(2)
        out[idx] = "lld" if spec in ("ld", "d", "lld", "li", "i") else ("@" if spec in ("@",) else spec)
    return out


def values(unit_holder):
    """Every string value of one localization: the plain value, plural / device variations, substitutions."""
    out = []
    if "stringUnit" in unit_holder:
        out.append(unit_holder["stringUnit"])
    for kind in unit_holder.get("variations", {}).values():
        for v in kind.values():
            out.extend(values(v))
    for sub in unit_holder.get("substitutions", {}).values():
        out.extend(values(sub))
    return out


def check_catalog(path, problems):
    data = json.load(open(path, encoding="utf-8"))
    rel = os.path.relpath(path, ROOT)
    if data.get("sourceLanguage") != SOURCE:
        problems.append(f"{rel}: sourceLanguage must be {SOURCE}")
    infoplist = path.endswith("InfoPlist.xcstrings")
    for key, entry in data.get("strings", {}).items():
        if entry.get("shouldTranslate") is False:
            continue
        locs = entry.get("localizations", {})
        source_units = values(locs.get(SOURCE, {}))
        source_text = source_units[0]["value"] if source_units else key
        if infoplist:
            # A key without a source value compiles the key name into InfoPlist.strings (App Review rejects it).
            if not source_units or not source_text or source_text == key:
                problems.append(f"{rel}: {key}: needs a {SOURCE} value")
        elif not CJK.search(source_text) and not CJK.search(key):
            continue  # symbols, numbers, names: nothing to translate
        elif not source_units:
            # The bundle's development region is en: a key without its own zh-Hans value would fall back to the
            # English table (and its plural rules) for Chinese users.
            problems.append(f"{rel}: {key!r}: no {SOURCE} value")
        want = specs(source_text)
        for lang in LANGS:
            units = values(locs.get(lang, {}))
            if not units:
                problems.append(f"{rel}: {key!r}: no {lang}")
                continue
            for u in units:
                if u.get("state") not in ("translated", None) or not u.get("value"):
                    problems.append(f"{rel}: {key!r}: {lang} is {u.get('state')}")
            # The placeholders of the plain value / every plural form.
            loc = locs[lang]
            texts = []
            if "stringUnit" in loc:
                texts.append(loc["stringUnit"]["value"])
            for kind in loc.get("variations", {}).values():
                for v in kind.values():
                    texts.extend(u["value"] for u in values(v))
            for t in texts:
                if "substitutions" in loc:
                    continue  # the substitution carries the counted argument
                got = specs(t)
                if sorted(got.values()) == sorted(want.values()):
                    continue
                if not got and _plural_one_without_count(loc, t):
                    continue
                problems.append(f"{rel}: {key!r}: {lang} placeholders {got} ≠ {want}: {t!r}")
    return data


def _plural_one_without_count(loc, text):
    """English "one" may spell the number out ("A file"): allowed only for the plural form `one`."""
    plural = loc.get("variations", {}).get("plural", {})
    return plural.get("one", {}).get("stringUnit", {}).get("value") == text


def normalize(text, literal=False):
    """A literal's body or a catalog key, with every interpolation / placeholder reduced to one marker."""
    if literal and text.startswith("\n"):  # a multi-line literal: Swift drops the first newline and the closing indentation
        lines = text.split("\n")[1:]
        indent = len(lines[-1]) if lines and not lines[-1].strip() else 0
        lines = [l[indent:] for l in lines[:-1]] if indent or (lines and not lines[-1].strip()) else lines
        text = "\n".join(lines).replace("\\\n", "")
    out, i, depth = [], 0, 0
    while i < len(text):
        if depth == 0 and text.startswith("\\(", i):
            depth, i = 1, i + 2
            out.append("\x00")
            continue
        if depth:
            depth += {"(": 1, ")": -1}.get(text[i], 0)
            i += 1
            continue
        out.append(text[i])
        i += 1
    text = "".join(out)
    text = re.sub(r"\$\{\w+\}", "\x00", text)  # App Intents' ${parameter}
    text = SPEC.sub("\x00", text).replace("%%", "%")
    text = text.replace('\\"', '"').replace("\\n", "\n").replace("\\t", "\t").replace("\\\\", "\\")
    return text


def ignored_lines(src):
    lines = src.split("\n")
    if any("i18n-ignore-file" in l for l in lines[:20]):
        return None
    marked = set()
    for i, l in enumerate(lines, 1):
        if "// i18n-ignore" in l:
            stripped = l.strip()
            marked.add(i + 1 if stripped.startswith("// i18n-ignore") else i)
    return marked


def preview_lines(src):
    """Lines inside `#Preview { … }` blocks and `#if DEBUG` … `#endif` regions are not UI that ships."""
    lines, out, stack = src.split("\n"), set(), []
    for i, l in enumerate(lines, 1):
        s = l.strip()
        if s.startswith("#if"):
            stack.append("DEBUG" in s and "!" not in s)
        elif s.startswith("#else") and stack:
            stack[-1] = False
        elif s.startswith("#endif") and stack:
            stack.pop()
        if any(stack):
            out.add(i)
    depth, opened = None, False
    for i, l in enumerate(lines, 1):
        if depth is None and l.lstrip().startswith("#Preview"):
            depth, opened = 0, False
        if depth is not None:
            out.add(i)
            depth += l.count("{") - l.count("}")
            opened = opened or "{" in l
            if opened and depth <= 0:
                depth = None
    return out


def source_files():
    for d in SOURCE_DIRS:
        yield from sorted(glob.glob(os.path.join(APPLE, d, "**", "*.swift"), recursive=True))


def cjk_literals(path):
    src = open(path, encoding="utf-8").read()
    marked = ignored_lines(src)
    if marked is None:
        return []
    skip = preview_lines(src)
    out = []
    lines = src.split("\n")
    for lit in swift_literals.literals(src):
        if not lit.has_cjk or lit.line in marked or lit.line in skip:
            continue
        before = lines[lit.line - 1][: lit.col - 1].rstrip()
        if before.endswith("comment:"):  # a translator's note, not text
            continue
        out.append(lit)
    return out


def check():
    problems = []
    keys = set()
    for c in CATALOGS:
        path = os.path.join(APPLE, c)
        if not os.path.exists(path):
            problems.append(f"apple/{c}: missing")
            continue
        data = check_catalog(path, problems)
        for key in data.get("strings", {}):
            keys.add(normalize(key))
    missing = []
    for path in source_files():
        for lit in cjk_literals(path):
            if normalize(lit.text, literal=True) not in keys:
                missing.append(f"{os.path.relpath(path, ROOT)}:{lit.line}: {lit.text[:70]}")
    if missing:
        problems.append(f"{len(missing)} Chinese literals are in no catalog (localize them, or mark // i18n-ignore):")
        problems.extend("  " + m for m in missing)
    return problems


def stringsdata(derived):
    pattern = os.path.join(derived, "Build", "Intermediates.noindex", "**", "*.stringsdata")
    for f in glob.glob(pattern, recursive=True):
        parts = f.split(os.sep)
        if not any(f"{t}.build" in parts for t in TARGETS):
            continue
        yield f


def strict(derived):
    extracted = {}
    for f in stringsdata(derived):
        d = json.load(open(f, encoding="utf-8"))
        src = os.path.realpath(d.get("source", ""))
        for entries in d.get("tables", {}).values():
            for e in entries:
                loc = e.get("location")
                if loc:
                    extracted.setdefault(src, set()).add(loc["startingLine"])
    problems = []
    for path in source_files():
        lines = extracted.get(os.path.realpath(path), set())
        src_lines = open(path, encoding="utf-8").read().split("\n")
        for lit in cjk_literals(path):
            before = src_lines[lit.line - 1][: lit.col - 1]
            # Already a resource the compiler does not list with a place: App Shortcut phrases and App Intents'
            # parameter summaries (their own tables), and String(localized:) in a `case` pattern.
            if "\\(.applicationName)" in lit.text or "\\(\\.$" in lit.text or before.rstrip().endswith("String(localized:"):
                continue
            if lit.line not in lines:
                problems.append(f"{os.path.relpath(path, ROOT)}:{lit.line}: not localized: {lit.text[:70]}")
    return problems


def sync(derived):
    files = sorted(stringsdata(derived))
    if not files:
        sys.exit("no .stringsdata: build with SWIFT_EMIT_LOC_STRINGS=YES into this DerivedData first")
    catalogs = [os.path.join(APPLE, "Shared/Localization/Localizable.xcstrings"),
                os.path.join(APPLE, "Persona/Intents/AppShortcuts.xcstrings")]
    args = ["xcrun", "xcstringstool", "sync", *catalogs, "--skip-marking-strings-stale"]
    for f in files:
        args += ["--stringsdata", f]
    subprocess.run(args, check=True)
    print(f"synced {len(files)} .stringsdata into the catalogs")


def main():
    if len(sys.argv) < 2 or sys.argv[1] not in ("check", "strict", "sync"):
        sys.exit(__doc__)
    cmd = sys.argv[1]
    if cmd == "sync":
        sync(sys.argv[2])
        return
    problems = check() if cmd == "check" else strict(sys.argv[2])
    if problems:
        print("\n".join(problems))
        print(f"FAIL: {sum(1 for p in problems if not p.startswith('  '))} problems")
        sys.exit(1)
    print("OK: the Apple catalogs are complete in en and ja")


if __name__ == "__main__":
    main()
