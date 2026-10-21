//go:build ignore

package main

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"os"
	"strconv"
	"time"

	pm "github.com/goxide-lang/syntax/proc_macro"
	"github.com/goxide-lang/syntax/token"
)

var initialized int

func init() { initialized++ }

func main() {
	request, err := pm.ReadRequest(os.Stdin)
	if err != nil {
		panic(err)
	}
	output := pm.Success(request.Input())
	switch request.Entry() {
	case "state":
		if initialized != 1 || len(os.Environ()) != 0 || len(os.Args) != 1 || os.Args[0] != "goxide-proc-macro" {
			panic("host state leaked")
		}
		initialized++
		var random [17]byte
		if _, err := rand.Read(random[:3]); err != nil {
			panic(err)
		}
		if _, err := rand.Read(random[3:]); err != nil {
			panic(err)
		}
		before := time.Now()
		time.Sleep(time.Hour)
		if time.Since(before) < time.Hour {
			panic("virtual sleep did not advance clock")
		}
		text := strconv.Quote(fmt.Sprintf("%d/%x/%d", initialized, random, time.Now().UnixNano()))
		result, err := token.ParseText(text, request.Context().DefSite())
		if err != nil {
			panic(err)
		}
		output = pm.Success(result)
	case "diagnostic":
		output.Diagnostics = []pm.Diagnostic{
			{Severity: pm.Note, Message: "before error"},
			{Severity: pm.Error, Message: "expected error", Span: request.Context().CallSpan()},
		}
	case "stdout_limit":
		os.Stdout.Write(bytes.Repeat([]byte{'x'}, pm.MaxMessageBytes+1))
		return
	case "stderr_limit":
		os.Stderr.Write(bytes.Repeat([]byte{'x'}, pm.MaxDiagnosticBytes+1))
		return
	case "unknown_reference":
		fmt.Fprintf(os.Stdout, "{\"id\":%q,\"tokens\":[{\"ref\":4294967295}]}", request.ID())
		return
	case "panic":
		panic("browser panic fixture")
	case "exit":
		os.Exit(7)
	}
	if err := pm.WriteResponse(os.Stdout, request, output); err != nil {
		panic(err)
	}
	if request.Entry() == "trailing_output" {
		fmt.Fprint(os.Stdout, "{}")
	}
	if request.Entry() == "response_loop" {
		for {
		}
	}
}
