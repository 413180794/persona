#!/usr/bin/env python3
"""Migrate Apple catalogs and their Swift literals to English development keys.

Run once on a Chinese-source catalog. The migration preserves each localization,
keeps colliding translations in distinct contextual keys, and remaps positional
arguments when English puts interpolations in a different order. No Swift compiler
is needed; unresolved source shapes fail before any file is written.
"""
import collections
import copy
import hashlib
import json
from pathlib import Path
import plistlib
import re
import sys

sys.path.insert(0, str(Path(__file__).resolve().parent))
import swift_literals

SPEC = re.compile(r"%(?:(\d+)\$)?(#@\w+@|[-+ 0#]*\d*(?:\.\d+)?(?:ll|l|h|hh|z|t|j|q|L)?[@dDiuUxXoOfeEgGcCsSpaA])")
CJK = swift_literals.CJK
DIRECTORIES = ['Persona', 'Shared', 'Mac', 'Vision', 'PersonaWatch', 'PersonaWidgets', 'LiveActivity', 'PersonaNotify', 'PersonaShare']
EXTRA = {
    'HTML 卡片': ('HTML Card', 'HTMLカード'),
    'VNC 小窗开着': ('VNC Floating Window Open', 'VNC小窓を表示中'),
    '已回复：%@': ('Replied: %@', '返信済み：%@'),
    '已打开：%@': ('Opened: %@', '開きました：%@'),
    '已打断 %lld 次': ('Interrupted %lld times', '%lld回中断しました'),
    '语音光球': ('Voice Orb', '音声オーブ'),
    '、': (', ', '、'), '。': ('.', '。'),
    '%@。': ('%@.', '%@。'), '%@。%@': ('%@. %@', '%@。%@'),
    '「%@」': ('“%@”', '「%@」'),
    '在车里和 Persona 语音长聊需要 iOS 26.4 或更新': ('Continuous voice chat with Persona in the car requires iOS 26.4 or later', '車内でPersonaと音声会話を続けるにはiOS 26.4以降が必要です'),
    '好，在 CarPlay 上接着聊': ('Continue on CarPlay', 'CarPlayで会話を続けます'),
    '连上 CarPlay 后打开 Persona 就能一直聊。': ('Connect CarPlay and open Persona to keep chatting.', 'CarPlayに接続してPersonaを開くと、会話を続けられます。'),
    '先在车里的屏幕上打开 Persona，就能接着聊': ('Open Persona on your car display to continue', '車の画面でPersonaを開くと、会話を続けられます'),
    '说「跟 Persona 聊聊」再聊': ('Say “Talk to Persona” to continue', '「Personaと話す」と言って会話を続けてください'),
    '轻点「继续」再聊': ('Tap Continue to keep chatting', '「続ける」をタップして会話を続けてください'),
    '正在连接 Persona': ('Connecting to Persona', 'Personaに接続中'),
    '我在听，直接说': ('Listening — go ahead', '聞いています。お話しください'),
    '我在听': ('Listening', '聞いています'),
    'Persona 在说，可以随时插话': ('Persona is speaking — interrupt anytime', 'Personaが話しています。いつでも割り込めます'),
    'Persona 在说': ('Persona Is Speaking', 'Personaが話しています'),
    'Persona 在处理': ('Persona Is Working', 'Personaが処理中'),
    '处理中': ('Processing', '処理中'),
    '已结束。轻点「继续」再聊': ('Ended. Tap Continue to keep chatting.', '終了しました。「続ける」をタップして会話を続けてください。'),
    'Persona 不在屏幕上时不听。回到 Persona 会接着聊': ('Persona pauses listening when off screen. Return to continue.', 'Personaの画面を離れると聞き取りを一時停止します。戻ると会話を続けられます。'),
    '已结束后台': ('Background Session Ended', 'バックグラウンドの会話を終了しました'),
    '已切换到别的语音对话': ('Switched to Another Voice Chat', '別の音声会話に切り替えました'),
    '来电或别的 App 占用了音频，对话已停': ('Audio is in use by a call or another app. Chat stopped.', '通話または別のアプリが音声を使用しています。会話を停止しました。'),
    '麦克风正在录音或听写，先结束它再聊': ('Stop recording or dictation before chatting', '録音または音声入力を終了してからお話しください'),
    '打不开麦克风，对话已停': ('Could not open the microphone. Chat stopped.', 'マイクを使用できません。会話を停止しました。'),
    '音频服务重置，对话已停': ('Audio service reset. Chat stopped.', '音声サービスがリセットされました。会話を停止しました。'),
    '好久没说话，对话已停': ('No speech for a while. Chat stopped.', 'しばらく発話がなかったため、会話を停止しました。'),
    '连不上 hub，对话已停': ('Could not connect to the hub. Chat stopped.', 'ハブに接続できません。会話を停止しました。'),
    'hub 拒绝了连接（%lld）': ('The hub rejected the connection (%lld)', 'ハブが接続を拒否しました（%lld）'),
    '语音扩展协议不匹配，请升级 hub 的语音扩展': ('Voice extension protocol mismatch. Update the hub’s voice extension.', '音声拡張機能のプロトコルが一致しません。ハブの音声拡張機能を更新してください。'),
    '这个语音模型现在不可用': ('This Voice Model Is Currently Unavailable', 'この音声モデルは現在使用できません'),
    '语音出错，对话已停': ('Voice error. Chat stopped.', '音声エラーが発生しました。会話を停止しました。'),
    '打断': ('Interrupt', '中断'),
    '先在手机上的 Persona 里用一次听写或录音，允许使用麦克风': ('Use dictation or recording in Persona on your phone to allow microphone access', 'スマートフォンのPersonaで音声入力または録音を使い、マイクへのアクセスを許可してください'),
    '没有麦克风权限，到 设置 › Persona 打开': ('Allow microphone access in Settings › Persona', '設定 › Personaでマイクへのアクセスを許可してください'),
    '还没配对 hub，先在手机上打开 Persona': ('Open Persona on your phone to pair a hub first', 'まずスマートフォンでPersonaを開き、ハブとペアリングしてください'),
    '先在手机上打开 Persona，等主对话建好再聊': ('Open Persona on your phone and wait for the main chat to be ready', 'スマートフォンでPersonaを開き、メインチャットの準備ができるまでお待ちください'),
    'hub 上还没有在运行的语音扩展，先在 hub 配好双工语音入口': ('Set up a duplex voice extension on the hub first', 'まずハブで双方向音声の拡張機能を設定してください'),
    '跟 ${applicationName} 聊聊': ('Talk to ${applicationName}', '${applicationName}と話す'),
    '和 ${applicationName} 聊天': ('Chat with ${applicationName}', '${applicationName}と会話する'),
    '跟 Persona 聊聊': ('Talk to Persona', 'Personaと話す'),
    '在 CarPlay 上开始和 Persona 连续、免提的语音对话：你说，它答，你随时可以插话。': ('Start a continuous, hands-free voice chat with Persona on CarPlay. Speak, hear replies, and interrupt anytime.', 'CarPlayでPersonaとのハンズフリーの音声会話を始めます。話しかけると回答が聞け、いつでも割り込めます。'),
    '聊聊': ('Talk', '話す'), '聊天': ('Chat', '会話'),
}


