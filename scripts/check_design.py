#!/usr/bin/env python3
"""Check design Markdown and existing Go lowering examples, not an hgo compiler."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import subprocess
import sys
import tempfile
import unicodedata
from urllib.parse import unquote


ROOT = Path(__file__).resolve().parents[1]
FIXTURES = ROOT / "tests" / "design"


class CheckFailure(Exception):
    pass


def read_markdown(path: Path) -> tuple[str, list[tuple[str, str]], set[str]]:
    """Return prose, fenced blocks, and GitHub-style heading anchors."""
    prose, blocks, code = [], [], []
    fence = None
    language = ""
    anchors: set[str] = set()
    repetitions: dict[str, int] = {}
    for number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        marker = re.match(r"^ {0,3}(`{3,}|~{3,})(.*)$", line)
        if fence:
            if marker and marker[1][0] == fence[0] and len(marker[1]) >= len(fence) and not marker[2].strip():
                blocks.append((language, "\n".join(code) + "\n"))
                fence, code = None, []
            else:
                code.append(line)
            continue
        if marker:
            fence = marker[1]
            language = marker[2].strip().split()[0] if marker[2].strip() else ""
            continue
        prose.append(line)
        heading = re.match(r"^ {0,3}#{1,6}\s+(.+?)(?:\s+#+)?$", line)
        if heading:
            text = heading[1].lower().replace("`", "")
            text = re.sub(r"\[([^]]+)\]\([^)]+\)", r"\1", text)
            slug = "".join(c for c in text if c in "-_ " or unicodedata.category(c)[0] in "LN").replace(" ", "-")
            count = repetitions.get(slug, 0)
            repetitions[slug] = count + 1
            anchors.add(slug + (f"-{count}" if count else ""))
    if fence:
        raise CheckFailure(f"{path.relative_to(ROOT)}: unclosed {fence} fence (at EOF)")
    return "\n".join(prose), blocks, anchors


def check_docs() -> dict[Path, tuple[str, list[tuple[str, str]], set[str]]]:
    files = sorted(p for p in ROOT.rglob("*.md") if ".git" not in p.relative_to(ROOT).parts)
    documents = {p.resolve(): read_markdown(p) for p in files}
    links = 0
    problems = []
    pattern = re.compile(r"!?\[[^]\n]*\]\((<[^>]+>|[^\s)]+)(?:\s+[^)]*)?\)")
    for path, (prose, _, _) in documents.items():
        prose = re.sub(r"(`+).*?\1", "", prose)
        for match in pattern.finditer(prose):
            target = match[1].removeprefix("<").removesuffix(">")
            if re.match(r"^[a-zA-Z][a-zA-Z0-9+.-]*:", target) or target.startswith("//"):
                continue
            local, _, anchor = target.partition("#")
            destination = (path.parent / unquote(local)).resolve() if local else path
            links += 1
            if not destination.exists():
                problems.append(f"{path.relative_to(ROOT)}: missing target {target}")
            elif anchor and destination.suffix.lower() == ".md":
                data = documents.get(destination) or read_markdown(destination)
                if unquote(anchor) not in data[2]:
                    problems.append(f"{path.relative_to(ROOT)}: missing anchor {target}")
    if problems:
        raise CheckFailure("\n".join(problems))
    print(f"PASS Markdown: {len(files)} files, {links} local links/anchors, balanced fences", flush=True)
    print("Scope: inline local Markdown links and ATX headings; external URLs are not fetched.", flush=True)
    return documents


def source_blocks(documents, name: str, package: str, count: int = 1) -> list[str]:
    path = (ROOT / "docs" / "design" / name).resolve()
    blocks = [code for language, code in documents[path][1]
              if language == "go" and re.search(rf"^package {re.escape(package)}\s*$", code, re.MULTILINE)]
    if len(blocks) != count:
        raise CheckFailure(f"{name}: expected {count} complete Go package {package} blocks, found {len(blocks)}")
    return blocks


def copy_fixture(name: str, destination: Path) -> None:
    shutil.copytree(FIXTURES / name, destination, dirs_exist_ok=True)
    for path in destination.rglob("go.mod.fixture"):
        path.rename(path.with_name("go.mod"))


def write_module(destination: Path, module: str) -> None:
    destination.mkdir(parents=True, exist_ok=True)
    (destination / "go.mod").write_text(f"module {module}\n\ngo 1.27\n", encoding="utf-8")


def tree_hash(directory: Path) -> str:
    digest = hashlib.sha256()
    for path in sorted(p for p in directory.rglob("*") if p.is_file()):
        digest.update(str(path.relative_to(directory)).encode())
        digest.update(path.read_bytes())
    return digest.hexdigest()


def check_go(documents) -> None:
    go = shutil.which("go")
    if not go:
        raise CheckFailure("Go >= 1.27 is required for full checks; --docs-only needs only Python 3.10+.")
    env = os.environ.copy()
    # Do not download a toolchain or inherit a parent workspace/build overlay.
    env.update(GOTOOLCHAIN="local", GOWORK="off", GOFLAGS="")
    info = subprocess.run([go, "env", "-json", "GOVERSION", "GOOS", "GOARCH", "GOROOT", "CGO_ENABLED"],
                          env=env, text=True, capture_output=True, check=True)
    environment = json.loads(info.stdout)
    version = re.search(r"go(\d+)\.(\d+)", environment["GOVERSION"])
    if not version or tuple(map(int, version.groups())) < (1, 27):
        raise CheckFailure(f"Need local Go >= 1.27; found {environment['GOVERSION']} (no toolchain auto-download).")
    print("Environment: " + json.dumps({"python": sys.version.split()[0], "go": go, **environment}, ensure_ascii=False), flush=True)
    print("Overrides: GOTOOLCHAIN=local GOWORK=off GOFLAGS= (isolated temporary modules).", flush=True)
    print("Scope: existing Go lowering/support mechanisms only; no hgo frontend or GOROOT overlay checks.", flush=True)
    workspace = Path(tempfile.mkdtemp(prefix="hgo-design-check-"))
    logs = workspace / "logs"
    logs.mkdir()
    print(f"Artifacts retained: {workspace}", flush=True)
    passed = []

    def run(label: str, cwd: Path, args: list[str], *, expected_failure: str | None = None, expected_output: str | None = None):
        print(f"\n[{label}] {shlex.join([go, *args])} (cwd={cwd})", flush=True)
        try:
            completed = subprocess.run([go, *args], cwd=cwd, env=env, text=True,
                                       stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=180)
        except subprocess.TimeoutExpired as error:
            partial = error.stdout or ""
            if isinstance(partial, bytes):
                partial = partial.decode("utf-8", errors="replace")
            (logs / f"{label}.log").write_text(partial + "\nTIMEOUT after 180 seconds\n", encoding="utf-8")
            raise CheckFailure(f"{label}: timed out after 180 seconds; partial output: {logs / (label + '.log')}") from error
        (logs / f"{label}.log").write_text(completed.stdout, encoding="utf-8")
        print(completed.stdout, end="" if completed.stdout.endswith("\n") else "\n", flush=True)
        if expected_failure:
            if completed.returncode == 0 or not re.search(expected_failure, completed.stdout):
                raise CheckFailure(f"{label}: expected compile rejection matching {expected_failure!r}; see {logs}")
            print(f"PASS expected compile rejection: {label}", flush=True)
        elif completed.returncode:
            suffix = " The lock suite requires race-detector support and a usable C compiler." if "-race" in args else ""
            raise CheckFailure(f"{label}: command failed ({completed.returncode}); see {logs}.{suffix}")
        elif expected_output is not None and completed.stdout.strip() != expected_output:
            raise CheckFailure(f"{label}: output differs from {expected_output!r}; see {logs}")
        passed.append(label)

    for fixture, doc, package, module, race in [
        ("lock", "synchronization.md", "hgosync", "example.org/hgosync-probe", True),
        ("std", "standard-library.md", "stdsupport", "example.org/hgo-stdlib-design", False),
        ("views", "traits.md", "views", "example.org/hgo-views-check", False),
    ]:
        target = workspace / fixture
        copy_fixture(fixture, target)
        write_module(target, module)
        (target / "example.go").write_text(source_blocks(documents, doc, package)[0], encoding="utf-8")
        run(fixture, target, ["test", *( ["-race"] if race else []), "-count=1", "-v", "./..."])

    enum_root = workspace / "enum"
    write_module(enum_root, "example.org/hgodesigncheck")
    copy_fixture("enum", enum_root / "enumdemo")
    (enum_root / "enumdemo" / "example.go").write_text(source_blocks(documents, "enums-and-match.md", "enumdemo")[0], encoding="utf-8")
    run("enum", enum_root, ["test", "-count=1", "-v", "./..."])
    for variant, diagnostic in [("option", "hgoOption"), ("result", "hgoResult")]:
        target = enum_root / f"invalid_{variant}"
        copy_fixture(f"enum_invalid/{variant}", target)
        write_module(target, f"example.org/invalid{variant}")
        with (target / "go.mod").open("a", encoding="utf-8") as stream:
            stream.write("\nrequire example.org/hgodesigncheck v0.0.0\nreplace example.org/hgodesigncheck => ..\n")
        run(f"enum-invalid-{variant}", target, ["test", "./..."], expected_failure=diagnostic)

    for index, (code, expected) in enumerate(zip(source_blocks(documents, "macros.md", "main", 2),
                                               ["85 1 41", "untitled 1 9 true 7"]), 1):
        target = workspace / f"macro{index}"
        write_module(target, f"example.org/hgo-macro-check{index}")
        (target / "main.go").write_text(code, encoding="utf-8")
        run(f"macro{index}", target, ["run", "."], expected_output=expected)

    target = workspace / "ptr"
    copy_fixture("ptr", target)
    write_module(target, "example.org/rawptrcheck")
    run("ptr", target, ["test", "-count=1", "-v", "./..."])
    target = workspace / "ptr-invalid"
    copy_fixture("ptr_invalid", target)
    write_module(target, "example.org/invalidreceiver")
    run("ptr-invalid", target, ["test", "./..."], expected_failure=r"invalid receiver type|receiver base type cannot be a pointer")

    target = workspace / "marker"
    copy_fixture("marker", target)
    original_before = tree_hash(target / "original-native")
    run("marker-plain-before", target / "plain-consumer", ["test", "-count=1", "-v", "./..."])
    run("marker-enhanced", target / "build" / "consumer", ["test", "-count=1", "-v", "./..."])
    run("marker-plain-after", target / "plain-consumer", ["test", "-count=1", "-v", "./..."])
    if tree_hash(target / "original-native") != original_before:
        raise CheckFailure("marker: original dependency files changed")
    print("PASS marker original dependency files unchanged", flush=True)
    summary = {"environment": environment, "passed_commands": passed,
               "scope": "Go lowering/support mechanisms only; no hgo compiler, linter, macros, or LSP tested.",
               "excluded": "Historical GOROOT overlay probe is not part of this portable suite."}
    (workspace / "summary.json").write_text(json.dumps(summary, indent=2) + "\n", encoding="utf-8")
    print(f"\nPASS {len(passed)} Go commands (including expected failures); artifacts: {workspace}", flush=True)
    print(summary["scope"], flush=True)
    print(summary["excluded"], flush=True)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--docs-only", action="store_true", help="check local Markdown links, anchors, and fences without Go")
    args = parser.parse_args()
    try:
        documents = check_docs()
        if not args.docs_only:
            check_go(documents)
    except (CheckFailure, OSError, subprocess.SubprocessError, ValueError) as error:
        print(f"FAIL: {error}", file=sys.stderr, flush=True)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
