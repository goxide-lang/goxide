package compile

import (
	"os"
	"sync"

	"github.com/goxide-lang/goxide/internal/project"
)

// Every unit prepared by this process uses the same compiler identity.
var compilerIdentity = sync.OnceValues(func() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	compiler, err := os.ReadFile(executable)
	if err != nil {
		return "", err
	}
	return project.Digest(compiler), nil
})
