//go:build ignore

// Build the CLI with the content identity of its linked procedural macro SDK.
package main

import (
	"context"
	"encoding/json"
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
	packageName := flag.String("package", "./cmd/goxide", "compiler or internal build-tool package")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: go run ./scripts/build.go [-o binary] [-ldflags flags] [-package package]")
		os.Exit(2)
	}
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	env := append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "GOFLAGS=")
	identity, err := procmacro.ReadSDKIdentity(context.Background(), tool, ".", env)
	if err != nil {
		fail(err)
	}
	flags := *ldflags + " -X github.com/goxide-lang/goxide/internal/procmacro.CompilerSDKIdentity=" + identity
	module := exec.Command(tool, "mod", "edit", "-json")
	module.Env = env
	data, err := module.Output()
	if err != nil {
		fail(err)
	}
	var manifest struct {
		Require []struct{ Path, Version string }
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		fail(err)
	}
	for _, requirement := range manifest.Require {
		var symbol string
		switch requirement.Path {
		case "github.com/goxide-lang/std":
			symbol = "stdSuggestedVersion"
		case "github.com/goxide-lang/serde":
			symbol = "serdeSuggestedVersion"
		}
		if symbol != "" {
			flags += " -X main." + symbol + "=" + requirement.Version
		}
	}
	command := exec.Command(tool, "build", "-mod=readonly", "-trimpath", "-buildvcs=false", "-ldflags="+flags, "-o", *output, *packageName)
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
