package macros

import (
	"bytes"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"testing"
)

func TestAllocationFixedTypesAndNativeSemantics(t *testing.T) {
	run(t, `package main
import "fmt"
macro_rules! bytes {($n:expr,$c:expr)=>{make([]byte,$n,$c)};}
macro_rules! table {()=>{make(map[string][]byte)};}
macro_rules! pipe {($n:expr)=>{make(chan int,$n)};}
macro_rules! number {()=>{new(int)};}
macro_rules! pointer {()=>{new(*int)};}
macro_rules! slice {()=>{new([]int)};}
macro_rules! array {($n:expr)=>{new([$n]int)};}
macro_rules! nested {($n:expr)=>{array!($n)};}
macro_rules! composite {()=>{new(map[string][]*int)};}
macro_rules! parenthesized {()=>{new((int))};}
macro_rules! local_count {()=>{func()int{const N=3;return len(new([N]int))}()};}
func main(){
 trace:=""
 sizes,spaces:=0,0
 size:=func()int{sizes++;trace+="N";return 2};space:=func()int{spaces++;trace+="C";return 4}
 native:=make([]byte,size(),space());nativeTrace:=trace
 trace="";sizes=0;spaces=0
 a:=bytes!(size(),space());b:=bytes!(2,4)
 if trace!=nativeTrace||sizes!=1||spaces!=1||len(a)!=len(native)||cap(a)!=cap(native)||a[0]!=0{panic("slice allocation")}
 a[0]=9;if b[0]!=0{panic("shared allocation")}
 m:=table!();m["x"]=a;if m["x"][0]!=9{panic("map allocation")}
 c:=pipe!(1);c<-7;if <-c!=7{panic("channel allocation")}
 p,q:=number!(),number!();*p=3;if p==q||*q!=0{panic("new storage")}
 if *pointer!()!=nil||*slice!()!=nil||*composite!()!=nil||*parenthesized!()!=0{panic("new zero")}
 const N=3
 type Local int
 if len(*array!(len([2]Local{})))!=2{panic("caller length expression types")}
 if len(*array!(N))!=3||len(*nested!(2+2))!=4||local_count!()!=3{panic("array expression slot")}
 fmt.Println("allocation native ok")
}
`, "allocation native ok\n")
}

func TestAllocationPrimitiveIdentityAndOperationShadows(t *testing.T) {
	run(t, `package main
import("fmt";"reflect")
macro_rules! number {()=>{new(int)};}
macro_rules! items {($n:expr)=>{make([]int,$n)};}
macro_rules! own {()=>{func()int{new:=func(int)int{return 17};make:=func(int)int{return 3};return new(1)+make(2)}()};}
macro_rules! call {($f:ident,$v:expr)=>{$f($v)};}
func main(){
 type int string
 make:=func()int{return "kept"};new:=func(int)int{return "called"}
 p:=number!();s:=items!(2)
 if reflect.TypeOf(*p).Name()!="int"||reflect.TypeOf(s).Elem().Name()!="int"||*p!=0{panic("primitive identity")}
 if make()!="kept"||call!(new,"x")!="called"||own!()!=20{panic("ordinary binding")}
 {type new int;if reflect.TypeOf(new("n")).Name()!="new"{panic("unrelated type identity")}}
 fmt.Println("allocation identities ok")
}
`, "allocation identities ok\n")
}

func TestAllocationDefinitionFunctionsRemainOrdinary(t *testing.T) {
	run(t, `package main
import "fmt"
func make(v int)int{return v+10}
func new(v int)int{return v+20}
macro_rules! values {($x:expr)=>{make($x)+new($x)};}
func main(){make:=0;new:=0;fmt.Println(values!(1),make,new)}
`, "32 0 0\n")
}

