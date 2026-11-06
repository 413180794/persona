#!/usr/bin/env python3
"""Count Kotlin feature token adoption and reject hardcoded values above a reviewed baseline."""
import argparse
import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
BASELINE = ROOT / 'android/core/designsystem/tools/token-baseline.json'
CATEGORIES = ('color', 'font', 'dp')
NUMBER = r'(?:0[xX][0-9a-fA-F_]+|\d[\d_]*(?:\.\d[\d_]*)?(?:[eE][+-]?\d+)?)[fFlLuU]*'
PATTERNS = {
    'color': rf'\bColor\s*\(\s*(?:(?:color|red|green|blue|alpha)\s*=\s*)?[+-]?{NUMBER}|\bColor\s*\.\s*(?:Black|White|Red|Green|Blue|Yellow|Cyan|Magenta|Gray|LightGray|DarkGray|Transparent)\b|\b(?:Color\s*\.\s*)?parseColor\s*\(',
    'font': rf'(?<![\w.]){NUMBER}\s*\.\s*sp\b',
    'dp': rf'(?<![\w.]){NUMBER}\s*\.\s*dp\b',
}
TOKEN_PATTERNS = {
    'color': r'\b(?:Tokens\.(?:Common|Status|System|Semantic)|(?:PersonaTheme|PersonaTheme)\.(?:colors|palette)|MaterialTheme\.colorScheme)\.[\w.]+',
    'font': r'\b(?:Tokens\.Type|(?:PersonaTypography|PersonaTypography)|(?:PersonaTheme|PersonaTheme)\.type|MaterialTheme\.typography)\.[\w.]+',
    'dp': r'\b(?:Tokens\.(?:Space|Radius|MinTouch|Shape|Surface)|(?:PersonaTheme|PersonaTheme)\.shapes|MaterialTheme\.shapes)\.[\w.]+',
}


def code_only(source):
    # Keep offsets/newlines stable; Kotlin supports nested block comments.
    out = list(source)
    i = 0
    while i < len(source):
        start = i
        if source.startswith('//', i):
            end = source.find('\n', i)
            i = len(source) if end < 0 else end
        elif source.startswith('/*', i):
            i += 2
            depth = 1
            while i < len(source) and depth:
                if source.startswith('/*', i):
                    depth += 1
                    i += 2
                elif source.startswith('*/', i):
                    depth -= 1
                    i += 2
                else:
                    i += 1
        elif source.startswith('"""', i):
            end = source.find('"""', i + 3)
            i = len(source) if end < 0 else end + 3
        elif source[i] in '\"\'':
            quote = source[i]
            i += 1
            while i < len(source):
                if source[i] == '\\':
                    i += 2
                elif source[i] == quote:
                    i += 1
                    break
                else:
                    i += 1
        else:
            i += 1
            continue
        for pos in range(start, min(i, len(source))):
            if out[pos] != '\n':
                out[pos] = ' '
    return ''.join(out)


def count(source):
    code = code_only(source)
    imports = re.findall(r'^\s*import\s+([\w.]+)(?:\s+as\s+(\w+))?\s*$', code, re.M)
    code = re.sub(r'^\s*(?:import|package)\s+[^\n]+', '', code, flags=re.M)
    # Expand explicit member imports and aliases before applying the same rules.
    for path, alias in imports:
        name = alias or path.rsplit('.', 1)[-1]
        if 'designsystem.' in path:
            replacement = path.split('designsystem.', 1)[1]
        elif '.Color' in path:
            replacement = 'Color' + path.split('.Color', 1)[1]
        elif alias and path.endswith(('.dp', '.sp')):
            replacement = path.rsplit('.', 1)[-1]
        else:
            continue
        if name != replacement:
            code = re.sub(rf'(?<![\w.]){re.escape(name)}\b', replacement, code)
    code = re.sub(r'\s*\.\s*', '.', code)
    return {category: {
        'hardcoded': len(re.findall(PATTERNS[category], code)),
        'token': len(re.findall(TOKEN_PATTERNS[category], code)),
    } for category in CATEGORIES}


def scan(root):
    files = {}
    modules = {}
    # Deliberately restrict discovery to feature production source sets.
    for module in sorted((root / 'android/feature').iterdir()):
        if not module.is_dir() or not (module / 'src').is_dir():
            continue
        totals = {category: {'hardcoded': 0, 'token': 0} for category in CATEGORIES}
        for source_set in sorted((module / 'src').iterdir()):
            if 'test' in source_set.name.lower() or not source_set.is_dir():
                continue
            for path in sorted(source_set.rglob('*.kt')):
                counts = count(path.read_text())
                files[path.relative_to(root).as_posix()] = counts
                for category in CATEGORIES:
                    for kind in ('hardcoded', 'token'):
                        totals[category][kind] += counts[category][kind]
        for counts in totals.values():
            total = counts['token'] + counts['hardcoded']
            counts['token_ratio'] = round(counts['token'] / total, 6) if total else None
        modules[module.name] = totals
    if not files:
        raise ValueError('没有找到 feature Kotlin 源码，请检查仓库目录')
    return {'version': 1, 'files': files, 'modules': modules}


def regressions(current, baseline):
    failures = []
    for path, counts in current['files'].items():
        for category in CATEGORIES:
            limit = baseline['files'].get(path, {}).get(category, {}).get('hardcoded', 0)
            actual = counts[category]['hardcoded']
            if actual > limit:
                failures.append(f'{path}：{category} 硬编码 {actual}，基线 {limit}')
    return failures


def main():
    parser = argparse.ArgumentParser(description='统计 Android feature 的设计 token 采用率并检查硬编码增量')
    parser.add_argument('--root', type=Path, default=ROOT, help='仓库根目录')
    parser.add_argument('--baseline', type=Path, default=BASELINE, help='基线文件')
    parser.add_argument('--report', type=Path, help='另存本次统计结果')
    parser.add_argument('--update-baseline', action='store_true', help='明确接受当前计数并重写基线')
    args = parser.parse_args()
    current = scan(args.root)
    payload = json.dumps(current, ensure_ascii=False, indent=2, sort_keys=True) + '\n'
    if args.report:
        args.report.write_text(payload)
    if args.update_baseline:
        args.baseline.write_text(payload)
        print('已生成 token 采用率基线')
        return 0
    if not args.baseline.is_file():
        parser.error('基线不存在，请先运行 --update-baseline')
    baseline = json.loads(args.baseline.read_text())
    if baseline.get('version') != current['version']:
        parser.error('基线版本不匹配')
    for module, counts in current['modules'].items():
        parts = []
        for category, values in counts.items():
            ratio = values['token_ratio']
            rate = '无用量' if ratio is None else f'{ratio:.1%}'
            parts.append(f"{category} {rate}（token {values['token']}／硬编码 {values['hardcoded']}）")
        print(f"{module}：{'，'.join(parts)}")
    failures = regressions(current, baseline)
    if failures:
        print('\n硬编码超过基线：\n' + '\n'.join(failures))
        return 1
    print('token 基线检查通过')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
