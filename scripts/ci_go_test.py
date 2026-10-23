#!/usr/bin/env python3
"""Run go test quietly, preserving raw logs and reporting actual test outcomes."""

from collections import Counter
import json
import os
from pathlib import Path
import shlex
import subprocess
import sys
import tempfile
import time


def main():
    if len(sys.argv) < 3:
        sys.exit("usage: ci_go_test.py LABEL [go test FLAGS/PACKAGES...]")
    label, *arguments = sys.argv[1:]
    command = [os.environ.get("GO_BIN", "go"), "test", "-json", *arguments]
    environment = os.environ.copy()
    environment.pop("GOWORK", None)  # Workspace fixtures must discover their go.work.
    directory = Path(tempfile.mkdtemp(prefix="goxide-go-test-", dir=os.getenv("RUNNER_TEMP")))
    json_log, stderr_log = directory / "go-test.jsonl", directory / "stderr.log"
    outcomes = {"Top-level tests": Counter(), "Subtests": Counter()}
    packages, tested, active, failed_tests = {}, set(), set(), set()
    started = time.monotonic()
    with json_log.open("w", buffering=1) as log, stderr_log.open("w") as errors:
        try:
            process = subprocess.Popen(command, env=environment, stdout=subprocess.PIPE,
                                       stderr=errors, text=True, errors="replace")
        except OSError as error:
            errors.write(str(error) + "\n")
            returncode = 127
        else:
            for line in process.stdout:
                log.write(line)
                try:
                    event = json.loads(line)
                except ValueError:
                    continue
                package, test, action = event.get("Package"), event.get("Test"), event.get("Action")
                if not package:
                    continue
                packages.setdefault(package, "unfinished")
                if test:
                    tested.add(package)
                    key = (package, test)
                    if action == "run":
                        active.add(key)
                    elif action in ("pass", "fail", "skip"):
                        outcomes["Subtests" if "/" in test else "Top-level tests"][action] += 1
                        active.discard(key)
                        if action == "fail":
                            failed_tests.add(key)
                elif action in ("pass", "fail", "skip"):
                    packages[package] = action
            returncode = process.wait()
    elapsed = time.monotonic() - started
    for name, nested in (("Top-level tests", False), ("Subtests", True)):
        outcomes[name]["unfinished"] = sum(("/" in test) == nested for _, test in active)
    outcomes["Packages with observed tests"] = Counter(packages[p] for p in tested)
    outcomes["Packages without observed tests"] = Counter(v for p, v in packages.items() if p not in tested)
    rows = [f"### {label}", "", f"Command: `{shlex.join(command)}`", "",
            f"Exit code: **{returncode}**; wall time: **{elapsed:.2f}s**.", "",
            "| Level | Pass | Fail | Skip | Unfinished |",
            "| --- | ---: | ---: | ---: | ---: |"]
    for name, counts in outcomes.items():
        rows.append(f"| {name} | " + " | ".join(str(counts[key]) for key in
                    ("pass", "fail", "skip", "unfinished")) + " |")
    rows.extend(["", "Only observed outcomes are counted; package elapsed times are not summed.",
                 f"Raw logs: `{directory}`.", ""])
    summary = "\n".join(rows)
    print(summary, flush=True)
    if os.getenv("GITHUB_STEP_SUMMARY"):
        with open(os.environ["GITHUB_STEP_SUMMARY"], "a") as output:
            output.write(summary + "\n")
    if returncode:
        failed_packages = {p for p, status in packages.items() if status in ("fail", "unfinished")}
        test_fail_packages = {p for p, _ in failed_tests}
        print("Failure output:", flush=True)
        with json_log.open() as log:
            for line in log:
                try:
                    event = json.loads(line)
                except ValueError:
                    print(line, end="")
                    continue
                package, test = event.get("Package"), event.get("Test")
                if (event.get("Action") == "build-output" or not failed_packages or
                    (package in failed_packages and (not test or (package, test) in failed_tests
                                                     or package not in test_fail_packages))):
                    print(event.get("Output", ""), end="")
        print(stderr_log.read_text(), end="", flush=True)
    return returncode if returncode >= 0 else 128 - returncode


if __name__ == "__main__":
    sys.exit(main())
