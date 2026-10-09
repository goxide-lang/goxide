"""Build identity, toolchain checks and isolated browser-source helpers."""
from contextlib import contextmanager
import hashlib
import pathlib
import subprocess
import tempfile


def dependency_sources(packages, records, *, root, goroot, toolchain_version,
                       compiler_commit, exports):
    """Snapshot the fixed direct imports selected for the native export build.

    ``records`` must be the very same ``go list -export -deps -json`` output
    used for the embedded export archives. Publish complete selected GoFiles
    and CgoFiles, not tests, inactive target files, or the transitive closure.
    Paths identify the original package/distribution; no build-machine paths
    or module-cache paths are exposed. This helper never fetches or builds.
    """
    root = pathlib.Path(root).resolve(strict=True)
    goroot = pathlib.Path(goroot).resolve(strict=True)
    exports = pathlib.Path(exports)
    by_package = {record['ImportPath']: record for record in records}
    if len(set(packages)) != len(packages):
        raise ValueError('duplicate direct dependency package')

    def source_file(base, path, prefix):
        # Both lexical and resolved checks matter: neither traversal names nor
        # symlinks may turn the explicit source allowlist into a tree export.
        relative = pathlib.PurePosixPath(path)
        if relative.is_absolute() or '..' in relative.parts:
            raise ValueError(f'dependency source escapes its root: {path}')
        selected = base / relative
        if not selected.resolve(strict=True).is_relative_to(base):
            raise ValueError(f'dependency source escapes its root: {path}')
        content = selected.read_bytes()
        return dict(path=prefix + relative.as_posix(), source=content.decode('utf-8'),
                    sha256=hashlib.sha256(content).hexdigest())

    dependencies = []
    for package in packages:
        selected = by_package.get(package)
        if not selected or not selected.get('Export'):
            raise ValueError(f'dependency export record missing: {package}')
        directory = pathlib.Path(selected['Dir']).resolve(strict=True)
        module = selected.get('Module', {})
        if module.get('Replace'):
            raise ValueError(f'dependency replacements are not publishable: {package}')
        if selected.get('Standard') and selected.get('Goroot'):
            base, prefix = goroot, 'GOROOT/'
            version = toolchain_version
            origin = f'Go distribution {version}'
            license_bytes = go_license_path(goroot).read_bytes()
            licenses = [dict(path='Go distribution/LICENSE', source=license_bytes.decode('utf-8'),
                             sha256=hashlib.sha256(license_bytes).hexdigest())]
            identity = dict(kind='toolchain')
        elif module.get('Main'):
            base = pathlib.Path(module['Dir']).resolve(strict=True)
            if base != root:
                raise ValueError(f'dependency uses an unexpected main module: {package}')
            prefix = module['Path'] + '/'
            version = compiler_commit
            origin = module['Path'] + '@' + version
            licenses = [source_file(base, 'LICENSE', prefix)]
            identity = dict(kind='compiler', module=module['Path'])
        else:
            if not all(module.get(key) for key in ('Path', 'Dir', 'Version', 'Sum', 'GoModSum')):
                raise ValueError(f'dependency has no verified module identity: {package}')
            base = pathlib.Path(module['Dir']).resolve(strict=True)
            prefix = module['Path'] + '/'
            version = module['Version']
            origin = module['Path'] + '@' + version
            licenses = [source_file(base, 'LICENSE', prefix)]
            identity = dict(kind='module', module=module['Path'], sum=module['Sum'],
                            goModSum=module['GoModSum'])
        if not directory.is_relative_to(base):
            raise ValueError(f'dependency package escapes its source root: {package}')
        names = [*selected.get('GoFiles', []), *selected.get('CgoFiles', [])]
        if not names or len(names) != len(set(names)):
            raise ValueError(f'dependency has an invalid selected Go file inventory: {package}')
        files = [source_file(base, (directory.relative_to(base) / name).as_posix(), prefix)
                 for name in sorted(names)]
        archive = exports / (package.replace('/', '_') + '.a')
        dependencies.append(dict(package=package, version=version, source=origin,
                                 files=files, licenses=licenses,
                                 exportSha256=hashlib.sha256(archive.read_bytes()).hexdigest(),
                                 **identity))
    return dependencies


