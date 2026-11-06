#!/usr/bin/env python3
"""Check the Android app's translations (docs/i18n.md).

- Every language in android/i18n/*.properties has complete resources in each module, matching keys and placeholders,
  and plurals
  with the quantities their language needs; values/ is English, so its translatable text has no Chinese or Japanese.
- String keys start with the module's name (`settings_…`), since all modules' resources merge into one table.
- Kotlin code and XML outside the translated values directories (and outside tests and the design system's debug catalog)
  only lose hardcoded Chinese / Japanese text: each file may hold
  no more than its module's i18n-baseline.json records (one file per module, so modules move in parallel). A literal that is not UI text (a hub token it matches, a prompt
  sent to the model) says why on its line with `// i18n-ignore: <reason>`.

  python3 scripts/app/android_i18n_check.py            # check
  python3 scripts/app/android_i18n_check.py --update   # record the lower counts (even while another file is over)
"""
import argparse
import json
import re
import sys
import xml.etree.ElementTree as ET
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
ANDROID = ROOT / 'android'
BASELINE = 'i18n-baseline.json'
# Chinese, Japanese kana, and full-width punctuation.
CJK = re.compile(r'[　-〿぀-ヿ㐀-䶿一-鿿＀-￯]')
PLACEHOLDER = re.compile(r'%(?:\d+\$)?[-#+ 0,(]*\d*(?:\.\d+)?[sdfxXc%]')
IGNORE = 'i18n-ignore'
SKIP_DIRS = {'build', '.gradle', 'generated'}


def languages(root=ANDROID):
    """Resource directory -> metadata, discovered from i18n/<BCP47 tag>.properties (also read by Gradle)."""
    out = {}
    for file in sorted((root / 'i18n').glob('*.properties')):
        meta = dict(line.split('=', 1) for line in file.read_text(encoding='utf-8').splitlines()
                    if line.strip() and not line.startswith('#'))
        meta['tag'] = file.stem
        meta['matches'] = meta.get('matches', '').split(',')
        meta['plurals'] = set(meta.get('plurals', '').split(','))
        out[meta.get('resources', '')] = meta
    return out


def language_problems(root=ANDROID):
    """Every language has usable metadata; no subtag or resources directory is claimed twice."""
    problems, claimed, resources = [], set(), set()
    files = sorted((root / 'i18n').glob('*.properties'))
    if not {'en', 'zh-Hans', 'ja'} <= {p.stem for p in files}:
        problems.append('i18n/: source, fallback, and Japanese language resources are required')
    for file in files:
        try:
            meta = dict(line.split('=', 1) for line in file.read_text(encoding='utf-8').splitlines()
                        if line.strip() and not line.startswith('#'))
            tag = file.stem
            if not re.fullmatch(r'[a-z]{2,3}(?:-[A-Za-z0-9]{2,8})*', tag):
                problems.append(f'i18n/{file.name}: invalid BCP 47 tag')
            if not meta.get('name', '').strip():
                problems.append(f'i18n/{file.name}: name is empty')
            directory = meta.get('resources', '')
            if directory in resources or not re.fullmatch(r'values(?:-[a-z]{2,3}|-b\+[A-Za-z0-9+]+)?', directory):
                problems.append(f'i18n/{file.name}: duplicate or invalid resources directory {directory}')
            resources.add(directory)
            if (tag == 'en') != (directory == 'values'):
                problems.append(f'i18n/{file.name}: only en uses the values/ fallback')
            matches = meta.get('matches', '').split(',')
            if tag.split('-')[0] not in matches:
                problems.append(f'i18n/{file.name}: matches must include its own language subtag')
            for subtag in matches:
                if not re.fullmatch(r'[a-z]{2,3}', subtag) or subtag in claimed:
                    problems.append(f'i18n/{file.name}: duplicate or invalid match {subtag}')
                claimed.add(subtag)
            plural = set(meta.get('plurals', '').split(','))
            if not 'other' in plural or not plural <= {'zero', 'one', 'two', 'few', 'many', 'other'}:
                problems.append(f'i18n/{file.name}: invalid CLDR plural quantities')
        except ValueError:
            problems.append(f'i18n/{file.name}: expected one key=value per line')
    return problems


