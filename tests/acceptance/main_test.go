package acceptance_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/goxide-lang/goxide/tests/testutil"
)

func TestMain(m *testing.M) {
	code := m.Run()
	if err := testutil.CleanupCLI(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		code = 1
	}
	os.Exit(code)
}

func acceptanceCLI(t *testing.T) string {
	t.Helper()
	return testutil.CLI(t)
}
