package propagate

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/enums"
	"github.com/hxsf/goxide/internal/refs"
	"github.com/hxsf/goxide/internal/testdeps"
	"github.com/hxsf/goxide/internal/typecheck"
)

// Exercises the real enum/propagation passes and independently executes their
// output. ProjectOk is a native Go API here, not acceptance of source ref syntax.
func TestInlineSumPropagationRace(t *testing.T) {
	source := []byte(`package main
import s "github.com/goxide-lang/std/sum"
var trace string
func option(v Option[[]int]) Option[int] {trace+="O";xs:=v?;trace+="V";return Option[int]::Some(len(xs))}
func result(v Result[[]int,error]) Result[int,error] {trace+="R";xs:=v?;trace+="V";return Result[int,error]::Ok(len(xs))}
func invalid(){defer func(){if recover()==nil{panic("invalid zero Result accepted")}}();var zero Result[[]int,error];result(zero)}
func main(){
 if s.TagOption(option(Option[[]int]::None))!=0||trace!="O"{panic("None propagation")};trace=""
 if s.ValueSome(option(Option[[]int]::Some([]int{1,2})))!=2||trace!="OV"{panic("Some propagation")};trace=""
 r:=Result[[]int,error]::Ok([]int{1});before:=r;p:=s.ProjectOk(&r);*p=[]int{2,3}
 if s.ValueOk(result(r))!=2||trace!="RV"{panic("active payload")};trace=""
 if s.ValueOk(result(before))!=1{panic("shallow value copy")};trace=""
 r=Result[[]int,error]::Err(nil);*p=[]int{9};out:=result(r)
 if s.TagResult(out)!=2||s.ValueErr(out)!=nil||trace!="R"{panic("inactive slot or nil Err changed")};trace=""
 invalid();if trace!="R"{panic("validation occurred after success effects")}
}


`)
	prepared, err := Prepare("inline.gox", source)
	if err != nil {
		t.Fatal(err)
	}
	lowered, err := enums.Lower("inline.gox", prepared)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Lower("inline.gox", lowered.Source, Config{Importer: typecheck.NewImporter("input.gox"), Families: lowered.Enums})
	if err != nil {
		t.Fatalf("%v\n%s", err, lowered.Source)
	}
	if strings.Contains(string(out), ".(type)") || strings.Contains(string(out), "__goxide_enum_case_") {
		t.Fatal("generated propagation retains the removed case ABI")
	}
	dir := t.TempDir()
	if err := testdeps.WriteFile(filepath.Join(dir, "go.mod"), []byte("module inline.test\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := testdeps.WriteFile(filepath.Join(dir, "main.go"), out, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", "-race", ".")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s\n%s", err, output, out)
	}
}

// A deferred, shorthand pattern must retain the original place and its default
// binding mode through enum inference, projection binding and reference erasure.
func TestDeferredMatchKeepsOriginalPlaceRace(t *testing.T) {
	for _, tc := range []struct{ name, mode, pattern, body, expected string }{
		{"explicit-write", "", "ref mut x", "*x=7", "7"},
		{"automatic-write", "&mut ", "x", "*x=7", "7"},
		{"automatic-read", "&", "x", "if *x!=1{panic(\"wrong read payload\")}", "1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const filename = "deferred.gox"
			const packagePath = "goxide/generated"
			source := []byte(`package main
import s "github.com/goxide-lang/std/sum"
func place(e &mut Option[int],calls &mut int) &mut Option[int]{*calls+=1;return e}
func main(){e:=Option[int]::Some(1);calls:=0
match ` + tc.mode + `*place(&mut e,&mut calls) {Option::Some(` + tc.pattern + `)=>{` + tc.body + `},Option::None=>{}}
if s.ValueSome(e)!=` + tc.expected + `||calls!=1{panic("deferred match detached or repeated place")}
}`)
			enumResult, err := enums.Lower(filename, source)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := refs.Prepare(filename, enumResult.Source)
			if err != nil {
				t.Fatal(err)
			}
			imp := typecheck.NewImporter("input.gox")
			referenceTemps := enumResult.ReferenceTemps
			if referenceTemps == nil {
				referenceTemps = map[string]bool{}
			}
			out, err := Lower(filename, prepared.Source, Config{Importer: imp, Families: enumResult.Enums, PackagePath: packagePath, MatchReferenceTemps: referenceTemps})
			if err != nil {
				t.Fatalf("%v\n%s", err, prepared.Source)
			}
			registry := []refs.Projection{{PackagePath: packagePath, Function: "__goxide_enum_project_Option_Some", EnumTypeID: "github.com/goxide-lang/std/sum.Option[int]", PayloadTypeID: "int"}}
			bound, bindings, err := refs.BindProjectionsWithPath(packagePath, map[string][]byte{filename: out}, imp, registry)
			if err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
			finished, err := refs.FinishWithEnumTypes(packagePath, filename, bound[filename], imp, map[string]bool{"github.com/goxide-lang/std/sum.Option[int]": true}, referenceTemps)
			if err != nil {
				t.Fatalf("%v\n%s", err, bound[filename])
			}
			out, err = bindings.Restore(filename, finished.Source)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			if err := testdeps.WriteFile(filepath.Join(dir, "go.mod"), []byte("module deferred.test\ngo 1.27\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := testdeps.WriteFile(filepath.Join(dir, "main.go"), out, 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("go", "run", "-race", ".")
			cmd.Dir = dir
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v\n%s\n%s", err, output, out)
			}
		})
	}
}
