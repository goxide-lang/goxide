//go:build js && wasm

// goxide-playground exposes the real single-file frontend inside a Web Worker.
package main

import (
	"fmt"
	"github.com/hxsf/goxide/internal/frontend"
	"syscall/js"
)

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
	r, err := frontend.Compile("playground.gox", []byte(source))
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	if len(r.Go) > 2*1024*1024 {
		return map[string]any{"error": "generated output exceeds 2 MiB"}
	}
	return map[string]any{"go": string(r.Go)}
}

func main() {
	js.Global().Set("goxideCompile", js.FuncOf(compile))
	js.Global().Call("postMessage", map[string]any{"type": "ready"})
	select {}
}
