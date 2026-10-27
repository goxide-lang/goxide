package analysis

import (
	"go/build"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Goxide sources obey the same filename and header selection as their Go output.
func matchesModuleSource(filename string, context build.Context) (bool, error) {
	context.OpenFile = func(string) (io.ReadCloser, error) { return os.Open(filename) }
	base := strings.TrimSuffix(filepath.Base(filename), ".gox") + ".go"
	return context.MatchFile(filepath.Dir(filename), base)
}
