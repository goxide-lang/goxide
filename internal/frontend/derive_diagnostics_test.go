package frontend

import (
	"errors"
	"github.com/hxsf/goxide/internal/sourcemap"
	"strings"
	"testing"
)

func TestDeriveDiagnosticAfterMacroExpansion(t *testing.T) {
	const prefix = `package p
macro_rules! noise {()=>{
 _=1
 _=2
 _=3
};}
func Run(){noise!();noise!()}
`
	for _, tc := range []struct{ name, body, target, message string }{
		{"unknown derive", "#[derive(Unknown)]\ntype Item struct{N int}\n", "#[derive(Unknown)]", "unsupported derive Unknown"},
		{"duplicate derive", "#[derive(Clone,Clone)]\ntype Item struct{N int}\n", "#[derive(Clone,Clone)]", "duplicate derive Clone"},
		{"helper conflict", "func CloneItem(){}\n#[derive(Clone)]\ntype Item struct{N int}\n", "type Item", "generated helper conflicts"},
		{"unknown field attribute", "#[derive(Clone)]\ntype Item struct{\n #[unknown]\n N int\n}\n", "#[unknown]", "unsupported attribute"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, newline := range []string{"\n", "\r\n"} {
				source := strings.ReplaceAll(prefix+tc.body, "\n", newline)
				_, err := Compile("derive.gox", []byte(source))
				var d *sourcemap.Error
				start := strings.Index(source, tc.target)
				if !errors.As(err, &d) || d.Origin == nil || d.Origin.File != "derive.gox" || d.Origin.Start != start || d.Origin.Line != strings.Count(source[:start], "\n")+1 || !strings.Contains(d.Message, tc.message) {
					t.Fatalf("expected original %q at %d, got %v", tc.target, start, err)
				}
			}
		})
	}
}

func TestDeriveDiagnosticPackageFile(t *testing.T) {
	source := "package p\nfunc Run(){noise!();noise!()}\n#[derive(Unknown)]\ntype Item struct{N int}\n"
	_, err := CompilePackage("example/p", map[string][]byte{
		"macros.gox": []byte("package p\nmacro_rules! noise {()=>{\n_=1\n_=2\n};}\n"),
		"item.gox":   []byte(source),
	}, nil)
	var d *sourcemap.Error
	if !errors.As(err, &d) || d.Origin == nil || d.Origin.File != "item.gox" || d.Origin.Line != 3 || d.Origin.Start != strings.Index(source, "#[derive") {
		t.Fatalf("wrong package origin: %v", err)
	}
}

func TestMacroDiagnosticPreparedCoordinates(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		source := strings.ReplaceAll("package p\nimport \"fmt\"\nmacro_rules! bad {($v:expr)=>{_=$v?;_=fmt.Println};}\nfunc Run(){bad!(1)}\n", "\n", newline)
		for _, packaged := range []bool{false, true} {
			var err error
			if packaged {
				_, err = CompilePackage("example/p", map[string][]byte{"prepare.gox": []byte(source)}, nil)
			} else {
				_, err = Compile("prepare.gox", []byte(source))
			}
			var d *sourcemap.Error
			start := strings.Index(source, "Println")
			if !errors.As(err, &d) || d.Origin == nil || d.Origin.Start != start || d.Origin.Column != 43 || len(d.Stack) != 1 || d.Stack[0].Callsite.Line != 4 || d.Stack[0].Callsite.Start != strings.Index(source, "bad!(1)") {
				t.Fatalf("packaged=%v: wrong prepared diagnostic: %v", packaged, err)
			}
		}
	}
}

func TestDeriveDiagnosticEnumStage(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		source := "package p\nenum First {\n" + strings.Repeat("\n", 20) + " A,\n}\n#[derive(Error)]\nenum E { A, }\n" + strings.Repeat("var Z int\n", 40)
		source = strings.ReplaceAll(source, "\n", newline)
		_, err := Compile("enum.gox", []byte(source))
		var d *sourcemap.Error
		if !errors.As(err, &d) || d.Origin == nil || d.Origin.Start != strings.Index(source, "enum E") || d.Origin.Line != 26 || !strings.Contains(d.Message, "enum Error requires") {
			t.Fatalf("wrong enum stage: %v", err)
		}
	}
}

func TestMacroDiagnosticNestedPreparedCoordinates(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		defs := strings.ReplaceAll("package p\nimport \"fmt\"\nmacro_rules! inner {($v:expr)=>{_=$v?;_=fmt.Println};}\nmacro_rules! outer {($v:expr)=>{inner!($v)};}\n", "\n", newline)
		caller := strings.ReplaceAll("package p\nfunc Run(){_=Next()?;outer!(1)}\n", "\n", newline)
		_, err := CompilePackage("example/p", map[string][]byte{"definitions.gox": []byte(defs), "caller.gox": []byte(caller)}, nil)
		var d *sourcemap.Error
		if !errors.As(err, &d) || d.Origin == nil || d.Origin.File != "definitions.gox" || d.Origin.Start != strings.Index(defs, "Println") || len(d.Stack) != 2 {
			t.Fatalf("bad %v", err)
		}
		for _, frame := range d.Stack {
			for _, span := range []sourcemap.Span{frame.Definition, frame.Callsite} {
				source := caller
				if span.File == "definitions.gox" {
					source = defs
				}
				if span.Start < 0 || span.End > len(source) || span.Start >= span.End {
					t.Fatalf("bad span %+v", span)
				}
				want := "macro_rules! " + frame.Macro
				if span == frame.Callsite {
					want = frame.Macro + "!("
				}
				if !strings.HasPrefix(source[span.Start:span.End], want) {
					t.Fatalf("wrong frame %+v: %q", frame, source[span.Start:span.End])
				}
				if span.Line != strings.Count(source[:span.Start], "\n")+1 || span.Column != span.Start-strings.LastIndex(source[:span.Start], "\n") {
					t.Fatalf("wrong frame coordinates %+v", span)
				}
			}
		}

	}
}
