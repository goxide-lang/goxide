//go:build ignore

// Build the CLI with the content identity of its linked procedural macro SDK.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/goxide-lang/goxide/internal/procmacro"
)

func main() {
	output := flag.String("o", "goxide", "output binary")
	ldflags := flag.String("ldflags", "", "additional Go linker flags")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: go run ./scripts/build.go [-o binary] [-ldflags flags]")
		os.Exit(2)
	}
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	env := append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "GOFLAGS=")
	identity, err := procmacro.ReadSDKIdentity(context.Background(), tool, ".", env)
	if err != nil {
		fail(err)
	}
	flags := *ldflags + " -X github.com/goxide-lang/goxide/internal/procmacro.CompilerSDKIdentity=" + identity
	command := exec.Command(tool, "build", "-mod=readonly", "-trimpath", "-buildvcs=false", "-ldflags="+flags, "-o", *output, "./cmd/goxide")
	command.Env = env
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
