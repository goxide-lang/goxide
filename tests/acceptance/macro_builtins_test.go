package acceptance_test

import (
	"github.com/hxsf/goxide/internal/rootconfig"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/frontend"
)

func TestMacroBuiltinsModuleRace(t *testing.T) {
	source := t.TempDir()
	packagesWrite(t, source, "go.mod", "module builtins.example/app\ngo 1.27\n")
	writeMacroFixtureRoots(t, source, map[string]rootconfig.Mapping{
		"ops":    {Module: "builtins.example/app", Package: "builtins.example/app/ops"},
		"native": {Module: "builtins.example/app", Package: "builtins.example/app/native"},
	})
	packagesWrite(t, source, "ops/macros.gox", `package ops
pub macro_rules! size {($x:expr)=>{len($x)};}
pub macro_rules! space {($x:expr)=>{cap($x)};}
pub macro_rules! nested {($x:expr)=>{size!($x)+space!($x)};}
pub macro_rules! question {($x:expr)=>{len($x?)};}
`)
	packagesWrite(t, source, "native/model.go", "package native\ntype Mapping map[int]int\n")
	packagesWrite(t, source, "generic.go", "package app\ntype GenericMapping[K comparable] map[K]int\n")
	packagesWrite(t, source, "types.gox", "package app\nuse native as n;\ntype Record struct {len int}\ntype Mapping = n::Mapping\n")
	packagesWrite(t, source, "app.gox", `package app
use ops as m;
use strings as len;
const N=m::size!("abc")
var Calls int
func next(ok bool)Option[string]{Calls++;if ok{return Option[string]::Some("abc")};return Option[string]::None}
func Question(ok bool)Option[int]{return Option[int]::Some(m::question!(next(ok)))}
func Unwrap(ok bool)int{return match Question(ok){Option[int]::Some(n)=>n,Option[int]::None=>-1,}}
func Run()(int,int,int){
 text:=len::Repeat("x",2)
 len:=func()[]byte{return []byte(text)}
 cap:=123;_=cap
 data:=len()
 {len:=5;g:=GenericMapping[int]{len:7};if m::size!([]int{g[len]})!=1{panic("generic key")}}
 {len:=5;v:=Mapping{len:7};if m::size!([]int{v[len]})!=1||v[len]!=7{panic("cross-file map key")}}
 {len:=5;v:=Record{len:len};if m::size!([]int{v.len})+v.len!=6{panic("field binding")}}
 return m::size!(data),m::nested!(data),N
}
`)
	generated := filepath.Join(t.TempDir(), "generated")
	if _, err := frontend.CompileModule(source, generated); err != nil {
		t.Fatal(err)
	}
	packagesWrite(t, generated, "app_test.go", `package app
import "testing"
func TestBuiltins(t *testing.T){a,b,c:=Run();if a!=2||b!=4||c!=3{t.Fatal(a,b,c)};Calls=0;if Unwrap(true)!=3||Calls!=1{t.Fatal(Calls)};if Unwrap(false)!=-1||Calls!=2{t.Fatal(Calls)}}
`)
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	packagesCommand(t, generated, tool, true, "test", "-race", "-count=1", "-v", "./...")
	packagesCommand(t, generated, tool, true, "vet", "./...")
}

func TestMacroBuiltinInvalidCallsAndPackageShadow(t *testing.T) {
	for _, tc := range []struct{ decl, body, want string }{
		{"", `m::space!("abc")`, "cap"},
		{"", `m::size!(1)`, "len"},
		{"func len(string)int{return 99}", `m::size!("abc")`, "caller package declaration"},
		{"var len=func(string)int{return 99}", `m::size!("abc")`, "caller package declaration"},
		{"type len int", `m::size!("abc")`, "caller package declaration"},
	} {
		source := t.TempDir()
		packagesWrite(t, source, "go.mod", "module builtins.example/app\ngo 1.27\n")
		writeMacroFixtureRoots(t, source, map[string]rootconfig.Mapping{
			"ops":    {Module: "builtins.example/app", Package: "builtins.example/app/ops"},
			"native": {Module: "builtins.example/app", Package: "builtins.example/app/native"},
		})
		packagesWrite(t, source, "ops/macros.gox", "package ops\npub macro_rules! size {($x:expr)=>{len($x)};}\npub macro_rules! space {($x:expr)=>{cap($x)};}\n")
		packagesWrite(t, source, "app.gox", "package app\nuse ops as m;\nfunc F(){_="+tc.body+"}\n")
		packagesWrite(t, source, "shadow.go", "package app\n"+tc.decl+"\n")
		_, err := frontend.CompileModule(source, filepath.Join(t.TempDir(), "generated"))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s %s: %v", tc.decl, tc.body, err)
		}
		if !strings.Contains(err.Error(), "app.gox") || (tc.decl != "" && !strings.Contains(err.Error(), "macros.gox")) {
			t.Fatalf("lost source diagnostic: %v", err)
		}
	}
}
