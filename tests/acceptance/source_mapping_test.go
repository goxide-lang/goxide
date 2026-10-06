package acceptance_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/hxsf/goxide/internal/testdeps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/frontend"
	"github.com/hxsf/goxide/internal/macros"
	"github.com/hxsf/goxide/internal/sourcemap"
)

func TestSourceMappingRepeatedTokensNestedExpansion(t *testing.T) {
	const source = `package fixture
macro_rules! inner {($x:expr)=>{$x+7};}
macro_rules! outer {($x:expr)=>{inner!($x)+7};}
func first()int{return outer!(7)}
func second()int{return outer!(7)}
`
	result, err := macros.ExpandWithMap("nested.gox", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	innerLiteral := strings.Index(source, "$x+7") + len("$x+")
	outerLiteral := strings.Index(source, "($x)+7") + len("($x)+")
	firstCall := strings.Index(source, "outer!(7)")
	secondCall := strings.LastIndex(source, "outer!(7)")
	wantOrigins := []int{firstCall + len("outer!("), innerLiteral, outerLiteral, secondCall + len("outer!("), innerLiteral, outerLiteral}
	i := 0
	for _, origin := range result.Origins {
		if string(result.Source[origin.OutputStart:origin.OutputEnd]) != "7" {
			continue
		}
		if i >= len(wantOrigins) || origin.SourceStart != wantOrigins[i] || origin.SourceEnd != wantOrigins[i]+1 {
			t.Fatalf("repeated token %d mapped to [%d,%d), expected origins %v", i, origin.SourceStart, origin.SourceEnd, wantOrigins)
		}
		wantDepth := 2
		if i%3 == 2 {
			wantDepth = 1
		}
		if len(origin.Stack) != wantDepth || origin.Stack[0].Macro != "outer" || (wantDepth == 2 && origin.Stack[1].Macro != "inner") {
			t.Fatalf("token %d lost nested expansion chain: %+v", i, origin.Stack)
		}
		call := firstCall
		if i >= 3 {
			call = secondCall
		}
		if origin.Stack[0].Callsite.Start != call || origin.Stack[0].Callsite.End != call+len("outer!(7)") {
			t.Fatalf("token %d wrong outer invocation span: %+v", i, origin.Stack)
		}
		for _, frame := range origin.Stack {
			definition := strings.Index(source, "macro_rules! "+frame.Macro)
			if frame.Definition.Start != definition || frame.Definition.File != "nested.gox" || frame.Definition.End <= definition {
				t.Fatalf("wrong definition span: %+v", frame)
			}
			if frame.Macro == "inner" {
				start := strings.Index(source, "inner!($x)")
				if frame.Callsite.Start != start || frame.Callsite.End != start+len("inner!($x)") {
					t.Fatalf("wrong nested invocation span: %+v", frame)
				}
			}
		}
		i++
	}
	if i != len(wantOrigins) {
		t.Fatalf("checked %d repeated output tokens, want %d", i, len(wantOrigins))
	}
	compiled, err := frontend.Compile("nested.gox", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	i = 0
	for offset, b := range compiled.Go {
		if b != '7' {
			continue
		}
		s := compiled.Diagnostics.Lookup(offset)
		if i >= len(wantOrigins) || s == nil || s.Origin == nil || s.Origin.Start != wantOrigins[i] || s.Origin.End != wantOrigins[i]+1 {
			t.Fatalf("final formatted Go token %d lost precise origin: %+v; stages=%v", i, s, compiled.Diagnostics.Stages)
		}
		depth := 2
		if i%3 == 2 {
			depth = 1
		}
		if len(s.Stack) != depth {
			t.Fatalf("final Go token %d lost expansion stack: %+v", i, s)
		}
		i++
	}
	if i != len(wantOrigins) {
		t.Fatalf("checked %d final Go tokens, want %d", i, len(wantOrigins))
	}
}

func TestSourceMappingChangedRepeatedTokensStayUnmapped(t *testing.T) {
	before := []byte("package fixture\nvar A=7\nvar B=7\n")
	after := []byte("package fixture\nvar B=7\n")
	m := sourcemap.Identity("deleted.gox", before).Compose(before, after, "delete-A")
	s := m.Lookup(bytes.Index(after, []byte("7")))
	if s == nil || s.Origin != nil || s.Kind != "generated/unmapped" {
		t.Fatalf("deleted/repeated token acquired fabricated origin: %+v", s)
	}
	raw := "# fixture\n./main.go:2:7: synthetic failure\nother.go:2:7: unrelated failure\n"
	diagnostic := m.Diagnostics(after, raw)
	if !strings.Contains(diagnostic, "generated/unmapped") || strings.Contains(diagnostic, "deleted.gox") || strings.Contains(diagnostic, "unrelated failure") {
		t.Fatalf("unmapped or unrelated compiler position was fabricated: %s", diagnostic)
	}
}

func TestSourceMappingDeriveFieldDiagnosticSpan(t *testing.T) {
	const source = `package fixture
type Missing struct{}
type Earlier struct{ Also Missing }
macro_rules! twice {($x:expr)=>{$x+$x};}
func noise()int{return twice!(7)}
#[derive(Clone)]
type Broken struct{
 Good int
 Bad Missing
}

`
	_, err := frontend.Compile("derive_span.gox", []byte(source))
	if err == nil {
		t.Fatal("accepted missing nested Clone implementation")
	}
	start := strings.LastIndex(source, "Missing")
	wantSpan := fmt.Sprintf("source span [%d,%d)", start, start+len("Missing"))
	if !strings.Contains(err.Error(), "derive_span.gox:9:6:") || !strings.Contains(err.Error(), wantSpan) {
		t.Fatalf("derive diagnostic must identify exact offending field type, not an earlier repeated name or declaration: %v; expected %s", err, wantSpan)
	}
	var diagnostic *sourcemap.Error
	if !errors.As(err, &diagnostic) || diagnostic.Origin == nil || diagnostic.Origin.Start != start || diagnostic.Origin.End != start+len("Missing") {
		t.Fatalf("missing structured exact field diagnostic: %#v", err)
	}
}

func TestSourceMappingDeriveFieldMetadata(t *testing.T) {
	const source = "package fixture\ntype Earlier struct{ Values []int }\n#[derive(Clone)]\ntype Item struct{ Values []int; Other []int }\n"
	result, err := frontend.Compile("fields.gox", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		Fields []struct {
			Model  string          `json:"model"`
			Field  string          `json:"field"`
			Origin *sourcemap.Span `json:"origin"`
		} `json:"deriveFields"`
	}
	if err := json.Unmarshal(result.SourceMap, &metadata); err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"Values": strings.Index(source, "type Item struct{ Values []int") + len("type Item struct{ Values "), "Other": strings.LastIndex(source, "[]int")}
	if len(metadata.Fields) != len(want) {
		t.Fatalf("unexpected derived fields: %s", result.SourceMap)
	}
	for _, f := range metadata.Fields {
		start, ok := want[f.Field]
		if !ok || f.Model != "Item" || f.Origin == nil || f.Origin.File != "fields.gox" || f.Origin.Start != start || f.Origin.End != start+len("[]int") {
			t.Fatalf("derived field incorrectly mapped: %+v, origin=%+v", f, f.Origin)
		}
		delete(want, f.Field)
	}
}

func sourceMappingGo(t *testing.T, dir string, args ...string) ([]byte, error) {
	t.Helper()
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin/go"), args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOCACHE=/tmp/goxide-macro-cache", "GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off")
	return cmd.CombinedOutput()
}

