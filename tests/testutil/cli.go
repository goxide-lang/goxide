package testutil

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/goxide-lang/goxide/internal/project"
)

var (
	cliOnce sync.Once
	cliDir  string
	cliPath string
	cliErr  error

	// Fixture environment changes must not affect the shared compiler build.
	cliEnvironment = project.GoEnv(true, os.Environ())
	cliTempRoot    = os.TempDir()
)

// CLI builds the default SDK-stamped compiler once per test process.
// The package's TestMain must call CleanupCLI after m.Run.
func CLI(t testing.TB) string {
	t.Helper()
	cliOnce.Do(func() {
		cliDir, cliErr = os.MkdirTemp(cliTempRoot, "goxide-test-cli-")
		if cliErr != nil {
			return
		}
		cliPath = filepath.Join(cliDir, "goxide")
		_, here, _, _ := runtime.Caller(0)
		command := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "run", "./scripts/build.go", "-o", cliPath)
		command.Dir = filepath.Join(filepath.Dir(here), "../..")
		command.Env = cliEnvironment
		if output, err := command.CombinedOutput(); err != nil {
			cliErr = fmt.Errorf("build stamped CLI: %w\n%s", err, output)
		}
	})
	if cliErr != nil {
		t.Fatal(cliErr)
	}
	return cliPath
}

// CleanupCLI removes the shared compiler after all package tests finish.
func CleanupCLI() error {
	if cliDir == "" {
		return nil
	}
	return os.RemoveAll(cliDir)
}
