//go:build ignore

// Build with scripts/build.go so the selected procedural SDK is stamped.
// This tool runs only during a fixed website build, never in the browser.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/goxide-lang/goxide/internal/compile"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/playground"
	"github.com/goxide-lang/goxide/internal/procmacro"
)

func main() {
	project := flag.String("project", "", "fixed consumer project")
	output := flag.String("output", "", "output directory for manifest.json and proc-macros")
	library := flag.Bool("lib", false, "select the consumer library target")
	goTool := flag.String("go", "go", "Go toolchain executable")
	flag.Parse()
	if *project == "" || *output == "" || flag.NArg() != 0 {
		fail(fmt.Errorf("expected -project and -output"))
	}
	if err := os.MkdirAll(filepath.Join(*output, "proc-macros"), 0755); err != nil {
		fail(err)
	}
	host, err := procmacro.NewWASIHost(context.Background())
	if err != nil {
		fail(err)
	}
	defer host.Close()
	var bundle playground.MacroBundle
	result, err := compile.PrepareProject(*project, compile.Options{
		Lib: *library, GoTool: *goTool, Offline: true, RunProcMacro: host.Run,
		OnPreparedSources: func(prepared compile.PreparedSources) error {
			bundle.Roots, bundle.RuntimeRoots, bundle.SourcePackages = prepared.Roots, prepared.RuntimeRoots, prepared.SourcePackages
			for _, artifact := range prepared.Macros {
				snapshot, err := lower.SnapshotProcLibrary(artifact.Library)
				if err != nil {
					return err
				}
				digest := fmt.Sprintf("%x", sha256.Sum256(artifact.WASM))
				if err := os.WriteFile(filepath.Join(*output, "proc-macros", digest+".wasm"), artifact.WASM, 0644); err != nil {
					return err
				}
				bundle.Artifacts = append(bundle.Artifacts, playground.MacroArtifact{SHA256: digest, Library: snapshot})
			}
			return nil
		},
	})
	if err != nil {
		fail(err)
	}
	if result.Kind == "proc-macro" {
		fail(fmt.Errorf("website macro bundle requires an ordinary consumer target"))
	}
	command := exec.Command(result.Config.Environment.GoTool, "list", "-mod=readonly", "-f", "{{.ImportPath}}", result.Entry)
	command.Dir, command.Env = result.Root, result.Config.Environment.ExecEnv()
	data, err := command.CombinedOutput()
	if err != nil {
		fail(fmt.Errorf("identify browser package: %w: %s", err, data))
	}
	bundle.PackagePath, bundle.Features = strings.TrimSpace(string(data)), result.Config.Features
	data, err = json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		fail(err)
	}
	if _, err := playground.ParseMacroBundle(data); err != nil {
		fail(err)
	}
	if err := os.WriteFile(filepath.Join(*output, "manifest.json"), append(data, '\n'), 0644); err != nil {
		fail(err)
	}
	// This private build result is not copied into website assets.
	if err := json.NewEncoder(os.Stdout).Encode(map[string]string{"root": result.Root, "package": bundle.PackagePath}); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
