package macros

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestAppendNativeEvaluationAndStorage(t *testing.T) {
	run(t, `package main
import "fmt"
macro_rules! push {($s:expr,$v:expr)=>{append($s,$v)};}
macro_rules! extend {($s:expr,$v:expr)=>{append($s,$v...)};}
macro_rules! values {($s:expr,$($v:expr),*)=>{append($s,$($v),*)};}
macro_rules! nested {($s:expr,$v:expr)=>{push!(push!($s,$v),$v)};}
type Numbers []int
func main(){
 trace:=""
 base:=func()Numbers{trace+="S";return Numbers{1}}
 next:=func()int{trace+="V";return 2}
 result:=push!(base(),next())
 var named Numbers=result
 if trace!="SV"||len(named)!=2||named[1]!=2{panic("evaluation or named slice")}
 storage:=[4]int{1,2,0,0}
 grown:=push!(storage[:2],3)
 if &grown[0]!=&storage[0]||storage[2]!=3{panic("capacity reuse")}
 moved:=push!(storage[:2:2],9)
 if &moved[0]==&storage[0]||storage[2]!=3{panic("capacity growth")}
 overlap:=[]int{1,2,3,4}
 overlap=extend!(overlap[:1],overlap[1:3])
 if fmt.Sprint(overlap)!="[1 2 3]"{panic("overlap")}
 var absent []int
 empty:=[]int{}
 if values!(absent,)!=nil||values!(empty,)==nil{panic("nil/empty")}
 text:=extend!([]byte{65},"BC")
 if string(text)!="ABC"{panic("string spread")}
 trace=""
 repeated:=values!(Numbers{},next(),next())
 if trace!="VV"||fmt.Sprint(repeated)!="[2 2]"{panic("repeated capture")}
 trace=""
 doubled:=nested!(Numbers{},next())
 if trace!="VV"||fmt.Sprint(doubled)!="[2 2]"{panic("nested evaluation")}
 fmt.Println("append native ok")
}
`, "append native ok\n")
}

func TestAppendLexicalBindings(t *testing.T) {
	run(t, `package main
import "fmt"
import "reflect"
macro_rules! push {($s:expr,$v:expr)=>{append($s,$v)};}
macro_rules! own {()=>{func()int{append:=func(int)int{return 17};return append(1)}()};}
type Record struct{append int}
func parameter(append int)int{return push!([]int{},append)[0]}
func main(){
 append:=func()int{return 4}
 if push!([]int{},append())[0]!=4{panic("local binding")}
 {const append=3;v:=map[int]int{append:7};if push!([]int{},v[append])[0]!=7{panic("const map key")}}
 {append:=5;v:=Record{append:append};if push!([]int{},v.append)[0]!=5{panic("struct key")}}
 switch append:=any(8).(type){case int:if push!([]int{},append)[0]!=8{panic("type switch")}}
 {type append int;if reflect.TypeOf(append(1)).Name()!="append"{panic("unrelated named type")}}
 if parameter(6)!=6||own!()!=17{panic("parameter or template local")}
 fmt.Println("append bindings ok")
}
`, "append bindings ok\n")
}

func TestAppendCallerImportDifferentFile(t *testing.T) {
	definitions := []byte(`package main
macro_rules! push {($s:expr,$v:expr)=>{append($s,$v)};}
`)
	source := []byte(`package main
import "fmt"
import append "strings"
func main(){fmt.Println(string(push!([]byte{},append.Repeat("x",2)[0])),append.Repeat("y",2))}
`)
	pkg := registry(t, "example/main", map[string][]byte{"definitions.gox": definitions, "caller.gox": source}, nil)
	result, err := ExpandPackage("caller.gox", source, nil, pkg, nil)
	if err != nil {
		t.Fatal(err)
	}
	run(t, string(result.Source), "x yy\n")
}

func TestAppendCallerPackageShadow(t *testing.T) {
	definitions := []byte("package ops\npub macro_rules! push {($s:expr,$v:expr)=>{append($s,$v)};}\n")
	ops := registry(t, "example/ops", map[string][]byte{"ops.gox": definitions}, nil)
	for _, declaration := range []string{
		`func append([]int,int)[]int{return []int{9}}`,
		`var append=func([]int,int)[]int{return []int{9}}`,
		`type append int`,
	} {
		source := []byte("package caller\nimport ops \"example/ops\"\nfunc f(){_=ops.push!([]int{},1)}\n" + declaration + "\n")
		pkg := registry(t, "example/caller", map[string][]byte{"caller.gox": source}, map[string]*Package{"example/ops": ops})
		_, err := ExpandPackage("caller.gox", source, nil, pkg, nil)
		if err == nil || !strings.Contains(err.Error(), "caller package declaration") {
			t.Fatalf("%s: %v", declaration, err)
		}
	}
}

