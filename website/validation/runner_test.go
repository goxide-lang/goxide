package main

import (
	"os/exec"
	"testing"
)

// Keep the Python runner's failure-path contract in the repository-wide Go gate.
// Python 3 is already an explicit website/design validation prerequisite.
func TestValidationRunnerFailurePaths(t *testing.T) {
	cmd := exec.Command("python3", "-B", "run_test.py")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("runner failure-path checks: %v\n%s", err, output)
	}
}
