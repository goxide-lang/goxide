package acceptance_test

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/rootconfig"
	testdeps "github.com/goxide-lang/goxide/tests/testutil"
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
		return testdeps.WriteFile(target, data, 0600)
	}); err != nil {
		t.Fatal(err)
	}
	packagesWrite(t, source, "go.mod", "module macros.example/app\ngo 1.27\n"+stdFixtureGoRequirements(t))

	return source
}

func macroPackageCompiler(t *testing.T) func(source, destination string) error {
	t.Helper()
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	cli := acceptanceCLI(t)
	return func(source, destination string) error {
		command := exec.Command(cli, "transpile", "-o", destination, source)
		command.Dir = root
		command.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off")
		output, err := command.CombinedOutput()
		if err != nil {
			return fmt.Errorf("macro package CLI: %w\n%s", err, output)
		}
		return nil
	}
}

func TestMacroPackagesApplicationRace(t *testing.T) {
	compile := macroPackageCompiler(t)
	source := macroPackageSource(t)
	generated := filepath.Join(t.TempDir(), "generated")
	if err := compile(source, generated); err != nil {
		t.Fatal(err)
	}
	again := filepath.Join(t.TempDir(), "generated")
	if err := compile(source, again); err != nil {
		t.Fatal(err)
	}
	singleExternalSameTree(t, generated, again)
	packagesWrite(t, generated, "macro_test.go", macroPackageAssertions)
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	packagesCommand(t, generated, tool, true, "test", "-race", "-count=1", "-v", "./...")
	binary := filepath.Join(t.TempDir(), "application")
	packagesCommand(t, generated, tool, true, "build", "-buildvcs=false", "-o", binary, ".")
	output := packagesCommand(t, generated, binary, true)
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 2 || lines[1] != "workers 4 <nil> 102" {
		t.Fatalf("macro_packages output = %q", lines)
	}
	var event map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &event); err != nil {
		t.Fatal(err)
	}
	if event["msg"] != "request" || event["request_id"] != float64(42) || event["workers"] != float64(4) {
		t.Fatalf("macro log event = %#v", event)
	}
}

func TestMacroPackagesPrivateAndBudgets(t *testing.T) {
	compile := macroPackageCompiler(t)
	cases := []struct{ name, definition, main, want string }{
		{"private-runtime", `package logkit
func hidden() int{return 7}
pub macro_rules! secret { () => { hidden() }; }
`, `package main
use logkit as m;
func main(){_=m::secret!()}
`, "私有成员 hidden"},
		{"private-macro", `package logkit
macro_rules! secret { () => { 7 }; }
`, `package main
use logkit as m;
func main(){_=m::secret!()}
`, "macro"},
		{"recursive", `package logkit
pub macro_rules! forever { () => { forever!() }; }
`, `package main
use logkit as m;
func main(){_=m::forever!()}
`, "budget"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := t.TempDir()
			packagesWrite(t, source, "go.mod", "module macros.example/app\ngo 1.27\n"+stdFixtureGoRequirements(t))
			writeMacroFixtureRoots(t, source, map[string]rootconfig.Mapping{
				"a":      {Module: "macros.example/app", Package: "macros.example/app/a"},
				"b":      {Module: "macros.example/app", Package: "macros.example/app/b"},
				"dep":    {Module: "macros.example/app", Package: "macros.example/app/dep"},
				"logkit": {Module: "macros.example/app", Package: "macros.example/app/logkit"},
				"other":  {Module: "macros.example/app", Package: "macros.example/app/other"},
			})
			packagesWrite(t, source, "logkit/macros.gox", tc.definition)
			packagesWrite(t, source, "main.gox", tc.main)
			err := compile(source, filepath.Join(t.TempDir(), "generated"))
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), tc.want) {
				t.Fatalf("expected %s rejection: %v", tc.want, err)
			}
			if tc.name == "private-runtime" && (!strings.Contains(err.Error(), "main.gox") || !strings.Contains(err.Error(), "macros.gox")) {
				t.Fatalf("missing definition/callsite: %v", err)
			}
		})
	}
}

func TestMacroPackagesDependencyCycle(t *testing.T) {
	compile := macroPackageCompiler(t)
	source := t.TempDir()
	packagesWrite(t, source, "go.mod", "module macros.example/app\ngo 1.27\n"+stdFixtureGoRequirements(t))
	writeMacroFixtureRoots(t, source, map[string]rootconfig.Mapping{
		"a":      {Module: "macros.example/app", Package: "macros.example/app/a"},
		"b":      {Module: "macros.example/app", Package: "macros.example/app/b"},
		"dep":    {Module: "macros.example/app", Package: "macros.example/app/dep"},
		"logkit": {Module: "macros.example/app", Package: "macros.example/app/logkit"},
		"other":  {Module: "macros.example/app", Package: "macros.example/app/other"},
	})
	packagesWrite(t, source, "a/a.gox", "package a\nuse b;\npub macro_rules! value {()=>{b::value!()};}\n")
	packagesWrite(t, source, "b/b.gox", "package b\nuse a;\npub macro_rules! value {()=>{a::value!()};}\n")
	packagesWrite(t, source, "main.gox", "package main\nuse a;\nfunc main(){_=a::value!()}\n")
	err := compile(source, filepath.Join(t.TempDir(), "generated"))
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("dependency cycle accepted: %v", err)
	}
}

