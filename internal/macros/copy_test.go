package macros

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestCopyNativeEvaluationAndStorage(t *testing.T) {
	run(t, `package main
import "fmt"
macro_rules! transfer {($dst:expr,$src:expr)=>{copy($dst,$src)};}
macro_rules! duplicate {($dst:expr,$src:expr)=>{copy($dst,$src)+copy($dst,$src)};}
macro_rules! nested {($dst:expr,$src:expr)=>{transfer!($dst,$src)};}
type Numbers []int
func main(){
 dst:=Numbers{0,0,9}
 ptr:=&dst[0];size,space:=len(dst),cap(dst)
 trace:=""
 target:=func()Numbers{trace+="D";return dst}
 source:=func()[]int{trace+="S";return []int{4,5}}
 var count int=transfer!(target(),source())
 if trace!="DS"||count!=2||fmt.Sprint(dst)!="[4 5 9]"||&dst[0]!=ptr||len(dst)!=size||cap(dst)!=space{panic("native copy")}
 if transfer!(dst[:1],[]int{6,7})!=1||dst[0]!=6{panic("short destination")}
 overlap:=[]int{1,2,3,4}
 if transfer!(overlap[1:],overlap[:3])!=3||fmt.Sprint(overlap)!="[1 1 2 3]"{panic("right overlap")}
 if transfer!(overlap,overlap[1:])!=3||fmt.Sprint(overlap)!="[1 2 3 3]"{panic("left overlap")}
 var absent []int
 empty:=[]int{}
 if transfer!(absent,dst)!=0||transfer!(dst,absent)!=0||transfer!(empty,dst)!=0||absent!=nil||empty==nil{panic("nil/empty")}
 text:=[]byte{0,0,33}
 if transfer!(text,"ab")!=2||string(text)!="ab!"{panic("string source")}
 n:=7;pointers:=[]*int{&n};shared:=[]*int{nil}
 if transfer!(shared,pointers)!=1||shared[0]!=pointers[0]{panic("pointer identity")}
 *shared[0]=8
 if n!=8{panic("deep copy")}
 trace=""
 if duplicate!(target(),source())!=4||trace!="DSDS"{panic("template multiplicity")}
 trace=""
 if nested!(target(),source())!=2||trace!="DS"{panic("nested multiplicity")}
 fmt.Println("copy native ok")
}
`, "copy native ok\n")
}

func TestCopyConditionalAndDeferEvaluation(t *testing.T) {
	run(t, `package main
import "fmt"
macro_rules! conditional {($enabled:expr,$dst:expr,$src:expr)=>{if $enabled {copy($dst,$src)}};}
macro_rules! delayed {($dst:expr,$src:expr)=>{defer copy($dst,$src)};}
func deferred()(dst []int,trace string){
 dst=[]int{0};src:=[]int{1}
 target:=func()[]int{trace+="D";return dst}
 source:=func()[]int{trace+="S";return src}
 delayed!(target(),source())
 if trace!="DS"||dst[0]!=0{panic("defer registration")}
 src[0]=7;trace+="B"
 return
}
func main(){
 dst:=[]int{0};trace:=""
 enabled:=func(v bool)bool{trace+="E";return v}
 target:=func()[]int{trace+="D";return dst}
 source:=func()[]int{trace+="S";return []int{9}}
 conditional!(enabled(false),target(),source())
 if trace!="E"||dst[0]!=0{panic("disabled copy")}
 trace=""
 conditional!(enabled(true),target(),source())
 if trace!="EDS"||dst[0]!=9{panic("enabled copy")}
 deferredValue,deferredTrace:=deferred()
 if deferredValue[0]!=7||deferredTrace!="DSB"{panic("deferred copy contents")}
 fmt.Println("copy timing ok")
}
`, "copy timing ok\n")
}

func TestCopyLexicalBindings(t *testing.T) {
	run(t, `package main
import "fmt"
import "reflect"
macro_rules! transfer {($dst:expr,$src:expr)=>{copy($dst,$src)};}
macro_rules! own {()=>{func()int{copy:=func()int{return 17};return copy()}()};}
type Record struct{copy int}
func parameter(copy int)int{dst:=[]int{0};transfer!(dst,[]int{copy});return dst[0]}
func main(){
 dst:=[]int{0}
 copy:=func()[]int{return []int{4}}
 if transfer!(dst,copy())!=1||dst[0]!=4{panic("local binding")}
 {const copy=3;m:=map[int]int{copy:7};transfer!(dst,[]int{m[copy]});if dst[0]!=7{panic("constant key")}}
 {copy:=5;r:=Record{copy:copy};transfer!(dst,[]int{r.copy});if dst[0]!=5{panic("struct key")}}
 switch copy:=any(8).(type){case int:transfer!(dst,[]int{copy});if dst[0]!=8{panic("type switch")}}
 {type copy int;if reflect.TypeOf(copy(1)).Name()!="copy"{panic("unrelated named type")}}
 if parameter(6)!=6||own!()!=17{panic("parameter or template local")}
 fmt.Println("copy bindings ok")
}
`, "copy bindings ok\n")
}

