#!/usr/bin/env python3
"""Verify that an English-key migration retains every preexisting localization.

Usage: apple_english_keys_verify.py <pre-migration git revision>
Runs locally, without an Apple compiler. Compares complete translation trees,
including plural forms and substitutions, after the audited argument permutation.
"""
import copy
import json
from pathlib import Path
import plistlib
import subprocess
import sys

sys.path.insert(0, str(Path(__file__).resolve().parent))
import apple_english_keys as migration
import apple_l10n

ROOT = Path(__file__).resolve().parents[2]


def translated(holder):
    if 'stringUnit' in holder:
        holder['stringUnit']['state'] = 'translated'
    for forms in holder.get('variations', {}).values():
        for form in forms.values():
            translated(form)
    for substitution in holder.get('substitutions', {}).values():
        translated(substitution)


def verify(base, root=ROOT):
    root = Path(root)
    entries, trees, reordered = 0, 0, 0
    errors = []
    for path in sorted((root / 'apple').rglob('*.xcstrings')):
        relative = path.relative_to(root)
        old = json.loads(subprocess.check_output(['git', 'show', f'{base}:{relative}'], cwd=root, text=True))
        new = json.loads(path.read_text())
        for key, entry in old['strings'].items():
            locs = entry.get('localizations', {})
            if 'en' in locs:
                template, permutation = migration.key_template(migration.canonical(key, entry))
            else:
                en = migration.EXTRA.get(key, (key, key))[0]
                template, permutation = migration.key_template(en)
            english_key = template if migration.CJK.search(key) else key
            digest = migration.hashlib.sha256(key.encode()).hexdigest()[:6]
            scoped = [k for k in new['strings'] if k.startswith(english_key + ' [') and k.endswith(' ' + digest + ']')]
            candidates = scoped if scoped else [english_key] if english_key in new['strings'] else []
            if len(candidates) != 1:
                errors.append(f'{relative}: {key!r}: expected one migrated key, found {candidates}')
                continue
            migrated = new['strings'][candidates[0]]
            identity = {i: i for i in permutation}
            reordered += permutation != identity
            entries += 1
            for lang, holder in locs.items():
                expected = copy.deepcopy(holder)
                translated(expected)
                if migration.CJK.search(key):
                    migration.remap(expected, permutation)
                # An independently added CarPlay feature updated Info.plist but not its catalog.
                # This migration brings the three permission strings into agreement with it.
                if str(relative) == 'apple/Persona/Resources/InfoPlist.xcstrings' and key == 'NSMicrophoneUsageDescription':
                    plist = plistlib.loads(path.with_name('Info.plist').read_bytes())
                    microphone = plist.get(key, '')
                    if ' — on the phone or in CarPlay —' in microphone:
                        if lang == 'en':
                            expected['stringUnit']['value'] = microphone
                        elif lang == 'zh-Hans':
                            expected['stringUnit']['value'] = expected['stringUnit']['value'].replace(
                                '你发起语音对话时，', '你在手机或 CarPlay 上发起语音对话时，')
                        elif lang == 'ja':
                            expected['stringUnit']['value'] = expected['stringUnit']['value'].replace(
                                'あなたが始めた音声会話では、', 'スマートフォンまたはCarPlayであなたが始めた音声会話では、')
                if expected != migrated.get('localizations', {}).get(lang):
                    errors.append(f'{relative}: {key!r}: {lang} localization changed beyond argument renumbering')
                else:
                    trees += 1
        errors.extend(apple_l10n.catalog_problems(new, str(relative), infoplist=path.name == 'InfoPlist.xcstrings'))
    if errors:
        for error in errors:
            print(error)
        print(f'FAIL: {len(errors)} migration preservation errors')
        return 1
    print(f'OK: {entries} preexisting catalog entries and {trees} localization trees preserved; '
          f'{reordered} entries have audited argument permutations; all catalogs are complete in en / zh-Hans / ja')
    return 0


if __name__ == '__main__':
    if len(sys.argv) != 2:
        sys.exit('Usage: apple_english_keys_verify.py <pre-migration git revision>')
    sys.exit(verify(sys.argv[1]))
