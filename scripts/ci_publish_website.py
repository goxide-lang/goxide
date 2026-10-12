#!/usr/bin/env python3
"""Publish this successful main run's existing artifact to the existing Pages branch.

No builds, credential creation, force pushes, branch creation or Pages settings.
The workflow owns the CI dependency gate and the cross-run publication lock.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
from urllib.request import Request, urlopen


ROOT = Path(__file__).resolve().parents[1]
SOURCE = 'goxide-lang/goxide'
TARGET = 'https://github.com/goxide-lang/goxide-lang.github.io.git'
BRANCH = 'deploy/goxide-site-ae61ec6'
SITE = 'https://goxide-lang.github.io/'
ASSETS = frozenset(('index.html', '404.html', 'style.css', 'app.js', 'worker.js',
    'compiler.wasm', 'wasm_exec.js', 'examples.json', 'GO-LICENSE.txt', '_headers', '.nojekyll'))


def require(condition, message):
    if not condition:
        raise ValueError(message)


def git(root, *args, env=None):
    return subprocess.check_output(['git', '-c', 'credential.helper=', *args],
        cwd=root, env=env, text=True).strip()


def validate_artifact(dist, sha, root=ROOT):
    require(re.fullmatch('[0-9a-f]{40}', sha), 'invalid website source SHA')
    require({p.name for p in dist.iterdir()} == ASSETS, 'artifact differs from static asset allowlist')
    require(all(p.is_file() and not p.is_symlink() for p in dist.iterdir()), 'non-regular artifact file')
    manifest = json.loads((dist / 'examples.json').read_text())
    catalog = json.loads((root / 'website/catalog-source.json').read_text())
    require(manifest['websiteCommit'] == sha, 'artifact belongs to a different website SHA')
    require(manifest['commit'] == catalog['compilerCommit'] == manifest['catalog']['compilerCommit'],
        'artifact compiler pin differs from reviewed source')
    require(manifest['compilerDigest'] == catalog['compilerDigest'], 'artifact compiler digest differs')
    require(hashlib.sha256((dist / 'compiler.wasm').read_bytes()).hexdigest() == manifest['wasmSha256'],
        'artifact WASM hash differs')
    return manifest


def latest_main():
    request = Request(f'https://api.github.com/repos/{SOURCE}/git/ref/heads/main', headers={
        'Authorization': 'Bearer ' + os.environ['GH_TOKEN'],
        'Accept': 'application/vnd.github+json', 'X-GitHub-Api-Version': '2022-11-28',
    })
    # API denial is fatal; never fall back to a different identity or permission.
    with urlopen(request, timeout=30) as response:
        return json.load(response)['object']['sha']


def verify_pages_source():
    request = Request('https://api.github.com/repos/goxide-lang/goxide-lang.github.io/pages', headers={
        'Authorization': 'Bearer ' + os.environ['SITE_PUBLISH_TOKEN'],
        'Accept': 'application/vnd.github+json', 'X-GitHub-Api-Version': '2022-11-28',
    })
    with urlopen(request, timeout=30) as response:
        pages = json.load(response)
    require(pages.get('build_type') == 'legacy' and
        pages.get('source') == {'branch': BRANCH, 'path': '/'} and
        pages.get('html_url') == SITE, 'Pages source or URL changed; refusing to migrate or publish elsewhere')


def verify_forward(root, target, sha):
    previous = json.loads((target / 'examples.json').read_text()).get('websiteCommit', '')
    require(re.fullmatch('[0-9a-f]{40}', previous), 'deployed manifest lacks a valid website SHA')
    result = subprocess.run(['git', 'merge-base', '--is-ancestor', previous, sha], cwd=root,
        capture_output=True)
    require(result.returncode == 0,
        'refusing rollback or unrelated deployed source; reconcile the existing Pages branch explicitly')


def publish(dist, sha, root, target_url, branch, current_head, env, run_url):
    manifest = validate_artifact(dist, sha, root)
    if current_head() != sha:
        return dict(status='superseded', websiteCommit=sha)
    with tempfile.TemporaryDirectory(prefix='goxide-site-publish-') as directory:
        checkout = Path(directory) / 'site'
        git(root, 'clone', '--single-branch', '--branch', branch, target_url, str(checkout), env=env)
        verify_forward(root, checkout, sha)
        # Preserve repository metadata/documentation; reject unknown published files.
        require({p.name for p in checkout.iterdir()} <= ASSETS | {'.git', 'README.md'},
            'unexpected files in the Pages root; manual review required')
        for name in ASSETS:
            destination = checkout / name
            require(not destination.is_symlink(), 'symlink in Pages root: ' + name)
            shutil.copyfile(dist / name, destination)
        readme = checkout / 'README.md'
        require(not readme.is_symlink(), 'symlink README in Pages root')
        readme.write_text('# Goxide website and Playground\n\n'
            f'[Website]({SITE}) · [Build manifest]({SITE}examples.json)\n\n'
            f'Source: `{SOURCE}@{sha}`. Compiler: `{manifest["commit"]}`.\n\n'
            f'Validated CI run: {run_url}\n\n'
            f'Pages publishes the root of `{branch}`; the branch name is not a version.\n'
            'Generated static assets only. The manifest records exact compiler/std/serde pins, '
            'ABI, toolchain and WASM hash. The browser transpiles without executing generated Go.\n')
        git(checkout, 'add', '--', *sorted(ASSETS), 'README.md', env=env)
        changed = bool(git(checkout, 'diff', '--cached', '--name-only', env=env))
        if changed:
            git(checkout, '-c', 'user.name=github-actions[bot]',
                '-c', 'user.email=41898282+github-actions[bot]@users.noreply.github.com',
                'commit', '-m', f'Publish website from goxide main {sha}', env=env)
        # Latest-main check is deliberately adjacent to the only remote mutation.
        if current_head() != sha:
            return dict(status='superseded', websiteCommit=sha)
        if changed:
            # A concurrent writer causes a normal non-fast-forward failure. No retry/rebase/force.
            git(checkout, 'push', 'origin', f'HEAD:refs/heads/{branch}', env=env)
        commit = git(checkout, 'rev-parse', 'HEAD', env=env)
        return dict(status='published', websiteCommit=sha, deployCommit=commit,
            compilerCommit=manifest['commit'], wasmSha256=manifest['wasmSha256'])


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--dist', type=Path, required=True)
    args = parser.parse_args()
    require(os.environ.get('GITHUB_EVENT_NAME') == 'push' and
        os.environ.get('GITHUB_REF') == 'refs/heads/main' and
        os.environ.get('GITHUB_REPOSITORY') == SOURCE, 'publication requires this repository main push')
    sha = os.environ['GITHUB_SHA']
    require(git(ROOT, 'rev-parse', 'HEAD') == sha, 'checkout differs from successful main run')
    require(os.environ.get('SITE_PUBLISH_TOKEN'),
        'publication blocked: GOXIDE_SITE_PUBLISH_SECRET must name an EXISTING authorized Actions '
        'secret with Contents write and Pages read on goxide-lang/goxide-lang.github.io. GITHUB_TOKEN cannot '
        'publish across repositories or trigger branch Pages. No credential was created.')
    verify_pages_source()
    run_url = f'https://github.com/{SOURCE}/actions/runs/{os.environ["GITHUB_RUN_ID"]}'
    with tempfile.TemporaryDirectory(prefix='goxide-site-auth-') as directory:
        askpass = Path(directory) / 'askpass.py'
        askpass.write_text('#!/usr/bin/env python3\nimport os, sys\n'
            'print("x-access-token" if "Username" in sys.argv[1] else os.environ["SITE_PUBLISH_TOKEN"])\n')
        askpass.chmod(0o700)
        env = dict(os.environ, GIT_ASKPASS=str(askpass), GIT_TERMINAL_PROMPT='0')
        result = publish(args.dist.resolve(), sha, ROOT, TARGET, BRANCH, latest_main, env, run_url)
    print(json.dumps(result))
    with open(os.environ['GITHUB_OUTPUT'], 'a') as output:
        for name, value in result.items():
            print(f'{name}={value}', file=output)
    with open(os.environ['GITHUB_STEP_SUMMARY'], 'a') as output:
        print(f'Website publication: `{result["status"]}`; source `{sha}`.', file=output)
        if result['status'] == 'published':
            print(f'Published branch commit: `{result["deployCommit"]}`. '
                'Online asset/Worker verification follows; branch push alone is not deployment success.', file=output)


if __name__ == '__main__':
    main()
