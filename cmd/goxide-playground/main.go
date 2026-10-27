//go:build js && wasm

// goxide-playground exposes the compiler pipeline inside a Web Worker.
package main

import (
	"crypto/sha256"
	_ "embed"
	"fmt"
	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/paths"
	"github.com/goxide-lang/goxide/internal/playground"
	"github.com/goxide-lang/goxide/internal/rootconfig"
	"strings"
	"syscall/js"
)

//go:embed goxide.roots.toml
var dependencyContext []byte

var dependencyRoots paths.Roots

// Set from the same Go environment used to produce the embedded export archives.
var exportOS, exportArch string
var compilationEnvironment *config.Environment

func requestConfig() config.Config {
	return config.Config{Environment: compilationEnvironment, Reads: &config.Reads{}}
}

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
	var diagnostics []string
	reportedErrors := false
	options, err := compileOptions("goxide/generated", &diagnostics, &reportedErrors)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	output, err := playground.CompilePackage([]playground.File{{Path: "playground.gox", Source: source}}, options)
	if err != nil {
		if reportedErrors {
			err = analysis.SummarizeProcDiagnostics(err)
		}
		return map[string]any{"error": err.Error(), "diagnostics": strings.Join(diagnostics, "\n")}
	}
	return map[string]any{"go": output[0].Source, "diagnostics": strings.Join(diagnostics, "\n")}
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
	var diagnostics []string
	reportedErrors := false
	options, err := compileOptions("", &diagnostics, &reportedErrors)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	output, err := playground.CompilePackage(files, options)
	if err != nil {
		if reportedErrors {
			err = analysis.SummarizeProcDiagnostics(err)
		}
		return map[string]any{"error": err.Error(), "diagnostics": strings.Join(diagnostics, "\n")}
	}
	values := make([]any, len(output))
	for i, file := range output {
		values[i] = map[string]any{"path": file.Path, "source": file.Source}
	}
	return map[string]any{"files": values, "diagnostics": strings.Join(diagnostics, "\n")}
}

func main() {
	var err error
	compilationEnvironment, err = config.NewEnvironment(config.Target{OS: exportOS, Arch: exportArch}, []string{})
	if err != nil {
		js.Global().Call("postMessage", map[string]any{"type": "fatal", "error": "missing or invalid export-data target: " + err.Error()})
		return
	}
	dependencyRoots, err = rootconfig.Parse("goxide.roots.toml", dependencyContext)
	if err != nil {
		js.Global().Call("postMessage", map[string]any{"type": "fatal", "error": err.Error()})
		return
	}
	if err := loadMacroBundle(); err != nil {
		js.Global().Call("postMessage", map[string]any{"type": "fatal", "error": err.Error()})
		return
	}
	js.Global().Set("goxideCompile", js.FuncOf(func(this js.Value, args []js.Value) any {
		return asynchronousCompile(func() any { return compile(this, args) })
	}))
	js.Global().Set("goxideCompilePackage", js.FuncOf(func(this js.Value, args []js.Value) any {
		return asynchronousCompile(func() any { return compilePackage(this, args) })
	}))
	artifacts := make([]any, 0, len(macroBundle.Artifacts))
	for _, artifact := range macroBundle.Artifacts {
		artifacts = append(artifacts, artifact.SHA256)
	}
	js.Global().Call("postMessage", map[string]any{"type": "ready", "rootsSha256": fmt.Sprintf("%x", sha256.Sum256(dependencyContext)), "macroArtifacts": artifacts})
	select {}
}
