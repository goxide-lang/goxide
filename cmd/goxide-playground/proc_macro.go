//go:build js && wasm

package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"maps"
	"syscall/js"
	"time"

	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/playground"
	"github.com/goxide-lang/goxide/internal/procmacro"
	"github.com/goxide-lang/goxide/internal/typecheck"
	pm "github.com/goxide-lang/syntax/proc_macro"
)

//go:embed proc_bundle.json
var bundledMacros []byte

var macroBundle playground.MacroBundle
var compiling bool

func loadMacroBundle() error {
	var err error
	macroBundle, err = playground.ParseMacroBundle(bundledMacros)
	if err != nil {
		return err
	}
	for alias, root := range macroBundle.Roots {
		dependencyRoots[alias] = root
	}
	return nil
}

// A FuncOf callback must return before a goroutine can wait for a macro Worker.
// The returned Promise also keeps the existing worker request ID alive.
func asynchronousCompile(work func() any) js.Value {
	if compiling {
		return js.Global().Get("Promise").Call("resolve", map[string]any{"error": "compiler already has an active request"})
	}
	compiling = true
	executor := js.FuncOf(func(_ js.Value, args []js.Value) any {
		resolve := args[0]
		go func() {
			defer func() { compiling = false }()
			resolve.Invoke(work())
		}()
		return nil
	})
	promise := js.Global().Get("Promise").New(executor)
	executor.Release()
	return promise
}

func compileOptions(packagePath string, diagnostics *[]string, reportedErrors *bool) (playground.Options, error) {
	cfg := requestConfig()
	cfg.Features = macroBundle.Features
	if macroBundle.PackagePath != "" {
		packagePath = macroBundle.PackagePath
	}
	importer := typecheck.NewImporter("playground.gox", typecheck.ImporterOptions{Config: cfg})
	host := new(browserMacroHost)
	libraries, err := macroBundle.Libraries(importer, host.run)
	return playground.Options{
		Config: cfg, Roots: maps.Clone(dependencyRoots), Importer: importer, PackagePath: packagePath,
		ProcLibraries: libraries, RuntimeRoots: macroBundle.RuntimeRoots,
		SourcePackages: macroBundle.SourcePackages,
		OnProcDiagnostic: func(filename string, diagnostic expand.ProcDiagnostic) {
			if diagnostic.Severity == pm.Error {
				*reportedErrors = true
			}
			line, column := 1, int(diagnostic.Location.Span.Start)+1
			if file := diagnostic.Location.File; file != nil {
				for index, offset := range file.LineStarts {
					if offset > diagnostic.Location.Span.Start {
						break
					}
					line, column = index+1, int(diagnostic.Location.Span.Start-offset)+1
				}
			}
			*diagnostics = append(*diagnostics, fmt.Sprintf("%s:%d:%d: %s: %s", filename, line, column, diagnostic.Severity, diagnostic.Message))
		},
	}, err
}

type browserMacroHost struct{ spent time.Duration }

func (host *browserMacroHost) run(digest string, request pm.Request) (response []byte, err error) {
	remaining := min(procmacro.MaxInvocationTime, procmacro.MaxCompilationTime-host.spent)
	if remaining <= 0 {
		return nil, fmt.Errorf("procedural macro cumulative execution budget exceeded")
	}
	started := time.Now()
	budget := remaining
	defer func() {
		elapsed := time.Since(started)
		host.spent += elapsed
		if elapsed > budget {
			response, err = nil, fmt.Errorf("procedural macro execution time budget exceeded")
		}
	}()
	var input bytes.Buffer
	if err := pm.WriteRequest(&input, request); err != nil {
		return nil, err
	}
	remaining -= time.Since(started)
	if remaining <= 0 {
		return nil, fmt.Errorf("procedural macro invocation time budget exceeded")
	}
	function := js.Global().Get("goxideRunProcMacro")
	if function.Type() != js.TypeFunction {
		return nil, fmt.Errorf("browser procedural macro host is unavailable")
	}
	encoded := js.Global().Get("Uint8Array").New(input.Len())
	js.CopyBytesToJS(encoded, input.Bytes())
	value, err := awaitPromise(function.Invoke(digest, encoded, float64(remaining)/float64(time.Millisecond)))
	if err != nil {
		return nil, err
	}
	response, err = browserBytes(value.Get("response"), pm.MaxMessageBytes)
	if err != nil {
		return nil, err
	}
	stderr, err := browserBytes(value.Get("stderr"), pm.MaxDiagnosticBytes)
	if err != nil {
		return nil, err
	}
	if err := procmacro.ValidateResponse(request, response, len(stderr)); err != nil {
		return nil, fmt.Errorf("%w%s", err, browserStderr(stderr))
	}
	return response, nil
}

func browserStderr(stderr []byte) string {
	if len(stderr) == 0 {
		return ""
	}
	return ": " + string(stderr)
}

func browserBytes(value js.Value, limit int) ([]byte, error) {
	if !value.InstanceOf(js.Global().Get("ArrayBuffer")) {
		return nil, fmt.Errorf("procedural macro host returned invalid bytes")
	}
	if value.Get("byteLength").Int() > limit {
		return nil, fmt.Errorf("procedural macro host output exceeds %d bytes", limit)
	}
	array := js.Global().Get("Uint8Array").New(value)
	data := make([]byte, array.Length())
	js.CopyBytesToGo(data, array)
	return data, nil
}

func awaitPromise(promise js.Value) (js.Value, error) {
	type result struct {
		value js.Value
		err   error
	}
	done := make(chan result, 1)
	success := js.FuncOf(func(_ js.Value, args []js.Value) any {
		done <- result{value: args[0]}
		return nil
	})
	failure := js.FuncOf(func(_ js.Value, args []js.Value) any {
		done <- result{err: fmt.Errorf("procedural macro execution: %s", js.Global().Get("String").Invoke(args[0]).String())}
		return nil
	})
	defer success.Release()
	defer failure.Release()
	promise.Call("then", success, failure)
	finished := <-done
	return finished.value, finished.err
}