func TestAppendDefinitionBindings(t *testing.T) {
	for _, declaration := range []string{
		`var append=func([]int,int)[]int{return []int{9}}`,
		`var (append=func([]int,int)[]int{return []int{9}})`,
		`const append=3`,
		`type append int`,
		`struct append { X int }`,
		`import append "strings"`,
	} {
		src := "package p\n" + declaration + "\nmacro_rules! push {($s:expr,$v:expr)=>{append($s,$v)};}\nfunc f(){_=push!([]int{},1)}\n"
		_, err := Expand("shadow.gox", []byte(src))
		if err == nil || !strings.Contains(err.Error(), "definition-site") {
			t.Fatalf("%s: %v", declaration, err)
		}
	}
	run(t, `package main
import "fmt"
func append(s []int,v int)[]int{return []int{len(s)+v+10}}
macro_rules! push {($s:expr,$v:expr)=>{append($s,$v)};}
func main(){append:=func([]int,int)[]int{return []int{99}};fmt.Println(push!([]int{1},2),append([]int{},0))}
`, "[13] [99]\n")
}

func TestAppendConditionalEvaluation(t *testing.T) {
	run(t, `package main
import "fmt"
macro_rules! event_if {($enabled:expr,$message:expr,$s:expr,$v:expr)=>{if $enabled {fmt.Println($message,append($s,$v))}};}
func main(){
 trace:=""
 enabled:=func(value bool)bool{trace+="E";return value}
 message:=func()string{trace+="M";return "event"}
 base:=func()[]int{trace+="S";return []int{1}}
 value:=func()int{trace+="V";return 2}
 event_if!(enabled(false),message(),base(),value())
 if trace!="E"{panic("disabled evaluation")}
 trace=""
 event_if!(enabled(true),message(),base(),value())
 if trace!="EMSV"{panic("enabled evaluation")}
}
`, "event [1 2]\n")
}

func TestAppendBoundaries(t *testing.T) {
	for _, tc := range []struct{ source, want string }{
		{`macro_rules! value {()=>{append};} var v=value!()`, "direct call"},
		{`macro_rules! push {($s:expr,$v:expr)=>{append($s,$v)};} func f(){type append int;var n append;_=push!([]int{},int(n))}`, "local named type"},
		{`macro_rules! value {()=>{clear()};} func f(){value!()}`, "unsupported free template name"},
		{`macro_rules! value {()=>{make([]int,1)};} func f(){_=value!()}`, "unsupported free template name"},
		{`macro_rules! value {()=>{new(int)};} func f(){_=value!()}`, "unsupported free template name"},
	} {
		_, err := Expand("boundary.gox", []byte("package p\n"+tc.source))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: got %v want %s", tc.source, err, tc.want)
		}
	}
}

func TestAppendDeterminismAndOrigins(t *testing.T) {
	const source = "package p\nmacro_rules! push {\n ($s:expr,$v:expr)=>{append($s,$v)};\n}\nfunc f(){_=push!([]int{},7)}\n"
	for _, newline := range []string{"\n", "\r\n"} {
		src := []byte(strings.ReplaceAll(source, "\n", newline))
		first, err := ExpandWithMap("origin.gox", src)
		if err != nil {
			t.Fatal(err)
		}
		second, err := ExpandWithMap("origin.gox", src)
		if err != nil || !bytes.Equal(first.Source, second.Source) {
			t.Fatalf("nondeterministic expansion: %v", err)
		}
		found := false
		for _, origin := range first.Origins {
			if string(first.Source[origin.OutputStart:origin.OutputEnd]) == "append" {
				found = true
				if string(src[origin.SourceStart:origin.SourceEnd]) != "append" || origin.Line != 3 || len(origin.Stack) != 1 || origin.Stack[0].Callsite.Line != 5 {
					t.Fatalf("lost append definition/callsite: %+v", origin)
				}
			}
		}
		if !found {
			t.Fatal("missing append origin")
		}
		bad := []byte(strings.ReplaceAll("package p\nmacro_rules! value {()=>{\n 1; append\n};}\nfunc f(){_=value!()}\n", "\n", newline))
		_, err = ExpandWithMap("bad.gox", bad)
		var diagnostic *Diagnostic
		if !errors.As(err, &diagnostic) || diagnostic.Position.Line != 3 || string(bad[diagnostic.Position.Offset:diagnostic.End]) != "append" || len(diagnostic.Stack) != 1 {
			t.Fatalf("lost bare append origin: %v", err)
		}
	}
}
