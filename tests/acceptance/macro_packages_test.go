package acceptance_test

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/frontend"
	"github.com/hxsf/goxide/internal/sourcemap"
)

func macroPackageSource(t *testing.T) string {
	t.Helper()
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	source := filepath.Join(t.TempDir(), "source")
	example := filepath.Join(root, "examples/macro_packages")
	if err := filepath.WalkDir(example, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(example, path)
		if err != nil {
			return err
		}
		target := filepath.Join(source, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0600)
	}); err != nil {
		t.Fatal(err)
	}
	packagesWrite(t, source, "go.mod", fmt.Sprintf("module macros.example/app\ngo 1.27\nrequire github.com/hxsf/goxide v0.0.0\nreplace github.com/hxsf/goxide => %q\n", filepath.ToSlash(root)))
	return source
}

func TestMacroPackagesApplicationRace(t *testing.T) {
	source := macroPackageSource(t)
	generated := filepath.Join(t.TempDir(), "generated")
	if _, err := frontend.CompileModule(source, generated); err != nil {
		t.Fatal(err)
	}
	packagesWrite(t, generated, "macro_test.go", macroPackageAssertions)
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	packagesCommand(t, generated, tool, true, "test", "-race", "-count=1", "-v", "./...")
	binary := filepath.Join(t.TempDir(), "application")
	packagesCommand(t, generated, tool, true, "build", "-buildvcs=false", "-o", binary, ".")
	output := packagesCommand(t, generated, binary, true)
	if !strings.Contains(output, `"msg":"request"`) || !strings.Contains(output, "workers 4 <nil> 102") {
		t.Fatal(output)
	}
}

func TestMacroPackagesPrivateAndBudgets(t *testing.T) {
	cases := []struct{ name, definition, main, want string }{
		{"private-runtime", `package logkit
func hidden() int{return 7}
pub macro_rules! secret { () => { hidden() }; }
`, `package main
import m "macros.example/app/logkit"
func main(){_=m.secret!()}
`, "private"},
		{"private-macro", `package logkit
macro_rules! secret { () => { 7 }; }
`, `package main
import m "macros.example/app/logkit"
func main(){_=m.secret!()}
`, "macro"},
		{"recursive", `package logkit
pub macro_rules! forever { () => { forever!() }; }
`, `package main
import m "macros.example/app/logkit"
func main(){_=m.forever!()}
`, "budget"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := t.TempDir()
			packagesWrite(t, source, "go.mod", "module macros.example/app\ngo 1.27\n")
			packagesWrite(t, source, "logkit/macros.hgo", tc.definition)
			packagesWrite(t, source, "main.hgo", tc.main)
			_, err := frontend.CompileModule(source, filepath.Join(t.TempDir(), "generated"))
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), tc.want) {
				t.Fatalf("expected %s rejection: %v", tc.want, err)
			}
			if tc.name == "private-runtime" && (!strings.Contains(err.Error(), "main.hgo") || !strings.Contains(err.Error(), "macros.hgo")) {
				t.Fatalf("missing definition/callsite: %v", err)
			}
		})
	}
}

func TestMacroPackagesDependencyCycle(t *testing.T) {
	source := t.TempDir()
	packagesWrite(t, source, "go.mod", "module macros.example/app\ngo 1.27\n")
	packagesWrite(t, source, "a/a.hgo", "package a\nimport b \"macros.example/app/b\"\npub macro_rules! value {()=>{b.value!()};}\n")
	packagesWrite(t, source, "b/b.hgo", "package b\nimport a \"macros.example/app/a\"\npub macro_rules! value {()=>{a.value!()};}\n")
	_, err := frontend.CompileModule(source, filepath.Join(t.TempDir(), "generated"))
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("dependency cycle accepted: %v", err)
	}
}

