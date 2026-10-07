"""Exercise the review artifact guard against real Git objects and history."""

import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock


SPEC = importlib.util.spec_from_file_location(
    "review_artifact_guard_under_test",
    Path(__file__).with_name("check_review_artifacts.py"),
)
GUARD = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = GUARD
SPEC.loader.exec_module(GUARD)


class ReviewArtifactGuardTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="goxide-review-guard-")
        self.addCleanup(temporary.cleanup)
        self.repo = Path(temporary.name) / "repo"
        self.repo.mkdir()
        self.git("init", "-q")
        self.write(".gitignore", "docs/reviews/**/ignored.log\n")
        self.legacy = "docs/reviews/legacy-evidence/full-race.log"
        self.write(self.legacy, "historical output\n" * 5000)
        self.write(
            "docs/reviews/legacy-evidence/old-run.json",
            json.dumps({"stdout": "historical stdout", "stderr": "historical stderr"}),
        )
        self.write("docs/reviews/baseline.md", "Existing short report.\n")
        self.git("add", ".")
        self.baseline = self.commit("fixture baseline")

    def git(self, *args):
        environment = {
            key: value
            for key, value in os.environ.items()
            if not key.startswith("GIT_")
        }
        command = [
            "git",
            "-c", "user.name=Review Guard Fixture",
            "-c", "user.email=review-guard@example.invalid",
            "-c", "commit.gpgsign=false",
            "-c", "core.autocrlf=false",
            "-c", "core.hooksPath=" + str(self.repo / "unused-hooks"),
            *args,
        ]
        result = subprocess.run(
            command, cwd=self.repo, env=environment, check=True,
            stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        )
        return result.stdout.decode("utf-8").strip()

    def write(self, relative, content):
        path = self.repo / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(content.encode("utf-8") if isinstance(content, str) else content)

    def stage(self, relative, content, force=False):
        self.write(relative, content)
        arguments = ["add", "-f"] if force else ["add"]
        self.git(*arguments, "--", relative)

    def commit(self, message):
        self.git("commit", "-q", "-m", message)
        return self.git("rev-parse", "HEAD")

    def check(self, commit=None, baseline=None):
        with mock.patch.object(GUARD, "BASELINE", baseline or self.baseline):
            return GUARD.check_repository(self.repo, commit=commit)

    def accepted(self, **kwargs):
        self.assertIsInstance(self.check(**kwargs), dict)

    def rejected(self, **kwargs):
        with self.assertRaises(GUARD.CheckFailure):
            self.check(**kwargs)

    def test_legacy_same_path_blob_and_mode_are_grandfathered(self):
        # The baseline includes oversized text and JSON carrying raw stdout.
        self.accepted()
        self.accepted(commit=self.baseline)

    def test_short_reports_summaries_scripts_and_source_fixtures_are_allowed(self):
        files = {
            "docs/reviews/new-report.md": "# Review\n\nTwo checks passed; raw output stays external.\n",
            "docs/reviews/new-evidence/summary.json": json.dumps({
                "exitCode": 0, "passed": 2, "failed": 0,
                "rawLogSHA256": "a" * 64, "rawLogBytes": 90000,
                "stdoutPath": "/tmp/full-run.stdout", "stderr": "",
            }),
            "docs/reviews/new-evidence/reproduce.py": "print('reproduce a small check')\n",
            "docs/reviews/new-evidence/testdata/input.go": "package fixture\nvar Answer = 42\n",
            "docs/reviews/new-evidence/testdata/input.gox": "package fixture\nenum Value { Ready, }\n",
            "docs/reviews/new-evidence/testdata/input.go.txt": "package fixture\nvar Answer = 42\n",
        }
        for path, contents in files.items():
            self.stage(path, contents)
        self.accepted()
        head = self.commit("small review material")
        self.accepted(commit=head)

    def test_scripts_and_tests_outside_reviews_are_not_filtered(self):
        self.stage("scripts/testdata/expected.log", "intentional test input\n")
        self.stage(
            "tests/testdata/compiler-output.json",
            json.dumps({"stdout": "x" * (70 * 1024)}),
        )
        self.accepted()

    def test_git_add_force_does_not_bypass_the_index_guard(self):
        path = "docs/reviews/new-evidence/ignored.log"
        self.stage(path, "forced raw output\n", force=True)
        self.assertIn(path, self.git("ls-files", "--", path))
        self.rejected()

    def test_force_added_text_in_raw_directories_is_rejected(self):
        ignored = (self.repo / ".gitignore").read_text()
        ignored += "".join(f"docs/reviews/**/{directory}/\n" for directory in ("raw", "logs", "qa-artifacts"))
        self.stage(".gitignore", ignored)
        for directory in ("raw", "logs", "qa-artifacts"):
            with self.subTest(directory=directory):
                path = f"docs/reviews/{directory}/nested/output.txt"
                self.write(path, "small UTF-8 raw output\n")
                self.assertEqual(self.git("check-ignore", "--", path), path)
                self.git("add", "-f", "--", path)
                try:
                    with self.assertRaisesRegex(GUARD.CheckFailure, "raw artifact directory"):
                        self.check()
                finally:
                    self.git("reset", "-q", "--", path)

    def test_raw_extensions_are_rejected_throughout_reviews(self):
        extensions = ("log", "stdout", "stderr", "jsonl", "png", "jpg", "gif", "webp", "zip", "tar", "gz")
        for extension in extensions:
            with self.subTest(extension=extension):
                path = "docs/reviews/arbitrary/testdata/capture." + extension
                self.stage(path, "small but raw\n")
                try:
                    self.rejected()
                finally:
                    self.git("reset", "-q", "--", path)

    def test_raw_json_payloads_are_rejected_even_in_small_nested_summaries(self):
        payloads = (
            {"stdout": "raw output"},
            {"stdout": ["raw output"]},
            {"stderr": {"text": "raw error"}},
            {"summary": {"results": [{"stderr": "raw error"}]}},
            {"wrapped": [{"stdout": " "}]},
            {"checks": [{"events": [{
                "Time": "2026-10-07T00:00:00Z", "Action": "output",
                "Package": "example.org/p", "Output": "=== RUN TestExample\n",
            }]}]},
            {"checks": [{"Time": "2026-10-07T00:00:00Z", "Action": "pass",
                         "Package": "example.org/p", "Elapsed": 0.01}]},
        )
        for index, payload in enumerate(payloads):
            with self.subTest(payload=payload):
                path = f"docs/reviews/new-evidence/testdata/summary-{index}.json"
                self.stage(path, json.dumps(payload))
                try:
                    self.rejected()
                finally:
                    self.git("reset", "-q", "--", path)

    def test_invalid_json_cannot_fall_back_to_plain_text(self):
        self.stage("docs/reviews/report.json", '{"status": "passed"}\n{"stdout": "raw"}\n')
        self.rejected()

    def test_oversized_json_is_rejected_despite_summary_name(self):
        self.stage("docs/reviews/new-evidence/summary.json", json.dumps({"description": "x" * (64 * 1024)}))
        self.rejected()

    def test_file_size_limit_is_in_bytes_and_includes_plain_reports(self):
        path = "docs/reviews/report.md"
        self.stage(path, b"x" * (64 * 1024))
        self.accepted()
        self.stage(path, b"x" * (64 * 1024 + 1))
        self.rejected()
        self.stage(path, "汉" * (22 * 1024))
        self.rejected()

    def test_snapshot_budget_counts_paths_even_when_the_blob_is_shared(self):
        for index in range(4):
            self.stage(f"docs/reviews/report-{index}.md", b"x" * (64 * 1024))
        self.accepted()
        self.stage("docs/reviews/baseline.md", "y")
        self.rejected()

    def test_separate_valid_commits_can_exceed_the_baseline_cumulative_budget(self):
        for index in range(3):
            self.stage(f"docs/reviews/batch-a-{index}.md", b"a" * (64 * 1024))
        self.accepted()
        self.commit("first permitted batch")
        for index in range(2):
            self.stage(f"docs/reviews/batch-b-{index}.md", b"b" * (64 * 1024))
        self.accepted()
        head = self.commit("second permitted batch")
        self.accepted(commit=head)
        self.accepted()

    def test_non_utf8_and_binary_content_are_rejected(self):
        for content in (b"not utf8: \xff", b"binary\x00payload"):
            with self.subTest(content=content):
                self.stage("docs/reviews/report.md", content)
                self.rejected()

    def test_staged_content_is_checked_not_worktree_content(self):
        path = "docs/reviews/new-evidence/summary.json"
        self.stage(path, '{"passed": 2}\n')
        self.write(path, '{"stdout": "unstaged raw text"}\n')
        self.accepted()
        self.git("add", "--", path)
        self.write(path, '{"passed": 2}\n')
        self.rejected()

    def test_legacy_raw_mutation_is_rejected(self):
        self.stage(self.legacy, "replacement output\n")
        self.rejected()

    def test_legacy_raw_deletion_is_rejected(self):
        self.git("rm", "-q", "--", self.legacy)
        self.rejected()

    def test_legacy_mode_change_is_not_grandfathered(self):
        self.git("update-index", "--chmod=+x", "--", self.legacy)
        self.rejected()

    def test_legacy_blob_copy_to_a_new_path_is_not_grandfathered(self):
        self.stage("docs/reviews/copied-evidence/full-race.log", (self.repo / self.legacy).read_bytes())
        self.rejected()

    def test_raw_commit_then_delete_is_rejected_from_history(self):
        path = "docs/reviews/transient-evidence/session.log"
        self.stage(path, "raw output committed before removal\n")
        bad = self.commit("add raw artifact")
        self.git("rm", "-q", "--", path)
        head = self.commit("remove raw artifact")
        self.assertEqual(self.git("rev-parse", f"{head}^{{tree}}"), self.git("rev-parse", f"{self.baseline}^{{tree}}"))
        self.rejected(commit=bad)
        self.rejected(commit=head)
        self.rejected()

    def test_ours_merge_cannot_hide_raw_artifacts_in_side_branch_history(self):
        main = self.git("symbolic-ref", "--short", "HEAD")
        self.git("checkout", "-q", "-b", "raw-side")
        path = "docs/reviews/side-evidence/session.log"
        self.stage(path, "raw output present only in side history\n")
        self.commit("side branch raw artifact")
        self.git("checkout", "-q", main)
        self.stage("docs/reviews/main-report.md", "A small review on the main branch.\n")
        self.commit("main branch report")
        self.git("merge", "-q", "--no-ff", "--strategy=ours", "-m", "retain only the main tree", "raw-side")
        head = self.git("rev-parse", "HEAD")
        self.assertEqual(self.git("ls-tree", "HEAD", "--", path), "")
        self.rejected(commit=head)
        self.rejected()

    def test_prebaseline_side_branch_report_preserves_its_own_history(self):
        main = self.git("symbolic-ref", "--short", "HEAD")
        fork = self.baseline
        self.stage(self.legacy, "raw version updated before the policy baseline\n")
        self.stage("docs/reviews/later-evidence/future.log", "future historical raw\n")
        self.baseline = self.commit("policy baseline after side branch fork")
        self.git("checkout", "-q", "-b", "old-side", fork)
        self.stage("docs/reviews/side-report.md", "A small report from an older branch.\n")
        self.commit("side report keeps its original inherited raw")
        self.git("checkout", "-q", main)
        self.git("merge", "-q", "--no-ff", "-m", "merge the older side report", "old-side")
        self.accepted(commit="HEAD")
        self.accepted()

    def test_prebaseline_side_branch_raw_add_then_delete_is_rejected(self):
        main = self.git("symbolic-ref", "--short", "HEAD")
        fork = self.baseline
        self.stage("docs/reviews/later-evidence/future.log", "future historical raw\n")
        self.baseline = self.commit("policy baseline after side branch fork")
        self.git("checkout", "-q", "-b", "old-raw-side", fork)
        path = "docs/reviews/side-evidence/new.log"
        self.stage(path, "new raw output on the old branch\n")
        raw = self.commit("side branch new raw")
        self.git("rm", "-q", "--", path)
        self.commit("remove side branch new raw")
        self.git("checkout", "-q", main)
        self.git("merge", "-q", "--no-ff", "--strategy=ours", "-m", "merge the old side history", "old-raw-side")
        for commit in (None, "HEAD"):
            with self.subTest(commit=commit):
                with self.assertRaisesRegex(GUARD.CheckFailure, "commit " + raw):
                    self.check(commit=commit)

    def test_merging_exact_baseline_raw_is_not_a_new_artifact_batch(self):
        main = self.git("symbolic-ref", "--short", "HEAD")
        fork = self.baseline
        self.stage("docs/reviews/later-evidence/large.log", b"historical output\n" * 20000)
        self.baseline = self.commit("policy baseline contains large historical raw")
        self.git("checkout", "-q", "-b", "old-side", fork)
        self.stage("docs/reviews/side-report.md", "A small report from an older branch.\n")
        self.commit("side report")
        self.git("merge", "-q", "--no-ff", "-m", "bring baseline into older branch", main)
        self.accepted(commit="HEAD")
        self.accepted()

    def test_prebaseline_side_branch_cannot_change_inherited_exact_frozen_raw(self):
        main = self.git("symbolic-ref", "--short", "HEAD")
        fork = self.baseline
        self.stage("docs/reviews/later-evidence/future.log", "future historical raw\n")
        self.baseline = self.commit("policy baseline after side branch fork")
        for operation in ("change", "delete"):
            with self.subTest(operation=operation):
                branch = "old-side-" + operation
                self.git("checkout", "-q", "-b", branch, fork)
                if operation == "change":
                    self.stage(self.legacy, "changed inherited raw\n")
                else:
                    self.git("rm", "-q", "--", self.legacy)
                changed = self.commit(operation + " inherited raw on the old branch")
                self.git("checkout", "-q", main)
                self.git("merge", "-q", "--no-ff", "--strategy=ours", "-m", "merge old side history", branch)
                for commit in (None, "HEAD"):
                    with self.assertRaisesRegex(GUARD.CheckFailure, "commit " + changed):
                        self.check(commit=commit)
                self.git("reset", "-q", "--hard", self.baseline)

    def test_explicit_commit_does_not_read_the_current_index(self):
        self.stage("docs/reviews/short.md", "Passed the focused check.\n")
        head = self.commit("safe review")
        self.stage("docs/reviews/new-evidence/unstable.log", "raw staged output\n")
        self.accepted(commit=head)
        self.rejected()

    def test_shallow_side_branch_is_rejected_even_with_reachable_baseline(self):
        main = self.git("symbolic-ref", "--short", "HEAD")
        self.git("checkout", "-q", "-b", "shallow-side")
        path = "docs/reviews/side-evidence/session.log"
        self.stage(path, "raw output hidden before the shallow boundary\n")
        raw = self.commit("side branch raw artifact")
        self.git("rm", "-q", "--", path)
        boundary = self.commit("remove side branch raw artifact")
        self.git("checkout", "-q", main)
        self.stage("docs/reviews/main-report.md", "A small review on the main branch.\n")
        self.commit("main branch report")
        self.git("merge", "-q", "--no-ff", "--strategy=ours", "-m", "retain only the main tree", "shallow-side")
        shallow_path = Path(self.git("rev-parse", "--git-path", "shallow"))
        if not shallow_path.is_absolute():
            shallow_path = self.repo / shallow_path
        shallow_path.write_text(boundary + "\n")
        self.assertEqual(self.git("rev-parse", "--is-shallow-repository"), "true")
        self.git("merge-base", "--is-ancestor", self.baseline, "HEAD")
        self.assertNotIn(raw, self.git("rev-list", "HEAD").splitlines())
        for commit in (None, "HEAD"):
            with self.subTest(commit=commit):
                with self.assertRaisesRegex(GUARD.CheckFailure, "shallow history.*no automatic fetch"):
                    self.check(commit=commit)

    def test_missing_baseline_fails_instead_of_disabling_the_guard(self):
        self.rejected(baseline="0" * 40)
        self.rejected(commit=self.baseline, baseline="0" * 40)

    def test_nonancestor_baseline_and_commit_are_rejected(self):
        tree = self.git("rev-parse", "HEAD^{tree}")
        unrelated = self.git("commit-tree", tree, "-m", "unrelated root")
        self.rejected(baseline=unrelated)
        self.rejected(commit=unrelated)

    def test_cli_does_not_expose_a_baseline_override(self):
        result = subprocess.run(
            [sys.executable, SPEC.origin, "--help"], cwd=self.repo,
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("--commit", result.stdout)
        self.assertNotIn("--base", result.stdout)


if __name__ == "__main__":
    unittest.main()