def placeholders(text):
    """Yield (span, argument index, specifier), skipping literal percent signs."""
    i, implicit = 0, 0
    while i < len(text):
        if text.startswith('%arg', i):
            i += 4
            continue
        if text.startswith('%%', i):
            i += 2
            continue
        m = SPEC.match(text, i)
        if m:
            implicit += 1
            yield m.start(), m.end(), int(m[1]) if m[1] else implicit, m[2]
            i = m.end()
        else:
            i += 1


def interpolations(text):
    """Split the literal's text without changing nested interpolation expressions."""
    pieces, exprs, i, last = [], [], 0, 0
    while i < len(text):
        if text.startswith('\\(', i):
            end = swift_literals._scan(text, i + 2, len(text), [], stop_paren=True)
            assert end <= len(text) and text[end - 1] == ')', text
            pieces.append(text[last:i])
            exprs.append(text[i + 2:end - 1])
            last = end
            i = end
        else:
            i += 2 if text.startswith('\\\\', i) else 1
    pieces.append(text[last:])
    return pieces, exprs


def decode(text):
    return re.sub(r'\\(u\{[0-9a-fA-F]+\}|[ntr0"\\])', lambda m: {
        'n': '\n', 't': '\t', 'r': '\r', '0': '\0', '"': '"', '\\': '\\'
    }.get(m[1], chr(int(m[1][2:-1], 16)) if m[1].startswith('u{') else m[0]), text)


