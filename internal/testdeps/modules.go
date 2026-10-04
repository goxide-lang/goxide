// Package testdeps supplies the compiler's pinned public runtime dependencies
// to generated-code test modules. It does not download or replace modules.
package testdeps

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// WriteFile writes a generated-code fixture. For go.mod it also declares the
// real std/serde versions and copies their verified checksums from this checkout.
// Project resolver/trust fixtures should use os.WriteFile instead.
func WriteFile(name string, data []byte, mode os.FileMode) error {
	if filepath.Base(name) != "go.mod" {
		return os.WriteFile(name, data, mode)
	}
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(here), "../..")
	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return err
	}
	sum, err := os.ReadFile(filepath.Join(root, "go.sum"))
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(mod), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && (f[0] == "github.com/goxide-lang/std" || f[0] == "github.com/goxide-lang/serde") && !strings.Contains(string(data), f[0]+" ") {
			data = append(data, []byte(fmt.Sprintf("\nrequire %s %s\n", f[0], f[1]))...)
		}
	}
	if err := os.WriteFile(name, data, mode); err != nil {
		return err
	}
	sumfile := filepath.Join(filepath.Dir(name), "go.sum")
	existing, err := os.ReadFile(sumfile)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, line := range strings.Split(string(sum), "\n") {
		if strings.HasPrefix(line, "github.com/goxide-lang/") && !strings.Contains(string(existing), line) {
			existing = append(existing, []byte(line+"\n")...)
		}
	}
	return os.WriteFile(sumfile, existing, mode)
}
