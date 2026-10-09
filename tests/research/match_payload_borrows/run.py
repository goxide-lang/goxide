#!/usr/bin/env python3
"""Replay pure Go representation probes; never invoke the Goxide frontend."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
from datetime import datetime, timezone


HERE = Path(__file__).resolve().parent
CHECKOUT = HERE.parents[2]
GO_VERSION = "go1.27.1"


def json_text(value: object) -> str:
    return json.dumps(value, ensure_ascii=False, indent=2) + "\n"


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--go", default=os.environ.get("GO_BIN"), help="Go binary; defaults to GO_BIN, never the system go")
    parser.add_argument("--output-dir", type=Path, help="New or empty directory outside the checkout; defaults to a retained temporary directory")
    parser.add_argument("--timeout", type=float, default=180, help="Timeout in seconds for each command (default: 180)")
    args = parser.parse_args()
    if args.timeout <= 0:
        parser.error("--timeout must be positive")
    output = (args.output_dir or Path(tempfile.mkdtemp(prefix="goxide-match-borrow-evidence-"))).resolve()
    if output == CHECKOUT or CHECKOUT in output.parents:
        parser.error("--output-dir must be outside the checkout")
    if output.exists() and (not output.is_dir() or any(output.iterdir())):
        parser.error("--output-dir must be new or empty; previous evidence is never overwritten")
    output.mkdir(parents=True, exist_ok=True)

    env_changes = {
        "GOTOOLCHAIN": "local",
        "GOWORK": "off",
        "GOENV": "off",
        "GOFLAGS": "",
        "GOPROXY": "off",
        "GOSUMDB": "off",
    }
    env = os.environ.copy()
    env.update(env_changes)
    fixture_files = sorted(HERE.glob("*.fixture"))
    manifest = {
        "schema": 1,
        "started_utc": datetime.now(timezone.utc).isoformat(),
        "scope": "pure Go representation research; unapproved proposal; no frontend acceptance",
        "runner_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
        "required_go_version": GO_VERSION,
        "environment_overrides": env_changes,
        "inherited_go_environment": {key: env.get(key) for key in ("GOROOT", "CGO_ENABLED", "GOOS", "GOARCH", "GOMAXPROCS", "GOCACHE", "GOMODCACHE")},
        "fixtures": {file.name: hashlib.sha256(file.read_bytes()).hexdigest() for file in fixture_files},
        "commands": [],
    }
    summary: dict = {"status": "FAIL", "scope": manifest["scope"], "output_dir": str(output)}

    def save() -> None:
        (output / "manifest.json").write_text(json_text(manifest), encoding="utf-8")
        (output / "summary.json").write_text(json_text(summary), encoding="utf-8")

    def execute(argv: list[str], cwd: Path, stdout_name: str, stderr_name: str) -> tuple[int | None, bytes]:
        record = {"argv": argv, "cwd": str(cwd), "stdout": stdout_name, "stderr": stderr_name}
        try:
            completed = subprocess.run(argv, cwd=cwd, env=env, capture_output=True, timeout=args.timeout, check=False)
            stdout, stderr, returncode = completed.stdout, completed.stderr, completed.returncode
        except subprocess.TimeoutExpired as error:
            stdout, stderr, returncode = error.stdout or b"", error.stderr or b"", None
            record["error"] = f"timeout after {args.timeout} seconds"
        except OSError as error:
            stdout, stderr, returncode = b"", str(error).encode(), None
            record["error"] = str(error)
        (output / stdout_name).write_bytes(stdout)
        (output / stderr_name).write_bytes(stderr)
        record["returncode"] = returncode
        manifest["commands"].append(record)
        save()
        return returncode, stdout

    try:
        inputs = output / "fixtures"
        inputs.mkdir()
        for file in fixture_files:
            shutil.copyfile(file, inputs / file.name)
        if not args.go:
            raise ValueError("Set GO_BIN or pass --go with the activated Go 1.27.1 binary")
        selected = shutil.which(args.go)
        if selected is None:
            raise ValueError(f"Go binary is not executable: {args.go}")
        go = str(Path(selected).resolve())
        manifest["go_binary"] = go
        code, stdout = execute([go, "version"], output, "go-version.stdout.log", "go-version.stderr.log")
        version = stdout.decode("utf-8", errors="replace").strip()
        manifest["go_version"] = version
        if code != 0 or version.split()[:3] != ["go", "version", GO_VERSION]:
            raise ValueError(f"Expected {GO_VERSION}; observed {version!r}")

        with tempfile.TemporaryDirectory(prefix="goxide-match-borrow-module-") as temp:
            module = Path(temp)
            manifest["isolated_module"] = str(module)
            for file in sorted(inputs.glob("*.fixture")):
                shutil.copyfile(file, module / file.name.removesuffix(".fixture"))
            race_code, raw = execute([go, "test", "-mod=readonly", "-race", "-count=1", "-json", "./..."], module, "race.jsonl", "race.stderr.log")
            vet_code, _ = execute([go, "vet", "-mod=readonly", "./..."], module, "vet.stdout.log", "vet.stderr.log")
        manifest["isolated_module_removed"] = True

        events = [json.loads(line) for line in raw.splitlines() if line.strip()]
        passed = [event["Test"] for event in events if event.get("Action") == "pass" and "Test" in event and "/" not in event["Test"]]
        failed = [event["Test"] for event in events if event.get("Action") == "fail" and "Test" in event]
        skipped = [event["Test"] for event in events if event.get("Action") == "skip" and "Test" in event]
        observations = []
        # Keep each emitted JSON payload verbatim; do not rewrite its keys/values.
        lines = "".join(event.get("Output", "") for event in events).splitlines()
        for line in lines:
            if line.startswith("OBSERVATION "):
                payload = line.removeprefix("OBSERVATION ")
                json.loads(payload)
                observations.append(payload)
        (output / "observations.jsonl").write_text("".join(line + "\n" for line in observations), encoding="utf-8")
        unique_cases = {json.loads(line)["case"] for line in observations}
        ok = race_code == 0 and vet_code == 0 and bool(passed) and not failed and not skipped and len(observations) == len(passed) == len(unique_cases)
        summary.update({
            "status": "PASS" if ok else "FAIL",
            "go_version": version,
            "race_returncode": race_code,
            "vet_returncode": vet_code,
            "passed_tests": passed,
            "failed_tests": failed,
            "skipped_tests": skipped,
            "test_count": len(passed) + len(failed) + len(skipped),
            "observation_count": len(observations),
            "language_policy_selected": False,
        })
        if not ok:
            summary["error"] = "Require successful race/vet, at least one passing test, no failures/skips, and one unique observation per test"
    except (OSError, ValueError, KeyError) as error:
        summary["error"] = str(error)
    finally:
        manifest["finished_utc"] = datetime.now(timezone.utc).isoformat()
        save()
    sys.stdout.write(json_text(summary))
    return 0 if summary["status"] == "PASS" else 1


if __name__ == "__main__":
    raise SystemExit(main())