@contextmanager
def standalone_playground(source):
    """Copy exact browser input bytes to a temporary file with no module owner.

    A native file beneath go.mod selects configured recovery; the WASM input has
    no such owner. Resolve TMPDIR first so symlinks cannot hide a module ancestor.
    The caller still executes the real CLI and checks its unmodified output.
    """
    temporary_root = pathlib.Path(tempfile.gettempdir()).resolve(strict=True)
    for ancestor in (temporary_root, *temporary_root.parents):
        module = ancestor / 'go.mod'
        try:
            module.lstat()
        except FileNotFoundError:
            continue
        raise ValueError('browser CLI inputs require a module-free temporary directory; '
                         'found ' + str(module))
    with tempfile.TemporaryDirectory(prefix='goxide-web-playground-', dir=temporary_root) as directory:
        source_path = pathlib.Path(directory) / 'playground.gox'
        source_path.write_bytes(source)
        yield source_path


def is_compiler_source(name):
    path = pathlib.PurePosixPath(name)
    if name in {"go.mod", "go.sum"}:
        return True
    if path.suffix != ".go" or path.name.endswith("_test.go"):
        return False
    return (path.parts[0] in {"internal", "runtime", "formatting"} or
            path.parts[:2] in {("cmd", "goxide"), ("cmd", "goxide-playground")})


def verify_compiler_sources(root, pin, expected_digest):
    """Require identical pinned/current file sets, bytes, and reviewed digest.

    The pin must already exist in the local repository. This function never
    fetches history, downloads dependencies, or accepts an unverified fallback.
    """
    root = pathlib.Path(root)
    try:
        tree = subprocess.check_output(
            ["git", "ls-tree", "-r", "--name-only", "-z", pin], cwd=root)
    except subprocess.CalledProcessError as error:
        raise ValueError(f"compiler pin {pin} is unavailable; use a checkout with its history") from error
    pinned = {name for name in tree.decode().split("\0") if name and is_compiler_source(name)}
    current = {
        path.relative_to(root).as_posix()
        for pattern in ("internal/**/*.go", "cmd/goxide/**/*.go", "cmd/goxide-playground/**/*.go",
                        "runtime/**/*.go", "formatting/**/*.go", "go.mod", "go.sum")
        for path in root.glob(pattern)
        if path.is_file() and is_compiler_source(path.relative_to(root).as_posix())
    }
    if current != pinned:
        raise ValueError(f"compiler inventory differs from pin; missing={sorted(pinned-current)}, added={sorted(current-pinned)}")
    digest = hashlib.sha256()
    # Preserve the original reviewed inventory's pathlib (component) ordering.
    for name in sorted(pinned, key=pathlib.PurePosixPath):
        actual = (root / name).read_bytes()
        expected = subprocess.check_output(["git", "show", f"{pin}:{name}"], cwd=root)
        if actual != expected:
            raise ValueError(f"compiler source differs from pin: {name}")
        digest.update(name.encode())
        digest.update(actual)
    result = digest.hexdigest()
    if result != expected_digest:
        raise ValueError(f"compiler digest differs from reviewed release: {result}")
    return result


def go_license_path(goroot):
    """Use only the selected Go distribution's standard or Homebrew layout."""
    root = pathlib.Path(goroot).resolve()
    candidates = [root / "LICENSE"]
    if root.name == "libexec":
        candidates.append(root.parent / "LICENSE")
    for path in candidates:
        if path.is_file():
            return path
    raise FileNotFoundError(
        "Go LICENSE was not found in the selected toolchain; checked " +
        ", ".join(str(path) for path in candidates))
