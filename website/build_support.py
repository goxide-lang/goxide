"""Build identity, toolchain checks and isolated browser-source helpers."""
from contextlib import contextmanager
import hashlib
import pathlib
import subprocess
import tempfile


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
