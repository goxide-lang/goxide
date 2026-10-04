package prototype_test

import (
	"github.com/hxsf/goxide/internal/frontend"
	"strings"
	"testing"
)

func TestMacrosAndDerivesIntegrated(t *testing.T) {
	source := `package fixture
import "fmt"
macro_rules! publish { ($message:expr, $($field:expr),*) => { fmt.Sprint($message, $($field),*) }; }
macro_rules! once { ($value:expr) => {func() int { temporary := $value; return temporary + temporary }()}; }
macro_rules! exit { ($value:expr) => { return $value }; }
macro_rules! cleanup { ($action:ident) => { defer $action() }; }
#[derive(Debug, Clone, Serialize, Deserialize)]
struct Item { Name string }
#[derive(Debug, Clone, Serialize, Deserialize)]
struct Batch { Items []Item; Lookup map[string]Item }
#[derive(Error)]
#[error("storage unavailable")]
struct StorageError {
 #[from]
 Cause error
}
#[derive(Error)]
#[error("order {ID}: {Reason}")]
struct InvalidOrder { ID uint64; Reason string }
func Exercise() (int,string) {
 temporary := 90
 count := 0
 next := func()int{count++;return temporary}
 value := once!(next())
 fmt := "shadow"
 _ = fmt
 return value+count, publish!("request", "id=", 7)
}
func Boundaries(done *bool) int {mark:=func(){*done=true};cleanup!(mark);exit!(4)}
`
	result, err := frontend.Compile("application.gox", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	runGenerated(t, result.Go, `package fixture
import("errors";"strings";"testing";"github.com/goxide-lang/serde";"github.com/goxide-lang/serde/json")
type typedNilError struct{}
func(*typedNilError)Error()string{return "nil"}
func TestApplication(t *testing.T){
 n,event:=Exercise();if n!=181||!strings.Contains(event,"request"){t.Fatalf("macro effects: %d %q",n,event)}
 var done bool;if Boundaries(&done)!=4||!done{t.Fatal("macro changed return/defer boundary")}
 original:=Batch{Items:[]Item{{Name:"before"}},Lookup:map[string]Item{"one":{Name:"original"}}}
 cloned:=CloneBatch(&original);cloned.Items[0].Name="after";cloned.Lookup["one"]=Item{Name:"changed"}
 if original.Items[0].Name!="before"||original.Lookup["one"].Name!="original"{t.Fatal("clone aliases owned containers")}
 if !strings.Contains(DebugBatch(&original),"before"){t.Fatal("debug lost nested data")}
 wire,err:=serdejson.Encode(func(s serde.Serializer)error{return SerializeBatch(s,&cloned)});if err!=nil{t.Fatal(err)}
 var decoded Batch;err=serdejson.Decode(wire,func(d serde.Deserializer)error{var e error;decoded,e=DeserializeBatch(d);return e});if err!=nil{t.Fatal(err)}
 if decoded.Items[0].Name!="after"{t.Fatal("combined derive roundtrip failed")}
 cause:=errors.New("disk");wrapped:=FromStorageError(cause);if !errors.Is(wrapped,cause)||wrapped.Error()!="storage unavailable"{t.Fatal("error chain broken")}
 var inner *typedNilError;var asError error=inner;wrapped=FromStorageError(asError);if wrapped.Unwrap()==nil{t.Fatal("typed nil was collapsed")}
 if (InvalidOrder{ID:9007199254740993,Reason:"bad"}).Error()!="order 9007199254740993: bad"{t.Fatal("error formatting imprecise")}
}
`)
}

func TestIntegratedMacroAndErrorDiagnostics(t *testing.T) {
	cases := []string{
		`package fixture
#[derive(Error)]
struct Bad { Error string }
`,
		`package fixture
macro_rules! bad {($x:expr $y:expr)=>{$x};}
func f(){bad!(1 2)}
`,
		`package fixture
#[derive(Error)]
#[error("unknown {Missing}")]
struct Bad { Code int }
`,
		`package fixture
#[derive(Error)]
struct Bad {
 #[from]
 Cause error
 Code int
}
`,
	}
	for _, source := range cases {
		_, err := frontend.Compile("diagnostic.gox", []byte(source))
		if err == nil || !strings.Contains(err.Error(), "diagnostic.gox") {
			t.Fatalf("missing source-aware rejection: %v", err)
		}
	}
}

func TestMacroTemplateLocalShadowsImportedPackage(t *testing.T) {
	source := `package fixture
import "fmt"
macro_rules! local {($value:expr)=>{func() string {fmt := $value; return fmt.String()}()};}
func exerciseLocal(value *Builder) string { _ = fmt.Sprint(1); return local!(value) }
type Builder struct{}
func(*Builder)String()string{return "local-method"}
`
	result, err := frontend.Compile("shadow.gox", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	runGenerated(t, result.Go, `package fixture
import "testing"
func TestLocal(t *testing.T){if exerciseLocal(&Builder{})!="local-method"{t.Fatal("template local import shadowing broken")}}
`)
}