def shape(text, literal=False):
    if literal:
        if text.startswith('\n'):
            lines = text.split('\n')[1:]
            indent = len(lines[-1]) if not lines[-1].strip() else 0
            text = '\n'.join(line[indent:] for line in lines[:-1])
        pieces, _ = interpolations(text)
        text = '\x00'.join(decode(p) for p in pieces)
    text = re.sub(r'\$\{\w+\}', '\x00', text)
    for start, end, _, _ in reversed(list(placeholders(text))):
        text = text[:start] + '\x00' + text[end:]
    return text.replace('%%', '%')


def unit(text):
    return {'stringUnit': {'state': 'translated', 'value': text}}


def canonical(key, entry):
    en = entry['localizations']['en']
    if 'substitutions' in en:
        text = en['stringUnit']['value']
        for name, sub in en['substitutions'].items():
            text = text.replace('%#@' + name + '@', f'%{sub["argNum"]}${sub["formatSpecifier"]}')
        return text
    if 'stringUnit' in en:
        return en['stringUnit']['value']
    plural = en.get('variations', {}).get('plural', {})
    if 'other' in plural:
        return plural['other']['stringUnit']['value']
    raise ValueError(f'No English source form: {key!r}')


def key_template(text):
    """Strip positional indexes; retain a permutation to relabel translated values."""
    if text == '中文':
        text = 'Chinese'
    text = text.replace('“Outlook 接入方式”', '“Outlook Connection”').replace('＋', '+')
    text = text.replace('简体中文', 'Simplified Chinese').replace('日本語', 'Japanese')
    assert not CJK.search(text), f'English source key still contains CJK: {text!r}'
    tokens = list(placeholders(text))
    order = [idx for _, _, idx, _ in tokens]
    assert len(order) == len(set(order)), (text, order)
    out = text
    for start, end, _, spec in reversed(tokens):
        out = out[:start] + '%' + spec + out[end:]
    return out, {old: new for new, old in enumerate(order, 1)}


def remap(holder, mapping):
    if 'stringUnit' in holder:
        text = holder['stringUnit']['value']
        for start, end, idx, spec in reversed(list(placeholders(text))):
            if spec.startswith('#@'):
                continue
            assert idx in mapping, (text, idx, mapping)
            text = text[:start] + f'%{mapping[idx]}${spec}' + text[end:]
        holder['stringUnit']['value'] = text
    for variants in holder.get('variations', {}).values():
        for variant in variants.values():
            remap(variant, mapping)
    for sub in holder.get('substitutions', {}).values():
        sub['argNum'] = mapping[sub['argNum']]
        remap(sub, mapping)  # %arg stays local; ordinary references still follow the permutation.


def literal_span(src, lit, starts):
    start = starts[lit.line - 1] + lit.col - 1
    m = re.match(r'(#+)?("""|")', src[start:])
    assert m, (lit.line, lit.col)
    close = m[2] + (m[1] or '')
    end = start + len(m[0]) + len(lit.text) + len(close)
    assert src[end-len(close):end] == close
    return start, end


def swift_string(template, exprs, permutation):
    """An English literal whose arguments retain the exact original expressions."""
    if not exprs:
        return json.dumps(template, ensure_ascii=False)
    if permutation != {i: i for i in permutation}:
        for expr in exprs:
            if '(' in expr and expr != 'AssistantScheduleText.title(t)':
                raise ValueError(f'Reordered expression needs a purity audit: {expr}')
    tokens = list(placeholders(template))
    named = list(re.finditer(r'\$\{(\w+)\}', template))
    if named:
        assert len(named) == len(exprs), (template, exprs)
        by_name = {}
        for expr in exprs:
            name = re.search(r'([A-Za-z_]\w*)$', expr)
            assert name and name[1] not in by_name, (template, exprs)
            by_name[name[1]] = expr
        out, pos = '', 0
        for m in named:
            assert m[1] in by_name, (template, exprs)
            expr = by_name[m[1]]
            out += json.dumps(template[pos:m.start()], ensure_ascii=False)[1:-1] + '\\(' + expr + ')'
            pos = m.end()
        out += json.dumps(template[pos:], ensure_ascii=False)[1:-1]
        return '"' + out + '"'
    assert len(tokens) == len(exprs), (template, exprs)
    inverse = {new: old for old, new in permutation.items()}
    out, pos = '', 0
    for start, end, idx, _ in tokens:
        out += json.dumps(template[pos:start].replace('%%', '%'), ensure_ascii=False)[1:-1]
        out += '\\(' + exprs[inverse[idx] - 1] + ')'
        pos = end
    out += json.dumps(template[pos:].replace('%%', '%'), ensure_ascii=False)[1:-1]
    return '"' + out + '"'