func TestAllocationRejectsCapturedAndValueTypes(t *testing.T) {
	for _, tc := range []struct{ definition, call, want string }{
		{`macro_rules! allocate {($T:expr)=>{make($T,2)};}`, `allocate!([]int)`, "captured types"},
		{`macro_rules! allocate {($T:ident)=>{new($T)};}`, `allocate!(int)`, "captured types"},
		{`macro_rules! allocate {($T:tt)=>{new($T)};}`, `allocate!(int)`, "captured types"},
		{`macro_rules! allocate {($T:tt)=>{new($T)};}`, `allocate!((int))`, "captured types"},
		{`macro_rules! allocate {($T:expr)=>{new([]$T)};}`, `allocate!(int)`, "captured types"},
		{`macro_rules! allocate {($T:expr)=>{new(map[$T]int)};}`, `allocate!(string)`, "captured types"},
		{`macro_rules! allocate {($T:expr)=>{new(chan $T)};}`, `allocate!(int)`, "captured types"},
		{`macro_rules! allocate {($T:expr)=>{new(*$T)};}`, `allocate!(int)`, "captured types"},
		{`macro_rules! allocate {($T:expr)=>{new([len([2]$T{})]int)};}`, `allocate!(int)`, "captured types"},
		{`macro_rules! allocate {($T:ident)=>{new([len([2]$T{})]int)};}`, `allocate!(int)`, "captured types"},
		{`macro_rules! allocate {($T:expr)=>{new([len((*[2]$T)(nil))]int)};}`, `allocate!(int)`, "captured types"},
		{`macro_rules! allocate {($v:expr,$T:expr)=>{new([len($v.($T))]int)};}`, `allocate!(nil,[2]int)`, "captured types"},
		{`macro_rules! allocate {()=>{new(7)};}`, `allocate!()`, "new(expr)"},
		{`macro_rules! allocate {()=>{new(true)};}`, `allocate!()`, "new(expr)"},
		{`macro_rules! allocate {()=>{new(false)};}`, `allocate!()`, "new(expr)"},
		{`macro_rules! allocate {()=>{new(string(7))};}`, `allocate!()`, "new(expr)"},
		{`macro_rules! allocate {()=>{new(Unknown)};}`, `allocate!()`, "new(expr)"},
		{`macro_rules! allocate {()=>{new(Unknown[int])};}`, `allocate!()`, "new(expr)"},
		{`macro_rules! allocate {()=>{new(func())};}`, `allocate!()`, "new(expr)"},
		{`macro_rules! allocate {()=>{func()*int{int:=7;return new(int)}()};}`, `allocate!()`, "template-local binding"},
		{`macro_rules! allocate {()=>{make};}`, `allocate!()`, "direct call"},
		{`macro_rules! allocate {()=>{new};}`, `allocate!()`, "direct call"},
	} {
		_, err := Expand("allocation.gox", []byte("package p\n"+tc.definition+"\nfunc f(){_="+tc.call+"}\n"))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: got %v want %s", tc.definition, err, tc.want)
		}
	}
}

func TestAllocationDefinitionTypeBindings(t *testing.T) {
	for _, declaration := range []string{
		`type int string`, `type int = string`, `var int=7`, `const int=7`,
		`func int()string{return "helper"}`, `import int "strings"`,
	} {
		_, err := Expand("definition.gox", []byte("package p\n"+declaration+"\nmacro_rules! number {()=>{new(int)};}\nfunc f(){_=number!()}\n"))
		if err == nil || !strings.Contains(err.Error(), "definition-site") {
			t.Fatalf("%s: %v", declaration, err)
		}
	}
	for _, operation := range []string{"make", "new"} {
		for _, declaration := range []string{"var " + operation + "=1", "const " + operation + "=1", "type " + operation + " int", "import " + operation + " \"strings\""} {
			_, err := Expand("definition.gox", []byte("package p\n"+declaration+"\nmacro_rules! allocate {()=>{"+operation+"([]int,1)};}\nfunc f(){_=allocate!()}\n"))
			if err == nil || !strings.Contains(err.Error(), "definition-site") {
				t.Fatalf("%s: %v", declaration, err)
			}
		}
	}
}

func TestAllocationPackageScopesAndImportShadows(t *testing.T) {
	definitions := []byte("package main\nmacro_rules! items {()=>{make([]byte,2)};}\nmacro_rules! number {()=>{new(int)};}\n")
	caller := []byte(`package main
import("fmt";make "strings";new "strconv")
func main(){fmt.Println(len(items!()),*number!(),make.Repeat("x",2),new.Itoa(7))}
`)
	pkg := registry(t, "example/main", map[string][]byte{"definitions.gox": definitions, "caller.gox": caller}, nil)
	expanded, err := ExpandPackage("caller.gox", caller, nil, pkg, nil)
	if err != nil {
		t.Fatal(err)
	}
	run(t, string(expanded.Source), "2 0 xx 7\n")
	dep := registry(t, "example/ops", map[string][]byte{"ops.gox": []byte("package ops\npub macro_rules! number {()=>{new(int)};}\n")}, nil)
	for _, declaration := range []string{"type int string", "var new=1", "type new int", "import int \"strings\""} {
		src := []byte("package caller\nimport ops \"example/ops\"\n" + declaration + "\nfunc f(){_=ops.number!()}\n")
		p := registry(t, "example/caller", map[string][]byte{"caller.gox": src}, map[string]*Package{"example/ops": dep})
		_, err := ExpandPackage("caller.gox", src, nil, p, nil)
		if err == nil || !strings.Contains(err.Error(), "caller") {
			t.Fatalf("%s: %v", declaration, err)
		}
	}
}