func TestMacroPackagesDefinitionDiagnostic(t *testing.T) {
	source := t.TempDir()
	packagesWrite(t, source, "go.mod", "module macros.example/app\ngo 1.27\n")
	packagesWrite(t, source, "logkit/macros.hgo", `package logkit
func Emit(value string) {}
pub macro_rules! broken { () => { Emit(71) }; }
`)
	packagesWrite(t, source, "main.hgo", `package main
import m "macros.example/app/logkit"
func main(){m.broken!()}
`)
	_, err := frontend.CompileModule(source, filepath.Join(t.TempDir(), "generated"))
	var mapped *sourcemap.Error
	if !errors.As(err, &mapped) || mapped.Origin == nil || filepath.Base(mapped.Origin.File) != "macros.hgo" || mapped.Origin.Line != 3 {
		t.Fatalf("missing true definition source: %v", err)
	}
	found := false
	for _, frame := range mapped.Stack {
		if filepath.Base(frame.Callsite.File) == "main.hgo" && frame.Callsite.Line == 3 {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing consumer macro call: %#v", mapped.Stack)
	}
}

const macroPackageAssertions = `package main
import (
 "bytes"
 "encoding/json"
 "errors"
 "log/slog"
 "strings"
 "testing"
 logs "macros.example/app/logkit"
)
func TestConditionalLogHygieneAndAliases(t *testing.T) {
 var out bytes.Buffer
 old:=slog.Default();defer slog.SetDefault(old)
 slog.SetDefault(slog.New(slog.NewJSONHandler(&out,nil)))
 trace:=""
 enabled:=func()bool{trace+="E";return false}
 message:=func()string{trace+="M";return "business"}
 field:=func()int{trace+="F";return 17}
 emit(enabled,message,field)
 if trace!="E"||out.Len()!=0{t.Fatalf("disabled evaluation: %q %s",trace,out.String())}
 trace=""
 emit(func()bool{trace+="E";return true},message,field)
 if trace!="EMF"{t.Fatal(trace)}
 var event map[string]any
 if err:=json.Unmarshal(out.Bytes(),&event);err!=nil{t.Fatal(err)}
 if event["msg"]!="business"||event["request_id"]!=float64(17){t.Fatal(event)}
 out.Reset();secondAlias(true)
 if !strings.Contains(out.String(),"renamed"){t.Fatal(out.String())}
 if otherMacro(2)!=102{t.Fatal("same named macro package identity lost")}
}
func TestImportedMacroQuestionAndErrorChain(t *testing.T) {
 if n,e:=unpack(process(false,false));n!=4||e!=nil{t.Fatal(n,e)}
 _,e:=unpack(process(false,true));if !errors.Is(e,logs.Sentinel){t.Fatal(e)}
}
`

func TestMacroPackagesWholeBuildBudget(t *testing.T) {
	source := t.TempDir()
	packagesWrite(t, source, "go.mod", "module macros.example/app\ngo 1.27\n")
	// Each package is below the limit; the module must share the counter.
	for _, name := range []string{"a", "b"} {
		packagesWrite(t, source, name+"/macros.hgo", "package "+name+"\nmacro_rules! one {()=>{1};}\nfunc Run(){"+strings.Repeat("_=one!();", 2200)+"}\n")
	}
	_, err := frontend.CompileModule(source, filepath.Join(t.TempDir(), "generated"))
	if err == nil || !strings.Contains(err.Error(), "expansion count budget") {
		t.Fatalf("module budget: %v", err)
	}
}

func TestMacroPackagesPreparedDefinitionDiagnostic(t *testing.T) {
	for _, sameFile := range []bool{false, true} {
		t.Run(fmt.Sprintf("same-file-%t", sameFile), func(t *testing.T) {
			source := t.TempDir()
			packagesWrite(t, source, "go.mod", "module macros.example/app\ngo 1.27\n")
			definition := `package logkit
func first() Result[int,error] { return Result[int,error]::Ok(1) }
func prepared() Result[int,error] { n:=first()?; return Result[int,error]::Ok(n) }
func Emit(value string) {}
pub macro_rules! broken { () => { Emit(71) }; }
`
			caller := `package main
import m "macros.example/app/logkit"
func main(){m.broken!()}
`
			callLine := 3
			definitionFile := "macros.hgo"
			if sameFile {
				definition = strings.Replace(definition, "package logkit", "package main", 1) + "func main(){broken!()}\n"
				packagesWrite(t, source, "main.hgo", definition)
				callLine = 6
				definitionFile = "main.hgo"
			} else {
				packagesWrite(t, source, "logkit/macros.hgo", definition)
				packagesWrite(t, source, "main.hgo", caller)
			}
			_, err := frontend.CompileModule(source, filepath.Join(t.TempDir(), "generated"))
			var mapped *sourcemap.Error
			offset := strings.Index(definition, "71")
			if !errors.As(err, &mapped) || mapped.Origin == nil || filepath.Base(mapped.Origin.File) != definitionFile || mapped.Origin.Start != offset || mapped.Origin.End != offset+2 || mapped.Origin.Line != 5 {
				t.Fatalf("lost original literal after preparation: %v (%#v)", err, mapped)
			}
			if len(mapped.Stack) != 1 || filepath.Base(mapped.Stack[0].Callsite.File) != "main.hgo" || mapped.Stack[0].Callsite.Line != callLine || mapped.Stack[0].Definition.Line != 5 {
				t.Fatalf("wrong original expansion stack: %#v", mapped.Stack)
			}
		})
	}
}

func TestMacroPackagesSameFileAliases(t *testing.T) {
	source := t.TempDir()
	packagesWrite(t, source, "go.mod", "module macros.example/app\ngo 1.27\n")
	packagesWrite(t, source, "dep/macros.hgo", `package dep
func Emit() int { return 7 }
pub macro_rules! value {()=>{Emit()};}
`)
	packagesWrite(t, source, "main.hgo", `package main
import (
 a "macros.example/app/dep"
 b "macros.example/app/dep"
)
func main(){_=a.value!();_=b.value!()}
`)
	if _, err := frontend.CompileModule(source, filepath.Join(t.TempDir(), "generated")); err != nil {
		t.Fatal(err)
	}
}
