package prototype_test

import (
	"github.com/goxide-lang/goxide/tests/testutil"
	"strings"
	"testing"
)

func TestMacrosAndDerivesIntegrated(t *testing.T) {
	source := `package fixture; use std; use std::PartialEq; use serde;
use fmt;
macro_rules! publish { ($message:expr, $($field:expr),*) => { fmt::Sprint($message, $($field),*) }; }
macro_rules! once { ($value:expr) => {func() int { temporary := $value; return temporary + temporary }()}; }
macro_rules! exit { ($value:expr) => { return $value }; }
macro_rules! cleanup { ($action:ident) => { defer $action() }; }
#[derive(std::derive::Debug,std::derive::Clone,serde::derive::Serialize,serde::derive::Deserialize)]
type Item struct { Name string }
#[derive(std::derive::Debug,std::derive::Clone,serde::derive::Serialize,serde::derive::Deserialize)]
type Batch struct { Items []Item; Lookup map[string]Item }
#[derive(std::derive::Error)]
#[error("storage unavailable")]
type StorageError struct {
 #[from]
 Cause error
}
#[derive(std::derive::Error)]
#[error("order {ID}: {Reason}")]
type InvalidOrder struct { ID uint64; Reason string }
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

impl Item {
 pub func copy(value &Self) Self { return std::Clone::clone(value) }
 pub func describe(value &Self) string { return std::Debug::debug(value) }
 pub func encode(s serde::Serializer,value &Self) error { return serde::Write(s,value) }
 pub func decode(d serde::Deserializer) (Self,error) { return serde::Read[Self](d) }
}

impl Batch {
 pub func copy(value &Self) Self { return std::Clone::clone(value) }
 pub func describe(value &Self) string { return std::Debug::debug(value) }
 pub func encode(s serde::Serializer,value &Self) error { return serde::Write(s,value) }
 pub func decode(d serde::Deserializer) (Self,error) { return serde::Read[Self](d) }
}

impl StorageError {
 pub func wrap(cause error) Self { return StorageError::from::<std::From[error]>(cause) }
}

`
	result, err := testutil.FormalSource(t, "application.gox", []byte(source), "[dependencies.fmt]\ngo=\"fmt\"\n")
	if err != nil {
		t.Fatal(err)
	}
	runFormalSerde(t, result, `package fixture
import("errors";"strings";"testing";"github.com/goxide-lang/serde";"github.com/goxide-lang/serde/json")
type typedNilError struct{}
func(*typedNilError)Error()string{return "nil"}
func TestApplication(t *testing.T){
 n,event:=Exercise();if n!=181||!strings.Contains(event,"request"){t.Fatalf("macro effects: %d %q",n,event)}
 var done bool;if Boundaries(&done)!=4||!done{t.Fatal("macro changed return/defer boundary")}
 original:=Batch{Items:[]Item{{Name:"before"}},Lookup:map[string]Item{"one":{Name:"original"}}}
 cloned:=(BatchOps{}).Copy(&original);cloned.Items[0].Name="after";cloned.Lookup["one"]=Item{Name:"changed"}
 if original.Items[0].Name!="before"||original.Lookup["one"].Name!="original"{t.Fatal("clone aliases owned containers")}
 if !strings.Contains((BatchOps{}).Describe(&original),"before"){t.Fatal("debug lost nested data")}
 wire,err:=serdejson.Encode(func(s serde.Serializer)error{return (BatchOps{}).Encode(s,&cloned)});if err!=nil{t.Fatal(err)}
 var decoded Batch;err=serdejson.Decode(wire,func(d serde.Deserializer)error{var e error;decoded,e=(BatchOps{}).Decode(d);return e});if err!=nil{t.Fatal(err)}
 if decoded.Items[0].Name!="after"{t.Fatal("combined derive roundtrip failed")}
 cause:=errors.New("disk");wrapped:=(StorageErrorOps{}).Wrap(cause);if !errors.Is(wrapped,cause)||wrapped.Error()!="storage unavailable"{t.Fatal("error chain broken")}
 var inner *typedNilError;var asError error=inner;wrapped=(StorageErrorOps{}).Wrap(asError);if wrapped.Unwrap()==nil{t.Fatal("typed nil was collapsed")}
 if (InvalidOrder{ID:9007199254740993,Reason:"bad"}).Error()!="order 9007199254740993: bad"{t.Fatal("error formatting imprecise")}
}
`)
}

func TestIntegratedMacroAndErrorDiagnostics(t *testing.T) {
	cases := []string{
		`package fixture; use std; use std::PartialEq;
#[derive(std::derive::Error)]
type Bad struct { Error string }

impl Bad {
 pub func wrap(cause error) Self { return Bad::from::<std::From[error]>(cause) }
}
`,
		`package fixture; use std; use std::PartialEq;
macro_rules! bad {($x:expr $y:expr)=>{$x};}
func f(){bad!(1 2)}
`,
		`package fixture; use std; use std::PartialEq;
#[derive(std::derive::Error)]
#[error("unknown {Missing}")]
type Bad struct { Code int }

impl Bad {
 pub func wrap(cause error) Self { return Bad::from::<std::From[error]>(cause) }
}
`,
		`package fixture; use std; use std::PartialEq;
#[derive(std::derive::Error)]
type Bad struct {
 #[from]
 Cause error
 Code int
}

impl Bad {
 pub func wrap(cause error) Self { return Bad::from::<std::From[error]>(cause) }
}
`,
	}
	for _, source := range cases {
		_, err := testutil.FormalSource(t, "diagnostic.gox", []byte(source), "[dependencies.fmt]\ngo=\"fmt\"\n")
		if err == nil || !strings.Contains(err.Error(), "diagnostic.gox") {
			t.Fatalf("missing source-aware rejection: %v", err)
		}
	}
}

func TestMacroTemplateLocalShadowsImportedPackage(t *testing.T) {
	source := `package fixture; use std; use std::PartialEq;
use fmt;
macro_rules! local {($value:expr)=>{func() string {fmt := $value; return fmt.String()}()};}
func exerciseLocal(value *Builder) string { _ = fmt::Sprint(1); return local!(value) }
type Builder struct{}
func(*Builder)String()string{return "local-method"}
`
	result, err := testutil.FormalSource(t, "shadow.gox", []byte(source), "[dependencies.fmt]\ngo=\"fmt\"\n")
	if err != nil {
		t.Fatal(err)
	}
	runFormalSerde(t, result, `package fixture
import "testing"
func TestLocal(t *testing.T){if exerciseLocal(&Builder{})!="local-method"{t.Fatal("template local import shadowing broken")}}
`)
}