def modules(root=ANDROID):
    """Each Gradle module: the directory holding build.gradle.kts and a src/."""
    for build in sorted(root.glob('**/build.gradle.kts')):
        directory = build.parent
        if directory == root or 'build-logic' in directory.parts or not (directory / 'src').is_dir():
            continue
        if any(part in SKIP_DIRS for part in directory.relative_to(root).parts):
            continue
        yield directory


def prefix(module, root=ANDROID):
    return module.name.replace('-', '_') + '_'


def strings(directory):
    """name -> (kind, text or {quantity: text}, translatable) for every strings*.xml in a values directory."""
    found = {}
    for path in sorted(directory.glob('strings*.xml')):
        for element in ET.parse(path).getroot():
            name = element.get('name')
            if name is None:
                continue
            translatable = element.get('translatable') != 'false'
            if element.tag == 'string':
                found[name] = ('string', ''.join(element.itertext()), translatable)
            elif element.tag == 'plurals':
                found[name] = ('plurals', {item.get('quantity'): ''.join(item.itertext()) for item in element}, translatable)
            elif element.tag == 'string-array':
                found[name] = ('array', [''.join(item.itertext()) for item in element], translatable)
    return found


def placeholders(value):
    texts = value.values() if isinstance(value, dict) else value if isinstance(value, list) else [value]
    return sorted({p for text in texts for p in PLACEHOLDER.findall(text) if p != '%%'})


def resource_problems(module, root=ANDROID):
    problems = []
    res = module / 'src/main/res'
    if not res.is_dir():
        return problems
    name = module.relative_to(root).as_posix()
    metadata = languages(root)
    locales = ['values'] + sorted(d for d in metadata if d != 'values')
    plural = {d: m['plurals'] for d, m in metadata.items()}
    sets = {locale: strings(res / locale) for locale in locales}
    if not any(sets.values()):
        return problems
    for directory in res.glob('values-*'):
        if directory.name not in metadata and list(directory.glob('strings*.xml')):
            problems.append(f'{name}: {directory.name}/ has strings but no i18n language metadata')
    base = sets['values']
    for key, (kind, value, translatable) in base.items():
        if not key.startswith(prefix(module)):
            problems.append(f'{name}: string "{key}" should start with "{prefix(module)}"')
        if translatable and any(CJK.search(text) for text in (value.values() if isinstance(value, dict) else [value] if isinstance(value, str) else value)):
            problems.append(f'{name}: values/ is the English fallback, but "{key}" is not English')
        if kind == 'plurals' and not plural['values'] <= set(value):
            problems.append(f'{name}: plurals "{key}" in values/ needs {sorted(plural["values"])}')
    wanted = {key for key, (_, _, translatable) in base.items() if translatable}
    for locale in locales[1:]:
        have = sets[locale]
        for key in sorted(wanted - have.keys()):
            problems.append(f'{name}: "{key}" is missing from {locale}/')
        for key in sorted(have.keys() - wanted):
            why = 'is not translatable' if key in base else 'is not in values/'
            problems.append(f'{name}: "{key}" in {locale}/ {why}')
        for key in sorted(wanted & have.keys()):
            kind, value, _ = base[key]
            other_kind, other, _ = have[key]
            if kind != other_kind:
                problems.append(f'{name}: "{key}" is a {kind} in values/ but a {other_kind} in {locale}/')
                continue
            if placeholders(value) != placeholders(other):
                problems.append(f'{name}: "{key}" has placeholders {placeholders(value)} in values/ but {placeholders(other)} in {locale}/')
            if kind == 'plurals' and not plural[locale] <= set(other):
                problems.append(f'{name}: plurals "{key}" in {locale}/ needs {sorted(plural[locale])}')
    return problems