def ignored(src):
    lines = src.splitlines()
    if any('i18n-ignore-file' in row for row in lines[:20]):
        return None
    marked, stack = set(), []
    for n, row in enumerate(lines, 1):
        s = row.strip()
        if '// i18n-ignore' in row:
            marked.add(n + 1 if s.startswith('// i18n-ignore') else n)
        if s.startswith('#if'):
            stack.append('DEBUG' in s and '!' not in s)
        elif s.startswith('#else') and stack:
            stack[-1] = False
        elif s.startswith('#endif') and stack:
            stack.pop()
        if any(stack):
            marked.add(n)
    # Preview fixtures are not localized production sources.
    depth = None
    for n, row in enumerate(lines, 1):
        if row.lstrip().startswith('#Preview'):
            depth = 0
        if depth is not None:
            marked.add(n)
            depth += row.count('{') - row.count('}')
            if depth <= 0 and '{' in row:
                depth = None
    return marked


def direct_resource(before, src, start, typed_helpers=()):
    """Contexts with a literal-only localization type, or a direct localized API."""
    if any(re.search(r'\b' + re.escape(name) + r'\(\s*$', before) for name in typed_helpers):
        return True
    if 'DisplayRepresentation(' in before and re.search(r'\bsubtitle:[^\n]*$', before):
        return True
    if re.search(r'(?:String\(\s*localized:|LocalizedString(?:Resource|Key)(?:\.init)?\(|interfaceText\(|NSLocalizedString\()\s*$', before):
        return True
    if re.search(r':\s*LocalizedString(?:Resource|Key)\s*=\s*$', before):
        return True
    if re.search(r'(?:Text|Label|Button|Toggle|Picker|Section|LabeledContent|NavigationLink|ContentUnavailableView|Summary|IntentDialog|IntentDescription|ParameterSummary)\(\s*$', before):
        return True
    if re.search(r'\.(?:accessibilityLabel|accessibilityHint|accessibilityValue|navigationTitle|help|confirmationDialog|alert|searchable)\(\s*$', before):
        return True
    if re.search(r'(?:title|description|categoryName|requestValueDialog|shortTitle|name)\s*:\s*$', before) and ('/Intents/' in src or re.search(r'(?:@Parameter|DisplayRepresentation|TypeDisplayRepresentation)\([^\n]*$', before)):
        return True
    typed = re.search(r'var\s+\w+\s*:\s*LocalizedString(?:Resource|Key)\s*\{([^{}]*)$', before)
    if typed:
        return True
    if re.search(r'\.accessibilityAction\(named:\s*$', before):
        return True
    if '/Intents/' in src and ('searchKeywords:' in before or 'phrases:' in before):
        return True
    if re.search(r'(?:sheetCancel|sheetDone)\([^\n]*$', before):
        return True
    return False


def choose(candidates, lit, path):
    if len(candidates) == 1:
        return candidates[0]
    exact = decode(lit.text)
    if exact in candidates:
        return exact
    _, exprs = interpolations(lit.text)
    text = exprs[0] if exprs else ''
    if any(x in text for x in ['.name', '.title', '.displayTitle', 'senderName', 'formatted(']) or text in {'text', 'took', 'range'}:
        expected = '@'
    elif '.count' in text or text in {'count', 'thoughts', 'texts', 's', '$0', 'c', 'code'} or '.needsYou' in text or re.fullmatch(r'(?:t|ms) / 1000', text):
        expected = 'lld'
    else:
        raise ValueError(f'Unresolved interpolation type: {path}:{lit.line}: {text!r}: {candidates}')
    matches = [k for k in candidates if list(placeholders(k))[0][3] == expected]
    if len(matches) != 1:
        raise ValueError(f'Ambiguous {path}:{lit.line}: {lit.text!r}: {candidates}')
    return matches[0]


