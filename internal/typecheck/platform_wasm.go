//go:build js && wasm

package typecheck

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
	"go/importer"
	"go/token"
	"go/types"
	"io"
	"strings"
)

// Built by website/build.py from this checkout's Go toolchain. No network,
// subprocess, virtual filesystem, or hand-written package signatures.
//
//go:embed browserexports/*.a browserexports/origins.json
var browserExports embed.FS

type browserOrigin struct {
	Module       string `json:"module"`
	Standard     bool   `json:"standard"`
	ExportSHA256 string `json:"exportSha256"`
}

type browserImporter struct {
	types.Importer
	origins     map[string]browserOrigin
	originError error
}

func (i *browserImporter) PackageOrigin(path string) (string, bool, error) {
	if i.originError != nil {
		return "", false, i.originError
	}
	origin, ok := i.origins[path]
	if !ok {
		return "", false, fmt.Errorf("playground: package origin unavailable for %s", path)
	}
	if _, err := i.Import(path); err != nil {
		return "", false, err
	}
	return origin.Module, origin.Standard, nil
}

func (i *browserImporter) ImportFrom(path, dir string, mode types.ImportMode) (*types.Package, error) {
	if imp, ok := i.Importer.(types.ImporterFrom); ok {
		return imp.ImportFrom(path, dir, mode)
	}
	return i.Import(path)
}

func platformImporter() types.Importer {
	result := &browserImporter{}
	metadata, err := browserExports.ReadFile("browserexports/origins.json")
	if err == nil {
		err = json.Unmarshal(metadata, &result.origins)
	}
	result.originError = err
	result.Importer = importer.ForCompiler(token.NewFileSet(), "gc", func(path string) (io.ReadCloser, error) {
		data, err := browserExports.ReadFile("browserexports/" + strings.ReplaceAll(path, "/", "_") + ".a")
		if err != nil {
			return nil, fmt.Errorf("playground: import %q is outside the bundled package set", path)
		}
		if result.originError != nil {
			return nil, result.originError
		}
		origin, ok := result.origins[path]
		if !ok || fmt.Sprintf("%x", sha256.Sum256(data)) != origin.ExportSHA256 {
			return nil, fmt.Errorf("playground: export origin identity mismatch for %s", path)
		}
		return io.NopCloser(bytes.NewReader(data)), nil
	})
	return result
}
