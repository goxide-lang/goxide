#!/usr/bin/env python3
"""Keep raw run artifacts out of review commits, including force-added files.

Run without arguments before committing to check the index and HEAD history.
Use --commit REV to check a committed tree and its history. This requires the
fixed baseline and complete ancestry in a Git checkout; it never fetches.
Historical raw artifacts are grandfathered only at their exact path/blob/mode.
Short reports, compact JSON summaries, and small source fixtures remain allowed.
This is a bounded artifact policy, not detection of deliberately disguised data.
"""

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path, PurePosixPath
import subprocess
import sys


ROOT = Path(__file__).resolve().parents[1]
# An immutable policy anchor, not HEAD, a moving merge-base, or a CLI option.
BASELINE = "8ba3174bd43481c396e75be5a95aa8e622456271"
MAX_FILE_BYTES = 64 * 1024
MAX_CHANGED_BYTES = 256 * 1024
RAW_DIRECTORIES = {"raw", "logs", "qa-artifacts"}
RAW_SUFFIXES = {
    ".log", ".stdout", ".stderr", ".jsonl", ".ndjson", ".har", ".trace",
    ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".tif", ".tiff",
    ".mp4", ".mov", ".webm", ".zip", ".gz", ".tgz", ".tar", ".xz",
    ".bz2", ".7z", ".zst", ".wasm", ".exe", ".dll", ".dylib",
}


class CheckFailure(Exception):
    pass


class Repository:
    def __init__(self, root: Path):
        self.root = root
        self.sizes: dict[str, int] = {}
        self.reasons: dict[tuple[str, str, str], str | None] = {}
        self.trees: dict[str, dict[str, tuple[str, str]]] = {}

    def git(self, *args: str, allow_failure: bool = False) -> subprocess.CompletedProcess:
        environment = os.environ.copy()
        environment["GIT_NO_REPLACE_OBJECTS"] = "1"
        environment["GIT_NO_LAZY_FETCH"] = "1"
        result = subprocess.run(["git", "-C", str(self.root), *args],
                                env=environment, capture_output=True)
        if result.returncode and not allow_failure:
            raise CheckFailure(f"git {' '.join(args)}: {result.stderr.decode(errors='replace').strip()}")
        return result

    def tree(self, commit: str) -> dict[str, tuple[str, str]]:
        if commit in self.trees:
            return self.trees[commit]
        entries = {}
        for item in self.git("ls-tree", "-r", "-z", commit, "--", "docs/reviews").stdout.split(b"\0"):
            if item:
                metadata, path = item.split(b"\t", 1)
                mode, _, blob = metadata.decode("ascii").split()
                entries[os.fsdecode(path)] = (mode, blob)
        self.trees[commit] = entries
        return entries

    def first_parent_tree(self, commit: str) -> dict[str, tuple[str, str]]:
        parents = self.git("rev-list", "--parents", "-n", "1", commit).stdout.decode().split()
        return self.tree(parents[1]) if len(parents) > 1 else {}

    def index(self) -> dict[str, tuple[str, str]]:
        entries = {}
        for item in self.git("ls-files", "--stage", "-z", "--", "docs/reviews").stdout.split(b"\0"):
            if item:
                metadata, path = item.split(b"\t", 1)
                mode, blob, stage = metadata.decode("ascii").split()
                name = os.fsdecode(path)
                if stage != "0":
                    raise CheckFailure(f"unmerged review index entry: {name!r}")
                entries[name] = (mode, blob)
        return entries

    def size(self, blob: str) -> int:
        if blob not in self.sizes:
            self.sizes[blob] = int(self.git("cat-file", "-s", blob).stdout)
        return self.sizes[blob]

    def reason(self, path: str, entry: tuple[str, str]) -> str | None:
        mode, blob = entry
        key = (path, mode, blob)
        if key in self.reasons:
            return self.reasons[key]
        reason = self.classify(path, mode, blob)
        self.reasons[key] = reason
        return reason

    def classify(self, path: str, mode: str, blob: str) -> str | None:
        if mode not in {"100644", "100755"}:
            return "review artifacts must be regular files, not symlinks/submodules"
        parts = PurePosixPath(path)
        for directory in parts.parts[2:-1]:
            if directory.lower() in RAW_DIRECTORIES:
                return f"raw artifact directory {directory!r} belongs in external storage"
        suffix = parts.suffix.lower()
        if suffix in RAW_SUFFIXES:
            return f"raw run/screenshot/archive suffix {suffix} belongs in external storage"
        size = self.size(blob)
        if size > MAX_FILE_BYTES:
            return f"{size} bytes exceeds the {MAX_FILE_BYTES}-byte review-file limit"
        data = self.git("cat-file", "blob", blob).stdout
        try:
            text = data.decode("utf-8")
        except UnicodeDecodeError:
            return "binary/non-UTF-8 review artifact belongs in external storage"
        if "\0" in text:
            return "binary review artifact belongs in external storage"
        if suffix == ".json":
            try:
                value = json.loads(text)
            except (ValueError, RecursionError):
                return "review JSON must be one valid compact summary, not a raw stream"
            pending = [value]
            while pending:
                item = pending.pop()
                if isinstance(item, dict):
                    keys = {key.lower() for key in item}
                    if {"time", "action", "package"} <= keys:
                        return "raw Go test event records belong in external storage"
                    for key, value in item.items():
                        if key.lower() in {"stdout", "stderr"} and value:
                            return f"embedded raw {key} belongs in external storage; record a path/hash or counts instead"
                        pending.append(value)
                elif isinstance(item, list):
                    pending.extend(item)
        return None


