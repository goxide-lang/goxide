//go:build js && wasm

package typecheck

import (
	"bytes"
	"embed"
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
//go:embed browserexports/*.a
var browserExports embed.FS

func platformImporter() types.Importer {
	return importer.ForCompiler(token.NewFileSet(), "gc", func(path string) (io.ReadCloser, error) {
		data, err := browserExports.ReadFile("browserexports/" + strings.ReplaceAll(path, "/", "_") + ".a")
		if err != nil {
			return nil, fmt.Errorf("playground: import %q is outside the bundled package set", path)
		}
		return io.NopCloser(bytes.NewReader(data)), nil
	})
}
