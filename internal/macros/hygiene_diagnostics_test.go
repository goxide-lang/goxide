package macros

import (
	"fmt"
	"go/types"
	"strings"
	"testing"
)

func TestHygieneDiagnosticToken(t *testing.T) {
	for _, tc := range []struct{ name, body, token, message string }{
		{"selector value", "_=1\n_=fmt::Println", "Println", "direct function calls"},
		{"missing function", "fmt::Println(1)\nfmt::Missing()", "Missing", "must name a function"},
		{"key", "_=1\n_=map[string]int{key:2}", "key", "identifier keyed literals"},
		{"type", "_=1\ntype Local int", "Local", "local type"},
		{"label", "_=1\nHere: for {break}", "Here", "local type"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, newline := range []string{"\n", "\r\n"} {
				src := "package p\nimport \"fmt\"\nmacro_rules! bad {()=>{\n" + tc.body + "\n};}\nfunc Run(){bad!()}\n"
				src = strings.ReplaceAll(src, "\n", newline)
				_, err := ExpandWithMap("definition.gox", []byte(src))
				offset := strings.Index(src, tc.token)
				line := strings.Count(src[:offset], "\n") + 1
				column := offset - strings.LastIndex(src[:offset], "\n")
				want := fmt.Sprintf("definition.gox:%d:%d:", line, column)
				if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), tc.message) || !strings.Contains(err.Error(), "invoked at definition.gox:") {
					t.Fatalf("want %s %s, got %v", want, tc.message, err)
				}
			}
		})
	}
}

func TestHygieneDiagnosticFirstProblem(t *testing.T) {
	src := []byte("package p\nimport \"fmt\"\nmacro_rules! bad {()=>{\n_=fmt::Println\n_=map[string]int{key:2}\n};}\nfunc Run(){bad!()}\n")
	_, err := ExpandWithMap("first.gox", src)
	if err == nil || !strings.Contains(err.Error(), "first.gox:4:8:") || !strings.Contains(err.Error(), "direct function calls") {
		t.Fatalf("first problem lost: %v", err)
	}
}

func TestPackageHygieneDiagnosticDefinitionFile(t *testing.T) {
	definitions := []byte("package dep\nimport \"fmt\"\nmacro_rules! inner {()=>{\nfmt::Println(1)\n_=fmt::Println\n};}\npub macro_rules! outer {()=>{inner!()};}\n")
	dep := registry(t, "example/dep", map[string][]byte{"definitions.gox": definitions}, nil)
	caller := []byte("package caller\nimport d \"example/dep\"\nfunc Run(){d::outer!()}\n")
	pkg := registry(t, "example/caller", map[string][]byte{"caller.gox": caller}, map[string]*Package{"example/dep": dep})
	_, err := ExpandPackage("caller.gox", caller, nil, pkg, nil, ExpansionOptions{})
	for _, want := range []string{"definitions.gox:5:8:", "direct function calls", "invoked at definitions.gox:7:", "invoked at caller.gox:3:"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("want %q in %v", want, err)
		}
	}
}

type diagnosticImporter struct{ called bool }

func (i *diagnosticImporter) Import(path string) (*types.Package, error) {
	i.called = true
	return nil, fmt.Errorf("injected importer refused %s", path)
}
func TestSingleFileMacroImporter(t *testing.T) {
	imp := &diagnosticImporter{}
	_, err := ExpandPackage("single.gox", []byte("package p\nimport \"fmt\"\nmacro_rules! show {()=>{fmt::Println(1)};}\nfunc Run(){show!()}\n"), nil, nil, imp, ExpansionOptions{})
	if !imp.called || err == nil || !strings.Contains(err.Error(), "injected importer refused fmt") {
		t.Fatalf("single-file importer ignored: %v", err)
	}
}
