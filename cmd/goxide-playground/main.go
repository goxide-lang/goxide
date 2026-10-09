//go:build js && wasm

// goxide-playground exposes the real frontend inside a Web Worker.
package main

import (
	"crypto/sha256"
	_ "embed"
	"fmt"
	"github.com/hxsf/goxide/internal/frontend"
	"github.com/hxsf/goxide/internal/paths"
	"github.com/hxsf/goxide/internal/playground"
	"github.com/hxsf/goxide/internal/rootconfig"
	"github.com/hxsf/goxide/internal/typecheck"
	"syscall/js"
)

//go:embed goxide.roots.toml
var dependencyContext []byte

var dependencyRoots paths.Roots

func compile(_ js.Value, args []js.Value) (result any) {
	defer func() {
		if r := recover(); r != nil {
			result = map[string]any{"error": fmt.Sprintf("compiler panic: %v", r)}
		}
	}()
	if len(args) != 1 || args[0].Type() != js.TypeString {
		return map[string]any{"error": "expected source text"}
	}
	source := args[0].String()
	if len(source) > 32768 {
		return map[string]any{"error": "source exceeds 32 KiB"}
	}
	r, err := frontend.CompileWithOptions("playground.gox", []byte(source), frontend.FileOptions{
		Roots: dependencyRoots, Importer: typecheck.NewImporter("playground.gox"),
	})
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	if len(r.Go) > 2*1024*1024 {
		return map[string]any{"error": "generated output exceeds 2 MiB"}
	}
	return map[string]any{"go": string(r.Go)}
}

func compilePackage(_ js.Value, args []js.Value) (result any) {
	defer func() {
		if r := recover(); r != nil {
			result = map[string]any{"error": fmt.Sprintf("compiler panic: %v", r)}
		}
	}()
	if len(args) != 1 || !js.Global().Get("Array").Call("isArray", args[0]).Bool() {
		return map[string]any{"error": "expected a source file array"}
	}
	length := args[0].Length()
	if length < 1 || length > playground.MaxFiles {
		return map[string]any{"error": fmt.Sprintf("expected 1 to %d source files", playground.MaxFiles)}
	}
	files := make([]playground.File, length)
	for i := range files {
		file := args[0].Index(i)
		if file.Type() != js.TypeObject || file.IsNull() || file.Get("path").Type() != js.TypeString || file.Get("source").Type() != js.TypeString {
			return map[string]any{"error": fmt.Sprintf("file %d: expected path and source strings", i+1)}
		}
		files[i] = playground.File{Path: file.Get("path").String(), Source: file.Get("source").String()}
	}
	// On js/wasm this reads only the export archives embedded at website build
	// time. Each request receives a fresh importer and no request-local state.
	output, err := playground.CompilePackage(files, frontend.PackageOptions{
		Roots: dependencyRoots, Importer: typecheck.NewImporter("playground.gox"),
	})
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	values := make([]any, len(output))
	for i, file := range output {
		values[i] = map[string]any{"path": file.Path, "source": file.Source}
	}
	return map[string]any{"files": values}
}

func main() {
	var err error
	dependencyRoots, err = rootconfig.Parse("goxide.roots.toml", dependencyContext)
	if err != nil {
		js.Global().Call("postMessage", map[string]any{"type": "fatal", "error": err.Error()})
		return
	}
	js.Global().Set("goxideCompile", js.FuncOf(compile))
	js.Global().Set("goxideCompilePackage", js.FuncOf(compilePackage))
	js.Global().Call("postMessage", map[string]any{"type": "ready", "rootsSha256": fmt.Sprintf("%x", sha256.Sum256(dependencyContext))})
	select {}
}