def literals(source):
    """The string literals of Kotlin [source] that are code (not comments), with their line numbers."""
    found = []
    i, line = 0, 1
    n = len(source)
    while i < n:
        c = source[i]
        if c == '\n':
            line += 1
            i += 1
        elif source.startswith('//', i):
            end = source.find('\n', i)
            i = n if end < 0 else end
        elif source.startswith('/*', i):
            depth, i = 1, i + 2
            while i < n and depth:
                if source.startswith('/*', i):
                    depth, i = depth + 1, i + 2
                elif source.startswith('*/', i):
                    depth, i = depth - 1, i + 2
                else:
                    line += source[i] == '\n'
                    i += 1
        elif source.startswith('"""', i):
            end = source.find('"""', i + 3)
            end = n if end < 0 else end + 3
            found.append((line, source[i:end]))
            line += source.count('\n', i, end)
            i = end
        elif c == '"':
            start, i = i, i + 1
            while i < n and source[i] not in '"\n':
                i += 2 if source[i] == '\\' else 1
            found.append((line, source[start:i + 1]))
            i += 1
        elif c == "'":
            # A character literal (a single CJK character in quotes), or an apostrophe of nothing in particular: skip a short one.
            end = source.find("'", i + 1)
            i = end + 1 if 0 < end - i <= 8 else i + 1
        else:
            i += 1
    return found


def kotlin_count(path):
    text = path.read_text(encoding='utf-8')
    lines = text.split('\n')
    hits = set()
    for line, literal in literals(text):
        if CJK.search(literal) and IGNORE not in lines[line - 1]:
            hits.add(line)
    return len(hits)


def xml_count(path):
    count = 0
    for line in path.read_text(encoding='utf-8').split('\n'):
        stripped = re.sub(r'<!--.*?-->', '', line)
        if CJK.search(stripped) and IGNORE not in line and 'translatable="false"' not in line:
            count += 1
    return count


def source_files(root=ANDROID):
    for module in modules(root):
        src = module / 'src'
        for path in sorted(src.rglob('*')):
            if not path.is_file():
                continue
            parts = path.relative_to(src).parts
            if parts[0] in ('test', 'androidTest') or parts[0].startswith('test'):
                continue
            # Only the design system's developer catalog is exempt; ordinary debug client code is still checked.
            catalog = ('core', 'designsystem', 'src', 'debug', 'kotlin', 'persona', 'core', 'designsystem', 'catalog')
            if path.relative_to(root).parts[:len(catalog)] == catalog:
                continue
            if path.suffix == '.kt':
                yield path, kotlin_count
            elif path.suffix == '.xml' and not any(p in (set(languages(root)) - {'values'}) for p in parts):
                yield path, xml_count


def count(root=ANDROID):
    counts = {}
    for path, counter in source_files(root):
        n = counter(path)
        if n:
            counts[path.relative_to(root).as_posix()] = n
    return counts


def regressions(counts, baseline):
    return [f'{path}: {n} lines with hardcoded Chinese / Japanese text (baseline {baseline.get(path, 0)}); move them to strings.xml'
            for path, n in sorted(counts.items()) if n > baseline.get(path, 0)]


def baselines(root=ANDROID):
    """path -> recorded count, from every module's baseline file."""
    recorded = {}
    for module in modules(root):
        file = module / BASELINE
        if file.exists():
            recorded.update(json.loads(file.read_text()))
    return recorded


def write_baselines(counts, root=ANDROID):
    for module in modules(root):
        name = module.relative_to(root).as_posix() + '/'
        mine = {path: n for path, n in counts.items() if path.startswith(name)}
        file = module / BASELINE
        if mine:
            file.write_text(json.dumps(mine, indent=2, ensure_ascii=False, sort_keys=True) + '\n')
        elif file.exists():
            file.unlink()


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--update', action='store_true', help='record the current counts; they may only go down')
    parser.add_argument('--init', action='store_true', help=argparse.SUPPRESS)
    args = parser.parse_args()
    problems = language_problems()
    if problems:
        for problem in problems:
            print(problem, file=sys.stderr)
        return 1
    problems = [p for module in modules() for p in resource_problems(module)]
    counts = count()
    baseline = counts if args.init else baselines()
    problems += regressions(counts, baseline)
    if args.init and not problems:
        write_baselines(counts)
    elif args.update:
        # Record what went down even while another module is over its baseline (that one stays reported): agents
        # migrating different modules at once must not block each other.
        write_baselines({path: min(n, baseline[path]) for path, n in counts.items() if path in baseline})
    for problem in problems:
        print(problem, file=sys.stderr)
    total, before = sum(counts.values()), sum(baseline.values())
    print(f'android i18n: {len(problems)} problem(s); {total} hardcoded lines in {len(counts)} files (baseline {before})')
    return 1 if problems else 0


if __name__ == '__main__':
    sys.exit(main())
