import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

from test_persona_script_identity import identity, environment, SCRIPT


class PersonaServiceEnvFileTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix='persona-envfile-')
        self.addCleanup(temporary.cleanup)
        self.home = Path(temporary.name)
        self.unit = self.home / '.config/systemd/user/jarvisd.service'
        self.unit.parent.mkdir(parents=True)

    def root(self):
        return identity.data_dir(self.home, identity.service_environment(self.home, {}, 'hub'))

    def test_other_environment_fields_do_not_break_a_legacy_install_or_execute(self):
        old = self.home / '.jarvis'; old.mkdir()
        marker = self.home / 'not-executed'
        file = self.home / 'provider.env'
        file.write_text('API_KEY=fixture-only-private-value\nCOMMAND=$(touch ' + str(marker) + ')\nNAME=O\'Brien\n')
        self.unit.write_text('[Service]\nEnvironmentFile=' + str(file) + '\n')
        self.assertEqual(self.root(), str(old))
        self.assertFalse(marker.exists())
        self.assertEqual(identity._file_environment(file), {})
        result = subprocess.run([sys.executable, str(SCRIPT), '--service', 'hub'], env=environment(self.home), capture_output=True, text=True, timeout=10)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertNotIn('fixture-only-private-value', result.stdout + result.stderr)

    def test_files_override_inline_assignments_in_order_without_shell_expansion(self):
        first = self.home / 'first.env'; second = self.home / 'second file.env'
        first.write_text('PERSONA_HOME=first\nJARVIS_HOME=legacy\n')
        second.write_text('PERSONA_HOME="profile with $HOME %h spaces"\nAPI_KEY=fixture-only-private-value\n')
        self.unit.write_text('[Service]\nEnvironment="PERSONA_HOME=inline"\nEnvironmentFile=' + str(first) + '\nEnvironmentFile=' + str(second) + '\n')
        self.assertEqual(self.root(), str(self.home / 'profile with $HOME %h spaces'))
        values = identity.service_environment(self.home, {}, 'hub')
        self.assertEqual(set(values), {'PERSONA_HOME', 'JARVIS_HOME'})
        self.assertEqual(values['PERSONA_HOME'], 'profile with $HOME %h spaces')

    def test_optional_files_globs_and_reset_do_not_resurrect_old_roots(self):
        files = self.home / 'environment'; files.mkdir()
        (files / '10-old.env').write_text('PERSONA_HOME=old\n')
        (files / '20-new.env').write_text('PERSONA_HOME=new\n')
        self.unit.write_text('[Service]\nEnvironmentFile=-' + str(self.home / 'absent.env') + '\nEnvironmentFile=' + str(files / '*.env') + '\n')
        self.assertEqual(self.root(), str(self.home / 'new'))
        dropin = self.unit.with_name(self.unit.name + '.d') / 'override.conf'
        dropin.parent.mkdir()
        dropin.write_text('[Service]\nEnvironmentFile=\nEnvironment="JARVIS_HOME=after-reset"\n')
        self.assertEqual(self.root(), str(self.home / 'after-reset'))

    def test_unset_is_applied_after_files_and_can_itself_be_reset(self):
        file = self.home / 'root.env'; file.write_text('PERSONA_HOME=new\nJARVIS_HOME=old\n')
        self.unit.write_text('[Service]\nUnsetEnvironment=PERSONA_HOME\nEnvironmentFile=' + str(file) + '\n')
        self.assertEqual(self.root(), str(self.home / 'old'))
        self.unit.write_text(self.unit.read_text() + 'UnsetEnvironment=\nUnsetEnvironment="PERSONA_HOME=not-matching"\n')
        self.assertEqual(self.root(), str(self.home / 'new'))
        self.unit.write_text(self.unit.read_text() + 'UnsetEnvironment="PERSONA_HOME=new"\n')
        self.assertEqual(self.root(), str(self.home / 'old'))

    def test_defined_empty_new_home_in_a_file_suppresses_the_legacy_value(self):
        file = self.home / 'root.env'; file.write_text('PERSONA_HOME=\nJARVIS_HOME=wrong\n')
        self.unit.write_text('[Service]\nEnvironmentFile=' + str(file) + '\n')
        self.assertEqual(self.root(), str(self.home / '.persona'))
        self.assertFalse((self.home / 'wrong').exists())

    def test_quote_escape_and_multiline_other_fields_never_become_home_assignments(self):
        file = self.home / 'root.env'
        file.write_text('# ignored unmatched quote \"\nSECRET="first\nPERSONA_HOME=not-a-root\nlast"\n'
                        'PERSONA_HOME="profile with \\"quotes\\" \\$literal \\q"\n')
        self.assertEqual(identity._file_environment(file), {'PERSONA_HOME': 'profile with "quotes" $literal \\q'})
        file.write_text("PERSONA_HOME='single \\ backslash $literal'\n")
        self.assertEqual(identity._file_environment(file), {'PERSONA_HOME': 'single \\ backslash $literal'})
        file.write_text('PERSONA_HOME=profile\\ with\\ spaces\\\n/continued\n')
        self.assertEqual(identity._file_environment(file), {'PERSONA_HOME': 'profile with spaces/continued'})

    def test_escaped_trailing_space_never_merges_the_following_private_field(self):
        file = self.home / 'root.env'
        file.write_text('PERSONA_HOME=profile\\ \nOTHER=opaque-fixture-value\nJARVIS_HOME=correct\n')
        self.assertEqual(identity._file_environment(file), {'PERSONA_HOME': 'profile ', 'JARVIS_HOME': 'correct'})
        self.unit.write_text('[Service]\nEnvironmentFile=' + str(file) + '\n')
        result = subprocess.run([sys.executable, str(SCRIPT), '--service', 'hub'], env=environment(self.home), capture_output=True, text=True, timeout=10)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, str(self.home / 'profile ') + '\n')
        self.assertNotIn('opaque-fixture-value', result.stdout + result.stderr)

    def test_all_quote_segments_are_consumed_before_the_next_assignment(self):
        file = self.home / 'root.env'
        file.write_text('OTHER="begin" "continued\nPERSONA_HOME=opaque-fixture-value\nend"\nJARVIS_HOME=correct\n')
        self.assertEqual(identity._file_environment(file), {'JARVIS_HOME': 'correct'})
        file.write_text('PERSONA_HOME="profile" " with spaces"\n')
        self.assertEqual(identity._file_environment(file), {'PERSONA_HOME': 'profile with spaces'})

    def test_systemd_compatible_eof_in_other_quoted_fields_keeps_the_legacy_root(self):
        old = self.home / '.jarvis'; old.mkdir()
        file = self.home / 'root.env'
        for quote in ('"', "'"):
            with self.subTest(quote=quote):
                file.write_text('OTHER=' + quote + 'unclosed opaque fixture\n')
                self.unit.write_text('[Service]\nEnvironmentFile=' + str(file) + '\n')
                self.assertEqual(identity._file_environment(file), {})
                self.assertEqual(self.root(), str(old))

    def test_unset_exact_values_expand_the_same_unit_home_specifier(self):
        self.unit.write_text('[Service]\nEnvironment="PERSONA_HOME=%h/new" "JARVIS_HOME=%h/old"\nUnsetEnvironment="PERSONA_HOME=%h/new"\n')
        self.assertEqual(self.root(), str(self.home / 'old'))

    def test_optional_dangling_environment_file_is_ignored_after_glob_matching(self):
        file = self.home / 'optional.env'; file.symlink_to('not-generated.env')
        self.unit.write_text('[Service]\nEnvironment="JARVIS_HOME=retained"\nEnvironmentFile=-' + str(file) + '\n')
        self.assertEqual(self.root(), str(self.home / 'retained'))
        self.unit.write_text('[Service]\nEnvironmentFile=' + str(file) + '\n')
        with self.assertRaises(FileNotFoundError):
            self.root()

    def test_optional_non_home_invalid_utf8_does_not_block_the_retained_root(self):
        file = self.home / 'optional.env'; file.write_bytes(b'OTHER=opaque\xff\n')
        self.unit.write_text('[Service]\nEnvironment="JARVIS_HOME=retained"\nEnvironmentFile=-' + str(file) + '\n')
        self.assertEqual(self.root(), str(self.home / 'retained'))
        result = subprocess.run([sys.executable, str(SCRIPT), '--service', 'hub'], env=environment(self.home), capture_output=True, text=True, timeout=10)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, str(self.home / 'retained') + '\n')

    def test_unit_c_escaped_unset_words_preserve_word_boundaries(self):
        root = self.home / 'profile with spaces'
        file = self.home / 'root.env'; file.write_text('PERSONA_HOME="' + str(root) + '"\nJARVIS_HOME=legacy\n')
        for encoded in (r'%h/profile\x20with\x20spaces', r'%h/profile\040with\040spaces', r'%h/profile with spaces'):
            with self.subTest(encoded=encoded):
                self.unit.write_text('[Service]\nEnvironmentFile=' + str(file) + '\nUnsetEnvironment="PERSONA_HOME=' + encoded + '"\n')
                self.assertEqual(self.root(), str(self.home / 'legacy'))
        self.assertEqual(identity._unit_words(r'PERSONA_HOME=%h/a\x20b'), ['PERSONA_HOME=%h/a b'])
        self.assertEqual(identity._unit_words(r'"PERSONA_HOME=literal\\x20"'), [r'PERSONA_HOME=literal\x20'])

    def test_byte_escapes_form_utf8_while_unicode_escapes_form_codepoints(self):
        root = self.home / 'café'
        file = self.home / 'root.env'
        file.write_text('PERSONA_HOME="' + str(root) + '"\nJARVIS_HOME=legacy\n')
        for encoded in (r'%h/caf\xC3\xA9', r'%h/caf\303\251', r'%h/café', r'%h/caf\U000000E9'):
            with self.subTest(encoded=encoded):
                self.unit.write_text('[Service]\nEnvironmentFile=' + str(file) + '\nUnsetEnvironment="PERSONA_HOME=' + encoded + '"\n')
                self.assertEqual(self.root(), str(self.home / 'legacy'))
        self.assertEqual(identity._unit_words(r'"PERSONA_HOME=caf\xC3\xA9"'), ['PERSONA_HOME=café'])
        self.assertEqual(identity._unit_words(r'"PERSONA_HOME=\xF0\x9F\x8C\x9F"'), ['PERSONA_HOME=🌟'])
        self.assertEqual(identity._unit_words(r'"PERSONA_HOME=literal\\xC3\\xA9"'), [r'PERSONA_HOME=literal\xC3\xA9'])
        with self.assertRaises(ValueError):
            identity._unit_words(r'"PERSONA_HOME=\xFF"')

    def test_missing_required_file_fails_before_creating_data_and_explicit_root_bypasses_it(self):
        self.unit.write_text('[Service]\nEnvironmentFile=' + str(self.home / 'absent.env') + '\n')
        with self.assertRaises(FileNotFoundError):
            self.root()
        self.assertFalse((self.home / '.persona').exists())
        values = identity.service_environment(self.home, {'PERSONA_HOME': 'explicit'}, 'hub')
        self.assertEqual(identity.data_dir(self.home, values), str(self.home / 'explicit'))

    def test_embedded_resolver_recovers_envfile_root_without_returning_other_values(self):
        root = self.home / 'custom remote profile'
        file = self.home / 'root.env'; file.write_text('PERSONA_HOME="' + str(root) + '"\nAPI_KEY=fixture-only-private-value\n')
        self.unit.write_text('[Service]\nEnvironmentFile=' + str(file) + '\n')
        result = subprocess.run([sys.executable, str(SCRIPT), '--service', 'hub', '--prepare', '--prefix', '-'],
                                env=environment(self.home), capture_output=True, text=True, timeout=10)
        self.assertEqual(result.returncode, 0, result.stderr)
        command = result.stdout + '\nprintf "%s" "$PERSONA_HOME"\n'
        run = subprocess.run(['bash', '-c', command], env=environment(self.home), capture_output=True, text=True, timeout=10)
        self.assertEqual(run.returncode, 0, run.stderr)
        self.assertEqual(run.stdout, str(root))
        self.assertNotIn('fixture-only-private-value', result.stdout + result.stderr + run.stdout + run.stderr)
        self.assertFalse((self.home / '.jarvis').exists())


if __name__ == '__main__':
    unittest.main()
