#!/usr/bin/env python3
"""Execute the resource-only acceptance matrix, never a hub/node or deployment.

Use an already verified, pinned Deno executable. Raw output belongs in a private
log supplied by the caller. Cgroup settings and optional config files are read
only and compared before/after; no controller or service settings are written.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[2]


def state(watch):
    membership = Path('/proc/self/cgroup').read_text()
    result = {'membership': membership, 'controllers': {}, 'watched_hashes': {}}
    groups = {}
    for line in membership.splitlines():
        number, controllers, path = line.split(':', 2)
        if number == '0' and not controllers:
            groups['v2'] = path
        for name in controllers.split(','):
            groups[name] = path
    for line in Path('/proc/self/mountinfo').read_text().splitlines():
        left, right = line.split(' - ', 1)
        fields, fs = left.split(), right.split()
        if fs[0] not in ('cgroup', 'cgroup2'):
            continue
        names = ['v2'] if fs[0] == 'cgroup2' else fs[2].split(',')
        for name in names:
            group = groups.get(name)
            if group is None:
                continue
            relative = os.path.relpath(group, fields[3])
            if relative.startswith('..'):
                continue
            directory = Path(fields[4]) / relative
            for filename in ('memory.max', 'memory.swap.max', 'cpu.max',
                             'memory.limit_in_bytes', 'memory.memsw.limit_in_bytes',
                             'cpu.cfs_quota_us', 'cpu.cfs_period_us', 'pids.max'):
                file = directory / filename
                if file.exists():
                    result['controllers'][str(file)] = file.read_text().strip()
    for file in watch:
        result['watched_hashes'][str(file)] = hashlib.sha256(file.read_bytes()).hexdigest()
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--engine', required=True, type=Path)
    parser.add_argument('--sha256', required=True)
    parser.add_argument('--log', required=True, type=Path)
    parser.add_argument('--watch-file', action='append', type=Path, default=[])
    args = parser.parse_args()
    env = dict(os.environ, TEST_DENO_PATH=str(args.engine.resolve()),
               TEST_DENO_SHA256=args.sha256, PERSONA_DEV_SOURCE_RESOURCES='', GOFLAGS='')
    before = state(args.watch_file)
    phases = [
        ('production-default-and-opt-in', ['go', 'test', '-p', '1', '-count=1', '-v',
         '-timeout=90s', '-run', 'TestProduction', './internal/extension/denohost']),
        ('development-real-resource-and-isolation', ['go', 'test', '-p', '1', '-count=1',
         '-tags', 'persona_dev_resources', '-v', '-timeout=90s', '-run',
         'TestDenoDevelopment|TestDevelopment|TestSourceDevelopment', './internal/extension/denohost',
         './internal/extension/sandbox', './internal/extmgr']),
    ]
    failed = False
    # Exclusive creation prevents overwriting somebody else's private evidence.
    with args.log.open('x', encoding='utf-8') as log:
        os.chmod(args.log, 0o600)
        log.write('RESOURCE-ONLY ACCEPTANCE; no hub/node/deployment started\n')
        log.write('environment ' + json.dumps({k: env[k] for k in
                  ('TEST_DENO_PATH', 'TEST_DENO_SHA256', 'PERSONA_DEV_SOURCE_RESOURCES')}) + '\n')
        log.write('before ' + json.dumps(before, sort_keys=True) + '\n')
        for name, command in phases:
            log.write('\nCOMMAND ' + json.dumps(command) + '\n')
            log.flush()
            start = time.monotonic()
            proc = subprocess.Popen(command, cwd=ROOT / 'server', env=env,
                                    stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
            try:
                code = proc.wait(timeout=150)
                verdict = 'PASS' if code == 0 else 'FAIL'
            except subprocess.TimeoutExpired:
                os.killpg(proc.pid, signal.SIGTERM)
                try:
                    proc.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    os.killpg(proc.pid, signal.SIGKILL)
                    proc.wait(timeout=5)
                verdict, code = 'TIMEOUT', -1
            line = f'{name}: {verdict} exit={code} elapsed={time.monotonic()-start:.3f}s'
            log.write(line + '\n')
            print(line, flush=True)
            failed |= code != 0
        after = state(args.watch_file)
        unchanged = after == before
        log.write('after ' + json.dumps(after, sort_keys=True) + '\n')
        log.write(f'cgroup-and-watched-config-unchanged={unchanged}\n')
        print(f'cgroup-and-watched-config: {"PASS" if unchanged else "FAIL"}', flush=True)
        failed |= not unchanged
    return 1 if failed else 0


if __name__ == '__main__':
    sys.exit(main())