func TestSourceMappingUserHelperNameIsNotGeneratedProvenance(t *testing.T) {
	const source = "package fixture\n#[derive(Debug)]\ntype Item struct{ Value int }\nfunc CloneItem(v *Item)int{return v.Value}\n"
	result, err := frontend.Compile("user_helper.gox", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	offset := bytes.Index(result.Go, []byte("return v.Value")) + len("return ")
	origin := strings.LastIndex(source, "v.Value")
	s := result.Diagnostics.Lookup(offset)
	if s == nil || s.Origin == nil {
		t.Fatalf("user-written helper lost source origin: %+v", s)
	}
	if s.Origin.Start != origin || s.Kind == "derive field" {
		t.Fatalf("user-written CloneItem is not derived by Debug: kind=%s origin=%+v, want source offset %d", s.Kind, s.Origin, origin)
	}
}

// This intentionally reaches the real Go compiler: the ordinary Go input does
// not require the frontend's optional go/types pass. Comparing its stderr with
// CLI stderr makes preserving the original compiler evidence observable.
func TestSourceMappingRealCompilerCLI(t *testing.T) {
	const source = `package main
macro_rules! keep {($x:expr)=>{$x};}
func main(){
 _=keep!(7)
 var first int = "repeated"
 var second int = "repeated"
 _,_=first,second
}
`
	result, err := frontend.Compile("original.gox", []byte(source))
	if err != nil {
		t.Fatalf("fixture must reach the actual Go compiler: %v", err)
	}
	// The independent Go oracle must not give the standalone input a module.
	oracle := t.TempDir()
	for name, data := range map[string][]byte{"go.mod": []byte("module compilerfixture\n\ngo 1.27\n"), "main.go": result.Go} {
		if err := testdeps.WriteFile(filepath.Join(oracle, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := sourceMappingGo(t, oracle, "build", "-buildvcs=false", ".")
	if err == nil || bytes.Count(raw, []byte("cannot use")) != 2 {
		t.Fatalf("expected two actual compiler errors: %v\n%s", err, raw)
	}
	// Overlay builds may name a different generated .go file. Compare every
	// original diagnostic's exact generated line, column and full Go message;
	// source-map supplements cannot stand in for the raw compiler evidence.
	diagnostic := regexp.MustCompile(`(?m)^([^\r\n]+\.go):([0-9]+):([0-9]+): ([^\r\n]+)$`)
	type compilerDiagnostic struct{ file, positionMessage string }
	compilerErrors := func(output []byte) []compilerDiagnostic {
		var errors []compilerDiagnostic
		for _, match := range diagnostic.FindAllSubmatch(output, -1) {
			if bytes.HasPrefix(match[1], []byte("goxide: ")) {
				continue
			}
			errors = append(errors, compilerDiagnostic{string(match[1]), fmt.Sprintf("%s:%s: %s", match[2], match[3], match[4])})
		}
		return errors
	}
	wantErrors := compilerErrors(raw)
	if len(wantErrors) != 2 {
		t.Fatalf("expected two positioned Go diagnostics:\n%s", raw)
	}
	t.Logf("independent real Go compiler stderr:\n%s", raw)
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	artifacts := t.TempDir()
	cli := filepath.Join(artifacts, "goxide")
	if out, err := sourceMappingGo(t, root, "build", "-o", cli, "./cmd/goxide"); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, out)
	}
	for _, mode := range []string{"standalone", "configured"} {
		t.Run(mode, func(t *testing.T) {
			dir, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			input := filepath.Join(dir, "original.gox")
			if err := testdeps.WriteFile(input, []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			if mode == "configured" {
				if err := testdeps.WriteFile(filepath.Join(dir, "go.mod"), []byte("module compilerfixture\n\ngo 1.27\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command(cli, "build", "--runtime", root, "--go", filepath.Join(runtime.GOROOT(), "bin/go"), "-o", filepath.Join(artifacts, mode+"-program"), input)
			cmd.Dir = root
			cmd.Env = append(os.Environ(), "GOCACHE=/tmp/goxide-macro-cache", "GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off")
			output, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatal("CLI build accepted invalid Go")
			}
			gotErrors := compilerErrors(output)
			if len(gotErrors) != len(wantErrors) {
				t.Fatalf("CLI lost raw compiler diagnostics: got %q, want %q:\n%s", gotErrors, wantErrors, output)
			}
			for i, want := range wantErrors {
				if gotErrors[i].positionMessage != want.positionMessage {
					t.Fatalf("CLI changed raw compiler position/message: got %q, want %q:\n%s", gotErrors[i].positionMessage, want.positionMessage, output)
				}
				if gotErrors[i].file != gotErrors[0].file {
					t.Fatalf("CLI diagnostics came from different generated files: %q:\n%s", gotErrors, output)
				}
			}
			if mode == "standalone" {
				for _, line := range strings.Split(string(raw), "\n") {
					if strings.Contains(line, "cannot use") && !bytes.Contains(output, []byte(line)) {
						t.Fatalf("CLI lost original compiler stderr %q:\n%s", line, output)
					}
				}
			}
			for _, line := range []int{5, 6} {
				column := strings.Index(strings.Split(source, "\n")[line-1], `"repeated"`) + 1
				want := fmt.Sprintf("goxide: %s:%d:%d: ", input, line, column)
				if bytes.Count(output, []byte(want)) != 1 {
					t.Fatalf("expected one precise mapped occurrence %q:\n%s", want, output)
				}
			}
			t.Logf("real compiler stderr retained with mapped diagnostics:\n%s", output)
		})
	}
}

func TestSourceMappingRealCompilerGeneratedHelperStaysUnmapped(t *testing.T) {
	const source = `package main
func len()int{return 0}
#[derive(Clone)]
type Item struct{ Values []int }
func main(){}
`
	result, err := frontend.Compile("generated_helper.gox", []byte(source))
	if err != nil {
		t.Fatalf("fixture must reach generated helper compiler error: %v", err)
	}
	dir := t.TempDir()
	for name, data := range map[string][]byte{"go.mod": []byte("module helperfixture\n\ngo 1.27\n"), "main.go": result.Go} {
		if err := testdeps.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := sourceMappingGo(t, dir, "build", "-buildvcs=false", ".")
	if err == nil || !bytes.Contains(raw, []byte("too many arguments in call to len")) {
		t.Fatalf("expected actual generated helper compiler failure: %v\n%s", err, raw)
	}
	mapped := result.Diagnostics.Diagnostics(result.Go, string(raw))
	if !strings.Contains(mapped, "generated/unmapped") || strings.Contains(mapped, "generated_helper.gox") {
		t.Fatalf("generated helper error acquired fabricated user position:\nraw=%s\nmapped=%s", raw, mapped)
	}
	t.Logf("real generated helper error:\n%s%s", raw, mapped)
}