func TestCopyCallerImportDifferentFile(t *testing.T) {
	definitions := []byte("package main\nmacro_rules! transfer {($dst:expr,$src:expr)=>{copy($dst,$src)};}\n")
	source := []byte(`package main
import "fmt"
import copy "strings"
func main(){dst:=[]byte{0,0};fmt.Println(transfer!(dst,copy.Repeat("x",2)),string(dst),copy.Repeat("y",2))}
`)
	pkg := registry(t, "example/main", map[string][]byte{"definitions.gox": definitions, "caller.gox": source}, nil)
	result, err := ExpandPackage("caller.gox", source, nil, pkg, nil)
	if err != nil {
		t.Fatal(err)
	}
	run(t, string(result.Source), "2 xx yy\n")
}

func TestCopyDefinitionBindings(t *testing.T) {
	for _, declaration := range []string{
		`var copy=func([]int,[]int)int{return 9}`,
		`var (copy=func([]int,[]int)int{return 9})`,
		`const copy=3`,
		`type copy int`,
		`struct copy { X int }`,
		`import copy "strings"`,
	} {
		src := "package p\n" + declaration + "\nmacro_rules! transfer {($dst:expr,$src:expr)=>{copy($dst,$src)};}\nfunc f(){_=transfer!([]int{},[]int{})}\n"
		_, err := Expand("shadow.gox", []byte(src))
		if err == nil || !strings.Contains(err.Error(), "definition-site") {
			t.Fatalf("%s: %v", declaration, err)
		}
	}
	run(t, `package main
import "fmt"
func copy(dst,src []int)int{return 17}
macro_rules! transfer {($dst:expr,$src:expr)=>{copy($dst,$src)};}
func main(){copy:=func([]int,[]int)int{return 99};fmt.Println(transfer!([]int{},[]int{}),copy(nil,nil))}
`, "17 99\n")
}

func TestCopyCallerPackageShadow(t *testing.T) {
	definitions := []byte("package ops\npub macro_rules! transfer {($dst:expr,$src:expr)=>{copy($dst,$src)};}\n")
	ops := registry(t, "example/ops", map[string][]byte{"ops.gox": definitions}, nil)
	for _, declaration := range []string{
		`func copy([]int,[]int)int{return 9}`,
		`var copy=func([]int,[]int)int{return 9}`,
		`type copy int`,
	} {
		source := []byte("package caller\nimport ops \"example/ops\"\nfunc f(){_=ops.transfer!([]int{},[]int{})}\n" + declaration + "\n")
		pkg := registry(t, "example/caller", map[string][]byte{"caller.gox": source}, map[string]*Package{"example/ops": ops})
		_, err := ExpandPackage("caller.gox", source, nil, pkg, nil)
		if err == nil || !strings.Contains(err.Error(), "caller package declaration") {
			t.Fatalf("%s: %v", declaration, err)
		}
	}
}

func TestCopyBoundaries(t *testing.T) {
	for _, tc := range []struct{ source, want string }{
		{`macro_rules! value {()=>{copy};} var v=value!()`, "direct call"},
		{`macro_rules! transfer {($dst:expr,$src:expr)=>{copy($dst,$src)};} func f(){type copy int;var n copy;_=transfer!([]int{},[]int{int(n)})}`, "local named type"},
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

func TestCopyDeterminismAndOrigins(t *testing.T) {
	const source = "package p\nmacro_rules! transfer {\n ($dst:expr,$src:expr)=>{copy($dst,$src)};\n}\nfunc f(){_=transfer!([]int{0},[]int{7})}\n"
	for _, newline := range []string{"\n", "\r\n"} {
		src := []byte(strings.ReplaceAll(source, "\n", newline))
		first, err := ExpandWithMap("origin.gox", src)
		if err != nil {
			t.Fatal(err)
		}
		second, err := ExpandWithMap("origin.gox", src)
		if err != nil || !bytes.Equal(first.Source, second.Source) || !reflect.DeepEqual(first.Origins, second.Origins) {
			t.Fatalf("nondeterministic expansion/map: %v", err)
		}
		found := false
		for _, origin := range first.Origins {
			if string(first.Source[origin.OutputStart:origin.OutputEnd]) == "copy" {
				found = true
				if string(src[origin.SourceStart:origin.SourceEnd]) != "copy" || origin.Line != 3 || len(origin.Stack) != 1 || origin.Stack[0].Callsite.Line != 5 {
					t.Fatalf("lost copy definition/callsite: %+v", origin)
				}
			}
		}
		if !found {
			t.Fatal("missing copy origin")
		}
		bad := []byte(strings.ReplaceAll("package p\nmacro_rules! value {()=>{\n 1; copy\n};}\nfunc f(){_=value!()}\n", "\n", newline))
		_, err = ExpandWithMap("bad.gox", bad)
		var diagnostic *Diagnostic
		if !errors.As(err, &diagnostic) || diagnostic.Position.Line != 3 || string(bad[diagnostic.Position.Offset:diagnostic.End]) != "copy" || len(diagnostic.Stack) != 1 {
			t.Fatalf("lost bare copy origin: %v", err)
		}
	}
}
