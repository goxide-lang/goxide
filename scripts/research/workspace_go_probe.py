#!/usr/bin/env python3
"""Isolated native Go mechanism probes for proposed RFC 0026, not hgo tests."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--go', required=True, help='absolute path to the real Go executable')
    args = parser.parse_args()
    go = str(Path(args.go).resolve(strict=True))
    root = Path(tempfile.mkdtemp(prefix='goxide-workspace-research-'))
    env = dict(os.environ, GOTOOLCHAIN='local', GOPROXY='off', GOSUMDB='off',
               GONOPROXY='none', GOVCS='*:off', GOWORK='off', GOFLAGS='-buildvcs=false',
               GOMODCACHE=str(root / 'empty-modcache'), GOPATH=str(root / 'gopath'))
    results = []

    def write(path, content):
        dest = root / path
        dest.parent.mkdir(parents=True, exist_ok=True)
        dest.write_text(content)

    def run(label, cwd, argv, work='off', expected=None, error=None):
        result = subprocess.run([go, *argv], cwd=root / cwd,
                                env=dict(env, GOWORK=work), text=True,
                                stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        output = result.stdout.strip()
        if error is None:
            assert result.returncode == 0, (label, output)
            if expected is not None:
                assert expected in output, (label, expected, output)
        else:
            assert result.returncode != 0 and error in output, (label, error, output)
        results.append({'id': label, 'args': argv, 'work': work.replace(str(root), '<fixture>'),
                        'exit': result.returncode, 'output': output.replace(str(root), '<fixture>')})
        return output

    version = run('version', '.', ['version'])
    for directory, value in [('d1', 'one'), ('d2', 'two')]:
        write(f'{directory}/go.mod', 'module example.org/dep\n\ngo 1.27\n')
        write(f'{directory}/dep.go', f'package dep\nfunc Value() string {{ return "{value}" }}\n')
    replacements = ('replace example.org/dep v1.0.0 => ../d1\n'
                    'replace example.org/dep v1.1.0 => ../d2\n')
    for member, version_req in [('a', 'v1.0.0'), ('b', 'v1.1.0')]:
        write(f'{member}/go.mod', f'module example.org/{member}\n\ngo 1.27\n'
              f'require example.org/dep {version_req}\n{replacements}')
        write(f'{member}/main.go', 'package main\nimport ("fmt"; "example.org/dep")\n'
              'func main() { fmt.Println(dep.Value()) }\n')
    write('go.work', 'go 1.27\nuse (\n./a\n./b\n)\n')
    work = str(root / 'go.work')
    run('GO-01-standalone-A', 'a', ['run', '.'], expected='one')
    run('GO-02-all-main-A', 'a', ['run', '.'], work=work, expected='two')
    run('GO-03-all-main-version', 'a', ['list', '-m', 'example.org/dep'], work=work,
        expected='example.org/dep v1.1.0')
    write('selected.work', 'go 1.27\nuse ./a\n')
    run('GO-04-selected-main-A', 'a', ['run', '.'], work=str(root / 'selected.work'), expected='one')
    # Deliberately create two conflicting wildcard replacements, then resolve at workspace root.
    for member, dep in [('a', 'd1'), ('b', 'd2')]:
        write(f'{member}/go.mod', f'module example.org/{member}\n\ngo 1.27\n'
              f'require example.org/dep v1.0.0\nreplace example.org/dep => ../{dep}\n')
    run('GO-05-replace-conflict', 'a', ['list', '-m', 'all'], work=work,
        error='conflicting replacements')
    write('go.work', 'go 1.27\nuse (\n./a\n./b\n)\nreplace example.org/dep => ./d2\n')
    run('GO-06-root-override', 'a', ['run', '.'], work=work, expected='two')
    # Native vendor generation is used only to manufacture this local research fixture.
    run('GO-07-create-work-vendor', '.', ['work', 'vendor'], work=work)
    modules = (root / 'vendor/modules.txt').read_text()
    assert '## workspace' in modules, modules
    for dep in ['d1', 'd2']:
        (root / dep).rename(root / f'{dep}-unavailable')
    run('GO-08-cold-work-vendor', 'a', ['run', '-mod=vendor', '.'], work=work, expected='two')
    run('GO-09-work-vendor-not-module-vendor', 'a', ['run', '-mod=vendor', '.'],
        error='inconsistent vendoring')
    saved = (root / 'vendor/example.org/dep/dep.go').read_text()
    (root / 'vendor/example.org/dep/dep.go').unlink()
    run('GO-10-work-vendor-missing-package', 'a', ['run', '-mod=vendor', '.'], work=work,
        error='cannot find module providing package example.org/dep')
    write('vendor/example.org/dep/dep.go', saved)
    write('policy.work', 'go 1.27\ntoolchain go1.27.1\ngodebug panicnil=1\nuse ./a\n')
    policy = run('GO-11-policy-json', '.', ['work', 'edit', '-json', 'policy.work'])
    data = json.loads(policy)
    assert data['Go'] == '1.27' and data['Toolchain'] == 'go1.27.1'
    assert data['Godebug'] == [{'Key': 'panicnil', 'Value': '1'}]
    write('future/go.mod', 'module example.org/future\n\ngo 1.99\n')
    write('future/future.go', 'package future\n')
    write('future.work', 'go 1.27\nuse (\n./a\n./future\n)\n')
    run('GO-12-unselected-main-go-version', 'a', ['list', '-m'],
        work=str(root / 'future.work'), error='requires go >= 1.99')
    # Runtime defaults: workspace policy shadows member policy, without rewriting it.
    write('policy-member/go.mod', 'module example.org/policy\n\ngo 1.27\ngodebug panicnil=1\n')
    write('policy-member/main.go', 'package main\nfunc main() {}\n')
    write('runtime.work', 'go 1.27\ngodebug panicnil=0\nuse ./policy-member\n')
    run('GO-13-member-godebug', 'policy-member', ['list', '-mod=readonly', '-f', '{{.DefaultGODEBUG}}', '.'],
        expected='panicnil=1')
    defaults = run('GO-14-work-godebug', 'policy-member', ['list', '-mod=readonly', '-f', '{{.DefaultGODEBUG}}', '.'],
                   work=str(root / 'runtime.work'))
    assert 'panicnil=1' not in defaults, defaults
    run('GO-15-member-policy-restored', 'policy-member', ['list', '-mod=readonly', '-f', '{{.DefaultGODEBUG}}', '.'],
        expected='panicnil=1')
    # A native work member is importable even without a require edge from the caller.
    write('no-require/a/go.mod', 'module example.org/no-require-a\n\ngo 1.27\n')
    write('no-require/a/main.go', 'package main\nimport ("fmt"; "example.org/no-require-b")\n'
          'func main() { fmt.Println(b.Message()) }\n')
    write('no-require/b/go.mod', 'module example.org/no-require-b\n\ngo 1.27\n')
    write('no-require/b/b.go', 'package b\nfunc Message() string { return "member import" }\n')
    write('no-require/go.work', 'go 1.27\nuse (\n./a\n./b\n)\n')
    run('GO-16-member-import-without-require', 'no-require/a', ['run', '.'],
        work=str(root / 'no-require/go.work'), expected='member import')
    run('GO-17-standalone-missing-require', 'no-require/a', ['run', '-mod=readonly', '.'],
        error='cannot find module providing package example.org/no-require-b')
    # A longer module prefix is only a candidate, not necessarily the package owner.
    write('prefix/outer/go.mod', 'module example.org/p\n\ngo 1.27\n')
    write('prefix/outer/sub/c/c.go', 'package c\nfunc Value() string { return "outer" }\n')
    write('prefix/inner/go.mod', 'module example.org/p/sub\n\ngo 1.27\n')
    write('prefix/app/go.mod', 'module example.org/prefix-app\n\ngo 1.27\n')
    write('prefix/app/main.go', 'package main\nimport ("fmt"; "example.org/p/sub/c")\n'
          'func main() { fmt.Println(c.Value()) }\n')
    write('prefix/go.work', 'go 1.27\nuse (\n./outer\n./inner\n./app\n)\n')
    run('GO-18-prefix-not-owner', 'prefix/app', ['run', '.'],
        work=str(root / 'prefix/go.work'), expected='outer')
    write('prefix/inner/c/c.go', 'package c\nfunc Value() string { return "inner" }\n')
    run('GO-19-ambiguous-package-owner', 'prefix/app', ['run', '.'],
        work=str(root / 'prefix/go.work'), error='ambiguous import')
    record = {'scope': 'Native Go only; no hgo workspace implementation', 'go': version,
              'network': 'GOPROXY=off GOSUMDB=off GOVCS=*:off; local replacements only',
              'modules_txt': modules, 'checks': results}
    (root / 'results.json').write_text(json.dumps(record, indent=2) + '\n')
    digest = hashlib.sha256((root / 'results.json').read_bytes()).hexdigest()
    print(json.dumps({'passed': len(results) - 1, 'go': version,
                      'results': str(root / 'results.json'), 'sha256': digest}, indent=2))


if __name__ == '__main__':
    main()
