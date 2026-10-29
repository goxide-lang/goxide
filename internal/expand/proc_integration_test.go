package expand_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/parser"
)

func TestProceduralBridgeGeneratedProgram(t *testing.T) {
	directory := t.TempDir()
	run := func(dir string, input []byte, command string, args ...string) []byte {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, command, args...)
		cmd.Dir, cmd.Stdin = dir, bytes.NewReader(input)
		cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v\n%s", command, args, err, output)
		}
		return output
	}
	guest := filepath.Join(directory, "guest")
	run(".", nil, "go", "build", "-o", guest, "./testdata/proc_bridge/guest")
	var requests, responses [][]byte
	generate := func() map[string][]byte {
		t.Helper()
		files := map[string]*ast.File{}
		for _, name := range []string{"main.gox", "definition.gox"} {
			source, err := os.ReadFile(filepath.Join("testdata", "proc_bridge", name))
			if err != nil {
				t.Fatal(err)
			}
			file, diagnostics := parser.ParseFile(name, source)
			if len(diagnostics) != 0 {
				t.Fatalf("parse %s: %v", name, diagnostics)
			}
			files[name] = file
		}
		expanded, err := expand.BridgeExpressionForTest(files, func(request []byte) ([]byte, error) {
			response := run(directory, request, guest)
			requests = append(requests, request)
			responses = append(responses, response)
			return response, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		resolved := resolve.ResolveExpanded(files, expanded, nil)
		if len(resolved.Diagnostics) != 0 {
			t.Fatalf("resolve: %v", resolved.Diagnostics)
		}
		trees := map[string]*ast.File{}
		for name, file := range resolved.Files {
			trees[name] = file.AST
			seen := map[ast.NodeID]bool{}
			ast.Walk(file.AST, func(node ast.Node) bool {
				if node.ID() == 0 || seen[node.ID()] {
					t.Fatalf("duplicated output AST identity in %s: %d", name, node.ID())
				}
				seen[node.ID()] = true
				return true
			})
		}
		info, diagnostics := check.Check(trees, resolved)
		if len(diagnostics) != 0 {
			t.Fatalf("check: %v", diagnostics)
		}
		output, err := lower.Lower(trees, info)
		if err != nil {
			t.Fatal(err)
		}
		return output
	}
	output, again := generate(), generate()
	if !reflect.DeepEqual(output, again) || !bytes.Equal(requests[0], requests[1]) || !bytes.Equal(responses[0], responses[1]) {
		t.Fatal("identical input produced different protocol or generated Go bytes")
	}
	for name, source := range output {
		path := filepath.Join(directory, strings.TrimSuffix(name, ".gox")+".go")
		if err := os.WriteFile(path, source, 0600); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s:\n%s", name, source)
	}
	if err := os.WriteFile(filepath.Join(directory, "go.mod"), []byte("module example.org/proc-bridge\n\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	run(directory, nil, "go", "build", "-o", "consumer", ".")
	if got := string(run(directory, nil, filepath.Join(directory, "consumer"))); got != "143\n" {
		t.Fatalf("captured/definition/caller binding result: got %q, want 143", got)
	}
	programTest := fmt.Sprintf("package main\nimport \"testing\"\nfunc TestGenerated(t *testing.T) { if got := run(); got != %d { t.Fatal(got) } }\n", 143)
	if err := os.WriteFile(filepath.Join(directory, "consumer_test.go"), []byte(programTest), 0600); err != nil {
		t.Fatal(err)
	}
	run(directory, nil, "go", "test", "-count=1", ".")
}
