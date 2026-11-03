import gzip
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
import sys
import tempfile
import time
import unittest

ROOT = Path(__file__).resolve().parents[2]
CONFIG = ROOT / 'scripts/persona_config.py'
INSTALL = ROOT / 'scripts/deploy/install.sh'
IDENTITY = ROOT / 'integrations/runtime-sdk/lib/identity.js'
RUNTIME = ROOT / 'integrations/runtime-sdk/lib/runtime.js'
PACKAGE = ROOT / 'integrations/runtime-sdk/lib/package.js'

GO_MAIN = r'''
package main

import (
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "time"
)

func main() {
    home := os.Getenv("HOME")
    mode := os.Getenv("EDGE_MODE")
    cached := DataDir(home)
    gate := os.Getenv("EDGE_GATE")
    if gate != "" {
        if err := os.WriteFile(os.Getenv("EDGE_READY"), []byte("ready"), 0600); err != nil { panic(err) }
        deadline := time.Now().Add(10 * time.Second)
        for {
            if _, err := os.Stat(gate); err == nil { break }
            if time.Now().After(deadline) { panic("gate timeout") }
            time.Sleep(time.Millisecond)
        }
    }
    root := cached
    var err error
    if mode != "query" { root, err = PrepareHome(home) }
    out := map[string]string{"cached": cached, "root": root, "query": DataDir(home)}
    if err != nil {
        out["error"] = err.Error()
    } else if mode == "write" {
        if err := os.WriteFile(filepath.Join(root, "go-state.fixture"), []byte("go-payload"), 0600); err != nil {
            out["error"] = err.Error()
        }
    }
    data, _ := json.Marshal(out)
    fmt.Println(string(data))
}
'''

NODE_INSTALL = r'''
import { writeFileSync, existsSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { setTimeout } from 'node:timers/promises';
import { pathToFileURL } from 'node:url';
const { prepareSocketDirectory, stateDir } = await import(pathToFileURL(process.env.EDGE_IDENTITY));
const { defaultSocketPath } = await import(pathToFileURL(process.env.EDGE_RUNTIME));
const { installCurrent, packageHash } = await import(pathToFileURL(process.env.EDGE_PACKAGE));
const socket = defaultSocketPath();
const files = [{ path: 'pi/index.js', data: Buffer.from('export const fixture = true;\n') }];
const hash = packageHash(files);
writeFileSync(process.env.EDGE_READY, 'ready');
const deadline = Date.now() + 10000;
while (!existsSync(process.env.EDGE_GATE)) {
  if (Date.now() >= deadline) throw new Error('gate timeout');
  await setTimeout(1);
}
try {
  const root = prepareSocketDirectory(socket);
  const store = join(root, 'runtime-packages', 'jarvis-pi-runtime');
  const installed = installCurrent(store, hash, files);
  console.log(JSON.stringify({ cached: dirname(socket), root, query: stateDir(), installed, hash }));
} catch (err) {
  console.log(JSON.stringify({ cached: dirname(socket), query: stateDir(), error: String(err) }));
}
'''

NODE_QUERY = r'''
import { pathToFileURL } from 'node:url';
const { stateDir } = await import(pathToFileURL(process.env.EDGE_IDENTITY));
const { defaultSocketPath, createRuntime } = await import(pathToFileURL(process.env.EDGE_RUNTIME));
const root = stateDir();
const socket = defaultSocketPath();
if (process.env.EDGE_DAEMON) {
  createRuntime({ listSessions: async () => [], items: async () => ({ items: [] }) },
                { runtime: 'migration-edge-fixture', listen: true, token: 'fixture-only' });
}
console.log(JSON.stringify({ root, socket }));
'''


class PersonaMigrationEdgesTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temporary = tempfile.TemporaryDirectory(prefix='persona-migration-edges-')
        cls.addClassCleanup(cls.temporary.cleanup)
        cls.base = Path(cls.temporary.name)
        cls.node = shutil.which('node')
        cls.go = shutil.which('go')
        cls.go_helper = None
        cls.downloads = cls.base / 'downloads'
        cls.downloads.mkdir()
        os_name = {'Darwin': 'darwin', 'Linux': 'linux'}.get(platform.system())
        arch = {'arm64': 'arm64', 'aarch64': 'arm64', 'x86_64': 'amd64', 'AMD64': 'amd64'}.get(platform.machine())
        if not os_name or not arch:
            raise unittest.SkipTest('installer fixture requires a supported OS/architecture')
        name = f'jarvisd-{os_name}-{arch}.gz'
        data = gzip.compress(b'#!/bin/sh\nif [ "$1" = version ]; then printf "fixture-version\\n"; fi\n', mtime=0)
        (cls.downloads / name).write_bytes(data)
        (cls.downloads / 'SHA256SUMS').write_text(hashlib.sha256(data).hexdigest() + '  ' + name + '\n')

    def setUp(self):
        directory = tempfile.TemporaryDirectory(prefix='home-', dir=self.base)
        self.addCleanup(directory.cleanup)
        self.home = Path(directory.name)

    def clean_env(self, home=None, **extra):
        env = {
            'HOME': str(home or self.home),
            'PATH': os.environ['PATH'],
            'TMPDIR': str(self.base),
            'PYTHONDONTWRITEBYTECODE': '1',
            'GOCACHE': str(self.base / 'go-cache'),
            'GOPATH': str(self.base / 'go-path'),
            'GOENV': 'off',
            'GOTOOLCHAIN': 'local',
            'GO111MODULE': 'off',
        }
        env.update(extra)
        return env

    def run_command(self, argv, env=None, timeout=20):
        return subprocess.run(argv, env=env or self.clean_env(), capture_output=True,
                              text=True, timeout=timeout)

    def install(self, home=None, **extra):
        env = self.clean_env(home, PERSONA_DL=self.downloads.as_uri(),
                             PERSONA_SKIP_HERDR='1', PERSONA_ROLE='hub',
                             PERSONA_ADMIN_ACCESS='lan')
        env.update(extra)
        return self.run_command(['sh', str(INSTALL)], env)

    def require_node(self):
        if not self.node:
            self.skipTest('node is not installed')

    def node_env(self, home=None, **extra):
        return self.clean_env(home, EDGE_IDENTITY=str(IDENTITY),
                              EDGE_RUNTIME=str(RUNTIME), EDGE_PACKAGE=str(PACKAGE), **extra)

    def node_query(self, home=None, **extra):
        self.require_node()
        result = self.run_command([self.node, '--input-type=module', '-e', NODE_QUERY],
                                  self.node_env(home, **extra))
        self.assertEqual(result.returncode, 0, result.stderr)
        return json.loads(result.stdout)

    def ensure_go_helper(self):
        if not self.go:
            self.skipTest('go is not installed')
        cls = type(self)
        if cls.go_helper is not None:
            return cls.go_helper
        source = self.base / 'go-helper-source'
        source.mkdir()
        for name in ('paths.go', 'env.go'):
            text = (ROOT / 'server/internal/identity' / name).read_text()
            self.assertTrue(text.startswith('package identity\n'), name)
            (source / name).write_text(text.replace('package identity', 'package main', 1))
        (source / 'main.go').write_text(GO_MAIN)
        cls.go_helper = self.base / 'go-helper'
        result = self.run_command([self.go, 'build', '-o', str(cls.go_helper),
                                   str(source / 'main.go'), str(source / 'paths.go'), str(source / 'env.go')],
                                  timeout=120)
        self.assertEqual(result.returncode, 0, result.stderr)
        return cls.go_helper

    def go_query(self, home=None, **extra):
        helper = self.ensure_go_helper()
        result = self.run_command([str(helper)], self.clean_env(home, EDGE_MODE='query', **extra))
        self.assertEqual(result.returncode, 0, result.stderr)
        return json.loads(result.stdout)

    def assert_same_directory(self, first, second):
        self.assertTrue(os.path.samefile(first, second), f'different stores: {first} / {second}')

    def write_profile(self, name):
        file = self.home / f'{name}.json'
        file.write_text(json.dumps({'app': {'bundle_id': f'com.example.{name}'},
                                    'hub': {'url': f'https://{name}.example.test'}}))
        return file

    def test_config_profile_switch_recomputes_derived_exports(self):
        a, b = self.write_profile('a'), self.write_profile('b')
        code = r'''
set -eu
export PERSONA_CONFIG="$EDGE_A" JARVIS_CONFIG="$EDGE_A"
eval "$("$EDGE_PYTHON" -I -B "$EDGE_CONFIG" env)"
export PERSONA_CONFIG="$EDGE_B" JARVIS_CONFIG="$EDGE_B"
eval "$("$EDGE_PYTHON" -I -B "$EDGE_CONFIG" env)"
printf '%s|%s|%s|%s\n' "$JARVIS_HUB_URL" "$PERSONA_HUB_URL" "$JARVIS_BUNDLE_ID" "$PERSONA_BUNDLE_ID"
'''
        result = self.run_command(['sh', '-c', code], self.clean_env(
            EDGE_A=str(a), EDGE_B=str(b), EDGE_PYTHON=sys.executable, EDGE_CONFIG=str(CONFIG)))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.strip(),
                         'https://b.example.test|https://b.example.test|com.example.b|com.example.b')

    def test_legacy_config_switch_does_not_create_implicit_current_override(self):
        a, b = self.write_profile('a'), self.write_profile('b')
        code = r'''
set -eu
export JARVIS_CONFIG="$EDGE_A"
eval "$("$EDGE_PYTHON" -I -B "$EDGE_CONFIG" env)"
[ "${PERSONA_CONFIG+x}" != x ]
export JARVIS_CONFIG="$EDGE_B"
eval "$("$EDGE_PYTHON" -I -B "$EDGE_CONFIG" env)"
[ "${PERSONA_CONFIG+x}" != x ]
printf '%s|%s\n' "$JARVIS_HUB_URL" "$PERSONA_BUNDLE_ID"
'''
        result = self.run_command(['sh', '-c', code], self.clean_env(
            EDGE_A=str(a), EDGE_B=str(b), EDGE_PYTHON=sys.executable, EDGE_CONFIG=str(CONFIG)))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.strip(), 'https://b.example.test|com.example.b')

    def test_runtime_environment_bridge_preserves_explicit_empty_current_value(self):
        config = self.write_profile('a')
        code = r'''
set -eu
eval "$("$EDGE_PYTHON" -I -B "$EDGE_CONFIG" env)"
printf '%s|%s\n' "${JARVIS_RUNTIME_SOCKET}" "${JARVIS_ASSISTANT}"
[ "${PERSONA_LEGACY_ONLY+x}" != x ]
'''
        result = self.run_command(['sh', '-c', code], self.clean_env(
            JARVIS_CONFIG=str(config), PERSONA_RUNTIME_SOCKET='', JARVIS_RUNTIME_SOCKET='legacy.sock',
            PERSONA_ASSISTANT='1', JARVIS_ASSISTANT='0', JARVIS_LEGACY_ONLY='legacy',
            EDGE_PYTHON=sys.executable, EDGE_CONFIG=str(CONFIG)))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.strip(), '|1')

    def test_external_alias_dotdot_installs_in_the_runtime_lexical_root(self):
        state = self.home / 'storage/state'
        state.mkdir(parents=True)
        payload = state / 'hub.json'
        payload.write_text('{"fixture":"unchanged"}')
        (self.home / '.jarvis').symlink_to('storage/state')
        (self.home / '.persona').symlink_to('.jarvis')
        override = str(self.home / '.persona') + '/../profile'
        result = self.install(PERSONA_HOME=override)
        self.assertEqual(result.returncode, 0, result.stderr)
        expected = self.home / 'profile'
        self.assertTrue((expected / 'bin/jarvisd').is_file())
        self.assertTrue((expected / 'bin/personad').is_file())
        self.assertFalse((self.home / 'storage/profile').exists())
        self.assertEqual(payload.read_text(), '{"fixture":"unchanged"}')
        self.assertEqual(self.go_query(PERSONA_HOME=override)['root'], str(expected))
        self.assertEqual(self.node_query(PERSONA_HOME=override)['root'], str(expected))
        self.assert_same_directory(self.home / '.persona', state)

    def test_default_root_spelling_keeps_the_legacy_alias(self):
        for spelling in ('absolute-slash', 'absolute-dot', 'tilde-slash', 'relative-dot', 'dotdot'):
            with self.subTest(spelling=spelling), tempfile.TemporaryDirectory(dir=self.base) as directory:
                home = Path(directory)
                override = {
                    'absolute-slash': str(home / '.persona') + '/',
                    'absolute-dot': str(home / '.persona') + '/.',
                    'tilde-slash': '~/.persona/',
                    'relative-dot': '.persona/.',
                    'dotdot': str(home / '.persona') + '/../.persona/',
                }[spelling]
                result = self.install(home, PERSONA_HOME=override)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertTrue((home / '.persona').is_symlink())
                self.assertTrue((home / '.jarvis').is_dir())
                self.assertFalse((home / '.jarvis').is_symlink())
                self.assert_same_directory(home / '.persona', home / '.jarvis')
                self.assertTrue((home / '.jarvis/bin/jarvisd').is_file())
                self.assertEqual(Path(self.go_query(home, PERSONA_HOME=override)['root']), home / '.persona')
                self.assert_same_directory(self.node_query(home, PERSONA_HOME=override)['root'], home / '.persona')

    def test_nested_dangling_profile_does_not_touch_default_state(self):
        for target in ('.jarvis', '.persona'):
            with self.subTest(target=target), tempfile.TemporaryDirectory(dir=self.base) as directory:
                home = Path(directory)
                (home / 'profiles').mkdir()
                active = home / 'profiles/active'
                active.symlink_to(target)
                result = self.install(home, PERSONA_HOME=str(active))
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('unavailable state symlink', result.stderr)
                self.assertEqual(os.readlink(active), target)
                self.assertFalse(os.path.lexists(home / '.jarvis'))
                self.assertFalse(os.path.lexists(home / '.persona'))
                self.assertFalse(os.path.lexists(home / 'profiles' / target))

    def test_explicit_empty_home_overrides_the_old_profile(self):
        old = self.home / 'old-profile'
        result = self.install(PERSONA_HOME='', JARVIS_HOME=str(old))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(old.exists())
        self.assert_same_directory(self.home / '.persona', self.home / '.jarvis')
        env = {'PERSONA_HOME': '', 'JARVIS_HOME': str(old)}
        self.assert_same_directory(self.go_query(**env)['root'], self.node_query(**env)['root'])

    def test_socket_queries_and_daemon_construction_do_not_create_directories(self):
        for mode in ('default', 'profile', 'explicit-socket', 'daemon'):
            with self.subTest(mode=mode), tempfile.TemporaryDirectory(dir=self.base) as directory:
                home = Path(directory)
                extra = {}
                if mode == 'profile':
                    extra['PERSONA_HOME'] = str(home / 'profile')
                elif mode == 'explicit-socket':
                    extra['PERSONA_RUNTIME_SOCKET'] = str(home / 'socket-only/runtime.sock')
                elif mode == 'daemon':
                    extra['EDGE_DAEMON'] = '1'
                query = self.node_query(home, **extra)
                self.assertIsInstance(query['root'], str)
                self.assertEqual(list(home.iterdir()), [])

    def concurrent_install(self, home):
        helper = self.ensure_go_helper()
        self.require_node()
        gate = home / 'start.gate'
        go_ready, node_ready = home / 'go.ready', home / 'node.ready'
        go_env = self.clean_env(home, EDGE_MODE='write', EDGE_GATE=str(gate), EDGE_READY=str(go_ready))
        node_env = self.node_env(home, EDGE_GATE=str(gate), EDGE_READY=str(node_ready))
        processes = []
        try:
            processes.append(subprocess.Popen([str(helper)], env=go_env, stdout=subprocess.PIPE,
                                              stderr=subprocess.PIPE, text=True))
            processes.append(subprocess.Popen([self.node, '--input-type=module', '-e', NODE_INSTALL],
                                              env=node_env, stdout=subprocess.PIPE,
                                              stderr=subprocess.PIPE, text=True))
            deadline = time.monotonic() + 15
            while not (go_ready.exists() and node_ready.exists()):
                self.assertTrue(all(p.poll() is None for p in processes), 'initializer exited before gate')
                self.assertLess(time.monotonic(), deadline, 'initializer readiness timeout')
                time.sleep(0.005)
            gate.write_text('start')
            outputs = []
            for process in processes:
                stdout, stderr = process.communicate(timeout=15)
                self.assertEqual(process.returncode, 0, stderr)
                outputs.append(json.loads(stdout))
            return outputs
        finally:
            for process in processes:
                if process.poll() is None:
                    process.kill()
                process.communicate(timeout=5)

    def test_actual_go_and_sdk_package_initialization_do_not_split_state(self):
        for initial in ('fresh', 'current', 'legacy'):
            for trial in range(12):
                with self.subTest(initial=initial, trial=trial), tempfile.TemporaryDirectory(dir=self.base) as directory:
                    home = Path(directory)
                    seed = None
                    if initial != 'fresh':
                        seed = home / ('.persona' if initial == 'current' else '.jarvis')
                        seed.mkdir()
                        (seed / 'existing.fixture').write_text('existing-payload')
                    go_result, sdk_result = self.concurrent_install(home)
                    for result in (go_result, sdk_result):
                        if 'error' in result:
                            self.assertIn('initialization', result['error'])
                        else:
                            for key in ('cached', 'root', 'query'):
                                self.assert_same_directory(result[key], self.go_query(home)['root'])
                    if 'error' not in go_result and 'error' not in sdk_result:
                        self.assert_same_directory(home / '.persona', home / '.jarvis')
                        self.assert_same_directory(go_result['root'], sdk_result['root'])
                    if 'error' not in go_result:
                        self.assertEqual((Path(go_result['root']) / 'go-state.fixture').read_text(), 'go-payload')
                    if 'error' not in sdk_result:
                        installed = Path(sdk_result['installed'])
                        self.assertEqual((installed / 'pi/index.js').read_bytes(), b'export const fixture = true;\n')
                        self.assert_same_directory(installed, installed.parent / 'current')
                    if seed is not None:
                        self.assertEqual((seed / 'existing.fixture').read_text(), 'existing-payload')

    def test_dot_prefixed_dangling_default_alias_is_restored(self):
        self.require_node()
        helper = self.ensure_go_helper()
        for implementation in ('go', 'sdk', 'installer'):
            for alias, target in (('.persona', './.jarvis'), ('.jarvis', './.persona')):
                with self.subTest(implementation=implementation, alias=alias), tempfile.TemporaryDirectory(dir=self.base) as directory:
                    home = Path(directory)
                    (home / alias).symlink_to(target)
                    if implementation == 'go':
                        result = self.run_command([str(helper)], self.clean_env(home))
                        self.assertEqual(result.returncode, 0, result.stderr)
                        self.assertNotIn('error', json.loads(result.stdout))
                    elif implementation == 'sdk':
                        code = r'''
import { pathToFileURL } from 'node:url';
const { prepareStateDir } = await import(pathToFileURL(process.env.EDGE_IDENTITY));
console.log(prepareStateDir());
'''
                        result = self.run_command([self.node, '--input-type=module', '-e', code], self.node_env(home))
                        self.assertEqual(result.returncode, 0, result.stderr)
                    else:
                        result = self.install(home)
                        self.assertEqual(result.returncode, 0, result.stderr)
                    self.assertEqual(os.readlink(home / alias), target)
                    self.assert_same_directory(home / '.persona', home / '.jarvis')

    def test_dot_prefixed_external_dangling_target_remains_rejected(self):
        self.require_node()
        helper = self.ensure_go_helper()
        for implementation in ('go', 'sdk', 'installer'):
            for alias in ('.persona', '.jarvis'):
                with self.subTest(implementation=implementation, alias=alias), tempfile.TemporaryDirectory(dir=self.base) as directory:
                    home = Path(directory)
                    (home / alias).symlink_to('./missing-external')
                    if implementation == 'go':
                        result = self.run_command([str(helper)], self.clean_env(home))
                        self.assertEqual(result.returncode, 0, result.stderr)
                        self.assertIn('error', json.loads(result.stdout))
                    elif implementation == 'sdk':
                        code = r'''
import { pathToFileURL } from 'node:url';
const { prepareStateDir } = await import(pathToFileURL(process.env.EDGE_IDENTITY));
try { prepareStateDir(); process.exitCode = 2; } catch (err) {
  if (!String(err).includes('unavailable symlink target')) throw err;
}
'''
                        result = self.run_command([self.node, '--input-type=module', '-e', code], self.node_env(home))
                        self.assertEqual(result.returncode, 0, result.stderr)
                    else:
                        result = self.install(home)
                        self.assertNotEqual(result.returncode, 0)
                    self.assertEqual(os.readlink(home / alias), './missing-external')
                    self.assertFalse((home / 'missing-external').exists())
                    other = '.jarvis' if alias == '.persona' else '.persona'
                    self.assertFalse(os.path.lexists(home / other))

    def test_existing_distinct_roots_are_not_merged_or_deleted(self):
        for name in ('.persona', '.jarvis'):
            (self.home / name).mkdir()
            (self.home / name / 'existing.fixture').write_text(name)
        go_result, sdk_result = self.concurrent_install(self.home)
        for name in ('.persona', '.jarvis'):
            self.assertEqual((self.home / name / 'existing.fixture').read_text(), name)
        for result in (go_result, sdk_result):
            self.assertNotIn('error', result)
            self.assert_same_directory(result['root'], self.home / '.jarvis')
        self.assertFalse(os.path.samefile(self.home / '.persona', self.home / '.jarvis'))


if __name__ == '__main__':
    unittest.main()
