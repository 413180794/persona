#!/usr/bin/env python3
"""Validate a committed checkout and build a single-platform Hub release."""
import argparse
import gzip
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile


def run(args, cwd, env=None):
    print('+', ' '.join(map(str, args)), flush=True)
    subprocess.run(args, cwd=cwd, env=env, check=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--out', type=Path, required=True)
    parser.add_argument('--build', required=True)
    parser.add_argument('--update-url', required=True)
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[2]
    out = args.out.resolve()
    out.mkdir(parents=True, exist_ok=True)
    sha = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=root, text=True).strip()
    if subprocess.check_output(['git', 'status', '--porcelain', '--untracked-files=no'], cwd=root).strip():
        raise RuntimeError('Only committed source may be released')
    run(['python3', 'scripts/privacy_check.py'], root)
    run(['go', 'mod', 'verify'], root / 'server')
    run(['go', 'vet', './...'], root / 'server')
    sys.path.insert(0, str(root / 'scripts/test'))
    from server_test_env import clean_environment
    test_env = clean_environment(os.environ)
    caches = json.loads(subprocess.check_output(
        ['go', 'env', '-json', 'GOCACHE', 'GOMODCACHE', 'GOPATH'], cwd=root / 'server'))
    test_env.update(caches)
    with tempfile.TemporaryDirectory(prefix='hub-release-tests-') as home:
        test_env.update(HOME=home, ZDOTDIR=home)
        run(['go', 'test', '-count=1', './...'], root / 'server', test_env)
        run(['go', 'test', '-race', '-count=1', './internal/hubedge', './internal/update',
             './internal/devices'], root / 'server', test_env)
    for suite in sorted((root / 'scripts').rglob('test_*.py')):
        if 'node_modules' not in suite.parts:
            run([sys.executable, '-B', '-m', 'unittest', '-v', suite.stem], suite.parent)
    run(['npm', 'test'], root / 'integrations/runtime-sdk')
    run(['npm', 'ci', '--no-audit', '--no-fund', '--registry', os.environ.get('PERSONA_NPM_REGISTRY', 'https://registry.npmjs.org/')], root / 'web')
    for script in ['typecheck', 'test', 'build']:
        run(['npm', 'run', script], root / 'web')
    version = f'master-{sha[:12]}-{args.build}'
    webdist = root / 'server/internal/hub/webdist'
    try:
        shutil.copytree(root / 'web/dist', webdist, dirs_exist_ok=True)
        run(['go', 'generate', './internal/runtimepkg'], root / 'server')
        flags = (f'-s -w -X main.version={version} -X main.channel=release '
                 f'-X main.build={args.build} -X main.commit={sha} -X main.updateURL={args.update_url}')
        bin_dir = out / 'bin'
        bin_dir.mkdir(exist_ok=True)
        binary = bin_dir / 'personad-linux-amd64'
        env = dict(os.environ, GOOS='linux', GOARCH='amd64', CGO_ENABLED='0')
        run(['go', 'build', '-tags', 'public', '-trimpath', '-ldflags', flags,
             '-o', str(binary), './cmd/personad'], root / 'server', env)
        run(['python3', 'scripts/release/check_bundle_privacy.py', '--whole-short', str(bin_dir)], root)
        with binary.open('rb') as src, gzip.open(out / 'personad-linux-amd64.gz', 'wb') as dest:
            shutil.copyfileobj(src, dest)
        run([str(binary), 'release', 'manifest', '--dir', str(out), '--version', version,
             '--build', args.build, '--commit', sha, '--out', str(out / 'manifest.json')], root)
        manifest = json.loads((out / 'manifest.json').read_text())
        artifact = manifest['files']['linux-amd64']
        name = f'personad-linux-amd64-{artifact["sha256"]}.gz'
        (out / artifact['name']).rename(out / name)
        artifact['name'] = name
        (out / 'manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
        os.replace(out / 'manifest.json', out / 'latest.json')
    finally:
        for child in webdist.iterdir():
            if child.name != 'README.md':
                shutil.rmtree(child) if child.is_dir() else child.unlink()


if __name__ == '__main__':
    main()
