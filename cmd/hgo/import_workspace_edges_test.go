package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeWorkspaceExternalUseAndTestSuffix(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	workspaceWrite(t, root, "go.work", "go 1.27\nuse (\n../app\n../lib\n)\n")
	workspaceWrite(t, parent, "app/go.mod", "module example.org/app.test\ngo 1.27\n")
	workspaceWrite(t, parent, "app/main.go", "package main\nimport(\"fmt\";\"example.org/lib.test\")\nfunc main(){fmt.Println(lib.Value)}\n")
	workspaceWrite(t, parent, "lib/go.mod", "module example.org/lib.test\ngo 1.27\n")
	workspaceWrite(t, parent, "lib/lib.go", "package lib\nconst Value=\"external\"\n")
	before := sourceHash(t, parent)
	dest := filepath.Join(t.TempDir(), "new")
	out, err := importCall(t, root, "--workspace", "--out", dest, "--offline", "--member-bin", "example.org/app.test::app=.")
	if err != nil {
		t.Fatalf("%s %v", out, err)
	}
	if sourceHash(t, parent) != before {
		t.Fatal("changed external use source")
	}
	// Copied use members must not rely on their old paths.
	if err = os.RemoveAll(parent); err != nil {
		t.Fatal(err)
	}
	out, err = projectRun(t, dest, "run", "-p", "example.org/app.test", "--offline", "--locked")
	if err != nil || strings.TrimSpace(out) != "external" {
		t.Fatalf("%s %v", out, err)
	}
}

func TestNativeWorkspaceRaceOnlyEdge(t *testing.T) {
	root := nativeWorkspaceFixture(t)
	workspaceWrite(t, root, "go.work", "go 1.27\nuse (\n./app\n./lib\n./raceonly\n)\n")
	workspaceWrite(t, root, "raceonly/go.mod", "module example.org/raceonly\ngo 1.27\n")
	workspaceWrite(t, root, "raceonly/lib.go", "package raceonly\nconst Value=1\n")
	workspaceWrite(t, root, "app/race_test.go", "//go:build race\n\npackage main\nimport(\"testing\";\"example.org/raceonly\")\nfunc TestRaceEdge(t *testing.T){if raceonly.Value!=1 {t.Fatal(\"value\")}}\n")
	dest := filepath.Join(t.TempDir(), "new")
	out, err := importCall(t, root, "--workspace", "--out", dest, "--offline")
	if err != nil {
		t.Fatalf("%s %v", out, err)
	}
	out, err = projectRun(t, dest, "test", "-p", "example.org/app", "--offline", "--locked")
	if err != nil {
		t.Fatalf("%s %v", out, err)
	}
}

func TestNativeWorkspaceImportGraphRejections(t *testing.T) {
	for _, kind := range []string{"unknown-directive", "duplicate-module", "module-cycle", "duplicate-bin"} {
		t.Run(kind, func(t *testing.T) {
			root := nativeWorkspaceFixture(t)
			switch kind {
			case "unknown-directive":
				workspaceWrite(t, root, "go.work", "go 1.27\nuse ./app\nunknown nope\n")
			case "duplicate-module":
				workspaceWrite(t, root, "lib/go.mod", "module example.org/app\ngo 1.27\n")
			case "module-cycle":
				workspaceWrite(t, root, "lib/lib.go", "package lib\nimport _ \"example.org/app/helper\"\nfunc Value()string{return \"cycle\"}\n")
				workspaceWrite(t, root, "app/helper/helper.go", "package helper\n")
			case "duplicate-bin":
				for _, dir := range []string{"a/web", "b/web"} {
					workspaceWrite(t, root, "app/"+dir+"/main.go", "package main\nfunc main(){}\n")
				}
			}
			before := sourceHash(t, root)
			dest := filepath.Join(t.TempDir(), "new")
			if out, err := importCall(t, root, "--workspace", "--out", dest, "--offline"); err == nil {
				t.Fatalf("accepted %s: %s", kind, out)
			}
			if _, err := os.Stat(dest); !os.IsNotExist(err) {
				t.Fatal("rejected output published")
			}
			if sourceHash(t, root) != before {
				t.Fatal("rejection changed source")
			}
		})
	}
}

func TestNativeWorkspaceMultipleTargets(t *testing.T) {
	root := nativeWorkspaceFixture(t)
	workspaceWrite(t, root, "app/cmd/web/main.go", "package main\nimport \"fmt\"\nfunc main(){fmt.Println(\"web\")}\n")
	workspaceWrite(t, root, "app/examples/demo/main.go", "package main\nimport \"fmt\"\nfunc main(){fmt.Println(\"demo\")}\n")
	dest := filepath.Join(t.TempDir(), "new")
	out, err := importCall(t, root, "--workspace", "--out", dest, "--offline", "--member-bin", "example.org/app::app=.", "--member-bin", "example.org/app::web=cmd/web")
	if err != nil {
		t.Fatalf("%s %v", out, err)
	}
	manifest := filepath.Join(dest, "members/m0/goxide.toml")
	f, err := os.OpenFile(manifest, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("\n[[example]]\nname=\"demo\"\npath=\"examples/demo\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	out, err = projectRun(t, dest, "build", "--workspace", "--offline")
	if err != nil {
		t.Fatalf("%s %v", out, err)
	}
	for _, target := range []string{"web", "demo"} {
		flag := "--bin"
		if target == "demo" {
			flag = "--example"
		}
		out, err = projectRun(t, dest, "run", "-p", "example.org/app", flag, target, "--offline", "--locked")
		if err != nil || strings.TrimSpace(out) != target {
			t.Fatalf("%s %v", out, err)
		}
	}
	// Unselected example errors must not prevent the explicitly selected binary.
	workspaceWrite(t, dest, "members/m0/examples/demo/main.go", "package main\nfunc main(){undefined()}\n")
	out, err = projectRun(t, dest, "build", "-p", "example.org/app", "--bin", "web", "--offline", "--locked")
	if err != nil {
		t.Fatalf("%s %v", out, err)
	}
	if _, err = projectRun(t, dest, "build", "-p", "example.org/app", "--example", "demo", "--offline", "--locked"); err == nil {
		t.Fatal("invalid selected example accepted")
	}
}