func TestAllocationOriginsAndDeterminism(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		source := []byte(strings.ReplaceAll("package p\nmacro_rules! number {()=>{\n new(int)\n};}\nfunc f(){_=number!()}\n", "\n", newline))
		a, err := ExpandWithMap("origin.gox", source)
		if err != nil {
			t.Fatal(err)
		}
		b, err := ExpandWithMap("origin.gox", source)
		if err != nil || !bytes.Equal(a.Source, b.Source) || !reflect.DeepEqual(a.Origins, b.Origins) {
			t.Fatalf("nondeterminism: %v", err)
		}
		found := false
		for _, o := range a.Origins {
			if string(a.Source[o.OutputStart:o.OutputEnd]) == "new" {
				found = true
				if o.Line != 3 || string(source[o.SourceStart:o.SourceEnd]) != "new" || len(o.Stack) != 1 || o.Stack[0].Callsite.Line != 5 {
					t.Fatalf("origin: %+v", o)
				}
			}
		}
		if !found {
			t.Fatal("missing new origin")
		}
		bad := []byte(strings.ReplaceAll("package p\nmacro_rules! number {()=>{\n new(7)\n};}\nfunc f(){_=number!()}\n", "\n", newline))
		_, err = ExpandWithMap("bad.gox", bad)
		var d *Diagnostic
		if !errors.As(err, &d) || d.Position.Line != 3 || string(bad[d.Position.Offset:d.End]) != "7" || len(d.Stack) != 1 {
			t.Fatalf("type operand origin: %v", err)
		}
	}
}

// Each fixed type token should be checked once even when array lengths contain
// further typed array literals. Stop at the bound instead of relying on timing,
// so a regression reports repeated work without spending exponential test time.
func TestAllocationNestedLengthWorkIsLinear(t *testing.T) {
	const depth = 40
	typeText := "[2]int"
	for range depth {
		typeText = "[len(" + typeText + "{})]int"
	}
	typ, err := parser.ParseExpr(typeText)
	if err != nil {
		t.Fatal(err)
	}
	checks := 0
	err = allocationType(typ, nil, nil, func(*ast.Ident) error {
		checks++
		if checks > depth+1 {
			return errors.New("type verification repeated primitive occurrences")
		}
		return nil
	}, func(_ ast.Node, message string) error { return errors.New(message) })
	if err != nil {
		t.Fatal(err)
	}
	if checks != depth+1 {
		t.Fatalf("checked %d primitive occurrences; source contains %d", checks, depth+1)
	}
}

func TestAllocationNestedFixedLiteral(t *testing.T) {
	run(t, `package main
import("fmt";"reflect")
macro_rules! values {()=>{[2]int{}};}
macro_rules! allocate {()=>{new([len(values!())]int)};}
func main(){p:=allocate!();fmt.Println(len(*p),reflect.TypeOf(*p).Elem().Name())}
`, "2 int\n")
	// Other compiler-generated type aliases do not prove a fixed primitive.
	_, err := Expand("sum.gox", []byte("package p\nmacro_rules! allocate {()=>{new(Option[int])};}\nfunc f(){_=allocate!()}\n"))
	if err == nil || !strings.Contains(err.Error(), "fixed predeclared type syntax") {
		t.Fatalf("sum alias passed allocation gate: %v", err)
	}
}

func TestAllocationNestedPrimitiveAliasKeepsDefinitionScope(t *testing.T) {
	inner := registry(t, "example/inner", map[string][]byte{"inner.gox": []byte(`package inner
pub macro_rules! values {()=>{[2]int{}};}
`)}, nil)
	outer := registry(t, "example/outer", map[string][]byte{"outer.gox": []byte(`package outer
import inner "example/inner"
type int string
pub macro_rules! allocate {()=>{new([len(inner.values!())]byte)};}
`)}, map[string]*Package{"example/inner": inner})
	src := []byte("package caller\nimport outer \"example/outer\"\nfunc f(){_=outer.allocate!()}\n")
	caller := registry(t, "example/caller", map[string][]byte{"caller.gox": src}, map[string]*Package{"example/outer": outer})
	expanded, err := ExpandPackage("caller.gox", src, nil, caller, nil)
	if err != nil {
		t.Fatal(err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), "expanded.go", expanded.Source, 0)
	if err != nil {
		t.Fatal(err)
	}
	aliases := map[string]string{}
	ast.Inspect(file, func(n ast.Node) bool {
		if spec, ok := n.(*ast.TypeSpec); ok && spec.Assign.IsValid() {
			if id, ok := spec.Type.(*ast.Ident); ok {
				aliases[spec.Name.Name] = id.Name
			}
		}
		return true
	})
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		name, ok := call.Fun.(*ast.Ident)
		if !ok || name.Name != "new" {
			return true
		}
		array := call.Args[0].(*ast.ArrayType)
		length := ast.Unparen(array.Len).(*ast.CallExpr)
		literal := ast.Unparen(length.Args[0]).(*ast.CompositeLit)
		element := literal.Type.(*ast.ArrayType).Elt.(*ast.Ident)
		if aliases[element.Name] != "int" {
			t.Fatalf("inner primitive rebound through outer int declaration: %s", expanded.Source)
		}
		found = true
		return false
	})
	if !found {
		t.Fatal("missing native new call")
	}
}