def check_repository(root: Path, commit: str | None = None) -> dict:
    repo = Repository(root)
    if repo.git("rev-parse", "--is-shallow-repository").stdout.strip() == b"true":
        raise CheckFailure("shallow history is unsupported; use a checkout with complete history (no automatic fetch)")
    anchor = repo.git("rev-parse", "--verify", BASELINE + "^{commit}", allow_failure=True)
    if anchor.returncode:
        raise CheckFailure(f"fixed baseline {BASELINE} is unavailable; use a checkout with complete history (no automatic fetch)")
    target = repo.git("rev-parse", "--verify", "--end-of-options", (commit or "HEAD") + "^{commit}").stdout.decode().strip()
    if repo.git("merge-base", "--is-ancestor", BASELINE, target, allow_failure=True).returncode:
        raise CheckFailure(f"fixed baseline {BASELINE} is not a proven ancestor of {target}; shallow/incomplete or unrelated history is unsupported")
    baseline = repo.tree(BASELINE)
    # Only raw baseline entries are frozen. Existing concise reports and source
    # fixtures may still evolve under the same size/content rules as new ones.
    frozen = {path: entry for path, entry in baseline.items() if repo.reason(path, entry)}

    def inspect(entries: dict[str, tuple[str, str]], previous: dict[str, tuple[str, str]], label: str,
                contains_baseline: bool = True) -> None:
        problems = []
        for path, entry in frozen.items():
            if (contains_baseline or previous.get(path) == entry) and entries.get(path) != entry:
                problems.append(f"{path!r}: historical raw artifact must retain its baseline path/blob/mode")
        changed_bytes = 0
        for path, entry in entries.items():
            changed = previous.get(path) != entry
            if changed and frozen.get(path) != entry and entry[0] in {"100644", "100755"}:
                changed_bytes += repo.size(entry[1])
            if not contains_baseline and not changed:
                continue
            if baseline.get(path) == entry:
                continue
            reason = repo.reason(path, entry)
            if reason:
                problems.append(f"{path!r}: {reason}")
        if changed_bytes > MAX_CHANGED_BYTES:
            problems.append(f"new/changed review files total {changed_bytes} bytes exceeds {MAX_CHANGED_BYTES} bytes")
        if problems:
            raise CheckFailure(label + ":\n" + "\n".join(problems))

    # --full-history retains branch commits even if a later merge or deletion
    # removes their artifacts. Checking only target/index would miss pushed blobs.
    commits = repo.git("rev-list", "--reverse", "--topo-order", "--full-history",
                       BASELINE + ".." + target, "--", "docs/reviews").stdout.decode().splitlines()
    for revision in commits:
        # The batch budget is per commit relative to its first parent, not a
        # lifetime limit on legitimate reports. All side-branch commits still
        # undergo raw-content checks through the full-history traversal above.
        # A branch forked before the policy baseline need not contain future
        # frozen files or rewrite inherited raw versions. Inspect its changes.
        ancestry = repo.git("merge-base", "--is-ancestor", BASELINE, revision, allow_failure=True)
        if ancestry.returncode not in (0, 1):
            raise CheckFailure(f"cannot inspect ancestry of {revision}: {ancestry.stderr.decode(errors='replace').strip()}")
        contains_baseline = ancestry.returncode == 0
        inspect(repo.tree(revision), repo.first_parent_tree(revision), "commit " + revision, contains_baseline)
    target_tree = repo.tree(target)
    if commit is None:
        inspect(repo.index(), target_tree, "index (HEAD " + target + ")")
    elif target not in commits:
        inspect(target_tree, target_tree, "commit " + target)
    return {"baseline": BASELINE, "target": target, "mode": "commit" if commit is not None else "index",
            "reviewCommits": len(commits), "checkedVersions": len(repo.reasons), "historicalRawFiles": len(frozen)}


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--commit", metavar="REV", help="check this commit and its history instead of index plus HEAD history")
    args = parser.parse_args(argv)
    try:
        result = check_repository(ROOT, args.commit)
    except (CheckFailure, OSError) as error:
        print(f"FAIL review artifacts: {error}", file=sys.stderr)
        return 1
    print("PASS review artifacts: " + json.dumps(result, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