def migrate(root):
    root = Path(root)
    catalogs = {p: json.loads(p.read_text()) for p in sorted((root / 'apple').rglob('*.xcstrings'))}
    if any(d['sourceLanguage'] != 'zh-Hans' for d in catalogs.values()):
        raise ValueError('Run on Chinese-source catalogs only; already migrated catalogs are not rewritten')
    localizable = root / 'apple/Shared/Localization/Localizable.xcstrings'
    shortcuts = root / 'apple/Persona/Intents/AppShortcuts.xcstrings'
    for key, (en, ja) in EXTRA.items():
        table = shortcuts if '${applicationName}' in key else localizable
        catalogs[table]['strings'].setdefault(key, {'localizations': {
            'en': unit(en), 'zh-Hans': unit(key), 'ja': unit(ja)}})
    original_entries = {path: copy.deepcopy(data['strings']) for path, data in catalogs.items()}
    entries = original_entries[localizable]
    table_lookups = {}
    for path, strings in original_entries.items():
        table_lookup = collections.defaultdict(list)
        for key in strings:
            table_lookup[shape(key)].append(key)
        table_lookups[path] = table_lookup
    lookup = table_lookups[localizable]
    sources = {}
    scopes = {}
    for directory in DIRECTORIES:
        for path in sorted((root / 'apple' / directory).rglob('*.swift')):
            src = path.read_text()
            marked = ignored(src)
            if marked is None:
                continue
            lits = swift_literals.literals(src)
            sources[path] = (src, lits, marked)
            for lit in lits:
                if lit.has_cjk and lit.line not in marked:
                    for key in lookup.get(shape(lit.text, True), []):
                        scopes.setdefault(key, re.sub(r'(?<!^)([A-Z])', r' \1', path.stem))
    metadata = {}
    collisions = 0
    for path, data in catalogs.items():
        new = {}
        proposed = {}
        for key, original in data['strings'].items():
            entry = copy.deepcopy(original)
            locs = entry.setdefault('localizations', {})
            if 'en' not in locs:
                en, ja = EXTRA.get(key, (key, key))
                if CJK.search(en):
                    raise ValueError(f'Missing English translation: {key!r}')
                locs['en'] = unit(en)
                locs.setdefault('ja', unit(ja))
            locs.setdefault('zh-Hans', unit(key))
            locs.setdefault('ja', copy.deepcopy(locs['en']))
            for loc in locs.values():
                def translated(x):
                    if 'stringUnit' in x:
                        x['stringUnit']['state'] = 'translated'
                    for variants in x.get('variations', {}).values():
                        for v in variants.values():
                            translated(v)
                    for sub in x.get('substitutions', {}).values():
                        translated(sub)
                translated(loc)
            text = canonical(key, entry)
            template, permutation = key_template(text)
            # Existing semantic English keys and Info.plist identities remain stable.
            new_key = template if CJK.search(key) else key
            proposed[key] = (new_key, template, permutation, entry)
        groups = collections.defaultdict(list)
        for key, value in proposed.items():
            groups[value[0]].append(key)
        for key, (new_key, template, permutation, entry) in proposed.items():
            if len(groups[new_key]) > 1 and CJK.search(key):
                collisions += 1
                scope = scopes.get(key, 'Translation')
                suffix = hashlib.sha256(key.encode()).hexdigest()[:6]
                new_key += f' [{scope} {suffix}]'
            if CJK.search(key):
                for loc in entry['localizations'].values():
                    remap(loc, permutation)
                entry['extractionState'] = 'manual'
            else:
                # A stable semantic key has English defaultValue and original argument numbering.
                permutation = {i: i for i in permutation}
            assert new_key not in new, new_key
            new[new_key] = entry
            metadata[(path, key)] = (new_key, template, permutation)
        data['strings'] = dict(sorted(new.items()))
        data['sourceLanguage'] = 'en'
        plist = path.with_name('Info.plist')
        if path == root / 'apple/Persona/Resources/InfoPlist.xcstrings' and plist.exists():
            microphone = plistlib.loads(plist.read_bytes()).get('NSMicrophoneUsageDescription')
            locs = data['strings']['NSMicrophoneUsageDescription']['localizations']
            if microphone and ' — on the phone or in CarPlay —' in microphone:
                locs['en']['stringUnit']['value'] = microphone
                locs['zh-Hans']['stringUnit']['value'] = locs['zh-Hans']['stringUnit']['value'].replace(
                    '你发起语音对话时，', '你在手机或 CarPlay 上发起语音对话时，')
                locs['ja']['stringUnit']['value'] = locs['ja']['stringUnit']['value'].replace(
                    'あなたが始めた音声会話では、', 'スマートフォンまたはCarPlayであなたが始めた音声会話では、')
    edits_by_path = {}
    failures = []
    migrated = 0
    for path, (src, lits, marked) in sources.items():
        starts = [0] + [m.end() for m in re.finditer('\n', src)]
        typed_helpers = set(re.findall(r'func\s+(\w+)\(\s*(?:_\s+)?\w+\s*:\s*LocalizedString(?:Resource|Key)\b', src))
        edits = []
        for lit in reversed(lits):
            if not lit.has_cjk or lit.line in marked:
                continue
            start, end = literal_span(src, lit, starts)
            if src[start] == '#':
                failures.append(f'Raw localized literal requires a manual migration: {path}:{lit.line}')
                continue
            body_start = start + re.match(r'(#+)?("""|")', src[start:]).end()
            body = lit.text
            children = []
            for child in sorted(edits):
                if start < child[0] and child[1] < end and not any(a <= child[0] and child[1] <= b for a, b, _ in children):
                    children.append(child)
            for a, b, replacement in reversed(children):
                body = body[:a-body_start] + replacement + body[b-body_start:]
            before = src[max(0, start-1000):start].rstrip()
            if re.search(r'comment:\s*$', before):
                continue
            table = shortcuts if '/Intents/PersonaShortcuts.swift' in str(path) and '\\(.applicationName)' in lit.text else localizable
            old_keys = original_entries[table]
            candidates = table_lookups[table].get(shape(lit.text, True), [])
            try:
                if re.search(r'defaultValue:\s*$', before):
                    m = re.search(r'(?:String\(localized:|LocalizedStringResource\()\s*"([^"\n]+)"\s*,\s*defaultValue:\s*$', before)
                    if not m or m[1] not in old_keys:
                        raise ValueError(f'No semantic key for defaultValue: {path}:{lit.line}: {lit.text}')
                    key = m[1]
                    new_key, template, permutation = metadata[(table, key)]
                    replacement = swift_string(template, interpolations(body)[1], permutation)
                else:
                    if not candidates:
                        raise ValueError(f'No old key: {path}:{lit.line}: {lit.text}')
                    key = choose(candidates, lit, path)
                    new_key, template, permutation = metadata[(table, key)]
                    replacement = swift_string(new_key, interpolations(body)[1], permutation)
                    if not direct_resource(before, str(path), start, typed_helpers):
                        replacement = 'String(localized: ' + replacement + ')'
                edits.append((start, end, replacement))
                migrated += 1
            except (ValueError, AssertionError, KeyError) as e:
                failures.append(str(e))
        top_level = []
        for edit in sorted(edits):
            if not top_level or edit[0] >= top_level[-1][1]:
                top_level.append(edit)
        edits_by_path[path] = top_level
    if failures:
        raise ValueError('\n'.join(failures))
    for path, edits in edits_by_path.items():
        src = sources[path][0]
        for start, end, replacement in reversed(edits):
            src = src[:start] + replacement + src[end:]
        if edits:
            path.write_text(src)
    for path, data in catalogs.items():
        path.write_text(json.dumps(data, ensure_ascii=False, indent=2) + '\n')
    project = root / 'apple/project.yml'
    project.write_text(project.read_text().replace('developmentLanguage: zh-Hans', 'developmentLanguage: en'))
    for relative in ['PersonaNotify/Info.plist', 'PersonaShare/Info.plist']:
        plist = root / 'apple' / relative
        if plist.exists():
            plist.write_text(plist.read_text().replace('<key>CFBundleDevelopmentRegion</key>\n\t<string>zh-Hans</string>',
                                                      '<key>CFBundleDevelopmentRegion</key>\n\t<string>en</string>'))
    print(f'OK: migrated {migrated} Swift literals in {sum(bool(e) for e in edits_by_path.values())} files; '
          f'{sum(len(d["strings"]) for d in catalogs.values())} catalog entries; {collisions} collision entries retained')
    return metadata


if __name__ == '__main__':
    migrate(Path(__file__).resolve().parents[2] if len(sys.argv) == 1 else sys.argv[1])
