//go:build ignore

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/goxide-lang/goxide/internal/procmacro"
	pm "github.com/goxide-lang/syntax/proc_macro"
	"github.com/goxide-lang/syntax/token"
)

func request(entry string) (pm.Request, error) {
	scope := token.NewScope("browser-conformance-" + entry)
	definition, call := scope.NewContext(), scope.NewContext()
	span, err := scope.NewSpan(0, 2)
	if err != nil {
		return pm.Request{}, err
	}
	input, err := token.ParseText("42", call)
	if err != nil {
		return pm.Request{}, err
	}
	return pm.NewRequest(scope, pm.RequestConfig{
		Entry: entry, Kind: pm.Function, Position: pm.ExpressionPosition,
		DefSite: definition, CallSite: call, DefSpan: span, CallSpan: span,
		Target: pm.Target{OS: "linux", Arch: "amd64"}, Input: input,
	})
}

func run() error {
	switch os.Args[1] {
	case "prepare":
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		prepared, err := procmacro.PrepareWASM(data)
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(prepared)
		return err
	case "admit":
		input, err := request(os.Args[2])
		if err != nil {
			return err
		}
		output, err := pm.ReadResponse(os.Stdin, input)
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, output.Tokens.Display())
		return nil
	case "suite":
		wasm, err := os.ReadFile(os.Args[2])
		if err != nil {
			return err
		}
		host, err := procmacro.NewWASIHost(context.Background())
		if err != nil {
			return err
		}
		defer host.Close()
		type fixture struct {
			Request  []byte `json:"request"`
			Response []byte `json:"response,omitempty"`
		}
		fixtures := map[string]fixture{}
		for _, entry := range []string{"echo", "state", "diagnostic", "stdout_limit", "stderr_limit", "panic", "exit", "trailing_output", "unknown_reference", "response_loop"} {
			input, err := request(entry)
			if err != nil {
				return err
			}
			var wire bytes.Buffer
			if err := pm.WriteRequest(&wire, input); err != nil {
				return err
			}
			value := fixture{Request: wire.Bytes()}
			if entry == "echo" || entry == "state" || entry == "diagnostic" {
				value.Response, err = host.Run(wasm, input)
				if err != nil {
					return fmt.Errorf("Go host %s: %w", entry, err)
				}
			}
			fixtures[entry] = value
		}
		return json.NewEncoder(os.Stdout).Encode(fixtures)
	default:
		return fmt.Errorf("unknown fixture command %q", os.Args[1])
	}
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