func TestMacroPackagesDefinitionTypeError(t *testing.T) {
	compile := macroPackageCompiler(t)
	source := t.TempDir()
	packagesWrite(t, source, "go.mod", "module macros.example/app\ngo 1.27\n"+stdFixtureGoRequirements(t))
	writeMacroFixtureRoots(t, source, map[string]rootconfig.Mapping{
		"a":      {Module: "macros.example/app", Package: "macros.example/app/a"},
		"b":      {Module: "macros.example/app", Package: "macros.example/app/b"},
		"dep":    {Module: "macros.example/app", Package: "macros.example/app/dep"},
		"logkit": {Module: "macros.example/app", Package: "macros.example/app/logkit"},
		"other":  {Module: "macros.example/app", Package: "macros.example/app/other"},
	})
	packagesWrite(t, source, "logkit/macros.gox", `package logkit
func Emit(value string) {}
pub macro_rules! broken { () => { Emit(71) }; }
`)
	packagesWrite(t, source, "main.gox", `package main
use logkit as m;
func main(){m::broken!()}
`)
	err := compile(source, filepath.Join(t.TempDir(), "generated"))
	if err == nil || !strings.Contains(err.Error(), "cannot use") {
		t.Fatalf("accepted invalid argument from macro definition: %v", err)
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
	compile := macroPackageCompiler(t)
	source := t.TempDir()
	packagesWrite(t, source, "go.mod", "module macros.example/app\ngo 1.27\n"+stdFixtureGoRequirements(t))
	writeMacroFixtureRoots(t, source, map[string]rootconfig.Mapping{
		"a":      {Module: "macros.example/app", Package: "macros.example/app/a"},
		"b":      {Module: "macros.example/app", Package: "macros.example/app/b"},
		"dep":    {Module: "macros.example/app", Package: "macros.example/app/dep"},
		"logkit": {Module: "macros.example/app", Package: "macros.example/app/logkit"},
		"other":  {Module: "macros.example/app", Package: "macros.example/app/other"},
	})
	// Each package is below the limit; the module must share the counter.
	for _, name := range []string{"a", "b"} {
		packagesWrite(t, source, name+"/macros.gox", "package "+name+"\nmacro_rules! one {()=>{1};}\nfunc Run(){"+strings.Repeat("_=one!();", 2200)+"}\n")
	}
	err := compile(source, filepath.Join(t.TempDir(), "generated"))
	if err == nil || !strings.Contains(err.Error(), "expansion count budget") {
		t.Fatalf("module budget: %v", err)
	}
}

func TestMacroPackagesPreparedDefinitionTypeError(t *testing.T) {
	compile := macroPackageCompiler(t)
	for _, sameFile := range []bool{false, true} {
		t.Run(fmt.Sprintf("same-file-%t", sameFile), func(t *testing.T) {
			source := t.TempDir()
			packagesWrite(t, source, "go.mod", "module macros.example/app\ngo 1.27\n"+stdFixtureGoRequirements(t))
			writeMacroFixtureRoots(t, source, map[string]rootconfig.Mapping{
				"a":      {Module: "macros.example/app", Package: "macros.example/app/a"},
				"b":      {Module: "macros.example/app", Package: "macros.example/app/b"},
				"dep":    {Module: "macros.example/app", Package: "macros.example/app/dep"},
				"logkit": {Module: "macros.example/app", Package: "macros.example/app/logkit"},
				"other":  {Module: "macros.example/app", Package: "macros.example/app/other"},
			})
			definition := `package logkit
func first() Result[int,error] { return Result[int,error]::Ok(1) }
func prepared() Result[int,error] { n:=first()?; return Result[int,error]::Ok(n) }
func Emit(value string) {}
pub macro_rules! broken { () => { Emit(71) }; }
`
			caller := `package main
use logkit as m;
func main(){m::broken!()}
`
			if sameFile {
				definition = strings.Replace(definition, "package logkit", "package main", 1) + "func main(){broken!()}\n"
				packagesWrite(t, source, "main.gox", definition)
			} else {
				packagesWrite(t, source, "logkit/macros.gox", definition)
				packagesWrite(t, source, "main.gox", caller)
			}
			err := compile(source, filepath.Join(t.TempDir(), "generated"))
			if err == nil || !strings.Contains(err.Error(), "cannot use") {
				t.Fatalf("accepted invalid argument after macro preparation: %v", err)
			}
		})
	}
}

func TestMacroPackagesSameFileAliases(t *testing.T) {
	compile := macroPackageCompiler(t)
	source := t.TempDir()
	packagesWrite(t, source, "go.mod", "module macros.example/app\ngo 1.27\n"+stdFixtureGoRequirements(t))
	writeMacroFixtureRoots(t, source, map[string]rootconfig.Mapping{
		"a":      {Module: "macros.example/app", Package: "macros.example/app/a"},
		"b":      {Module: "macros.example/app", Package: "macros.example/app/b"},
		"dep":    {Module: "macros.example/app", Package: "macros.example/app/dep"},
		"logkit": {Module: "macros.example/app", Package: "macros.example/app/logkit"},
		"other":  {Module: "macros.example/app", Package: "macros.example/app/other"},
	})
	packagesWrite(t, source, "dep/macros.gox", `package dep
func Emit() int { return 7 }
pub macro_rules! value {()=>{Emit()};}
`)
	packagesWrite(t, source, "main.gox", `package main
use dep as a;
use dep as b;
func main(){_=a::value!();_=b::value!()}
`)
	if err := compile(source, filepath.Join(t.TempDir(), "generated")); err != nil {
		t.Fatal(err)
	}
}
