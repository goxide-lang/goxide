package prototype_test

import (
	"github.com/hxsf/goxide/internal/frontend"
	"strings"
	"testing"
)

func TestDefaultConstruction(t *testing.T) {
	source := `package fixture
#[derive(Default)]
enum Status {
 Other(int),
 #[default]
 Ready,
}
#[derive(Default)]
struct Inner { Count int; Enabled bool; Label string }
#[derive(Default)]
struct Config { First Inner; State Status; Last uint64; Maybe Option[*int] }
#[derive(Default)]
struct Empty {}
macro_rules! fresh {()=>{DefaultConfig()};}
func Fresh() Config {DefaultConfig:=func() Config{panic("captured")};_=DefaultConfig;return fresh!()}
func IsReady(value Status) bool {return match value {Status::Ready=>true,Status::Other(_)=>false}}
func IsNone(value Option[*int]) bool {return match value {Option::None=>true,Option::Some(_)=>false}}
`
	r, e := frontend.Compile("default.gox", []byte(source))
	if e != nil {
		t.Fatal(e)
	}
	if strings.Index(string(r.Go), "out.First = DefaultInner()") > strings.Index(string(r.Go), "out.State = DefaultStatus()") {
		t.Fatal("field initialization reordered")
	}
	runGenerated(t, r.Go, `package fixture
import "testing"
func TestDefaults(t *testing.T){
 v:=Fresh(); if v.First.Count!=0||v.First.Enabled||v.First.Label!=""||v.Last!=0 {t.Fatalf("bad defaults: %#v",v)}
 if !IsReady(v.State){t.Fatal("wrong default variant")};if !IsNone(v.Maybe){t.Fatal("Option default must be None")}
 _=DefaultEmpty()
 var raw Status;if raw!=(Status{}){t.Fatal("Go zero value implicitly rewritten")}
 rejected:=false;func(){defer func(){rejected=recover()!=nil}();_=IsReady(raw)}();if !rejected{t.Fatal("invalid zero enum accepted by match")}
}
`)
}

func TestDefaultRejectsUnsupported(t *testing.T) {
	cases := []string{
		`#[derive(Default)]
enum E { A, B }`,
		`#[derive(Default)]
enum E { #[default] A, #[default] B }`,
		`#[derive(Default)]
enum E { #[default] A(Result[int,string]), B }`,
		`#[derive(Default)]
enum E { #[default] #[default] A }`,
		`enum E { #[default] A }`,
		`#[derive(Default)]
struct S { Value *int }`,
		`#[derive(Default)]
struct S { Value []int }`,
		`#[derive(Default)]
struct S { Value map[string]int }`,
		`#[derive(Default)]
struct S { Value Result[int,string] }`,
		`type Child struct{ X int }
func DefaultChild() Child{return Child{}}
#[derive(Default)]
struct S { Value Child }`,
		`func DefaultS() int{return 1}
#[derive(Default)]
struct S { Value int }`,
	}
	for _, body := range cases {
		_, e := frontend.Compile("invalid-default.gox", []byte("package fixture\n"+body+"\n"))
		if e == nil || !strings.Contains(e.Error(), "invalid-default.gox") {
			t.Errorf("expected source diagnostic for %s; got %v", body, e)
		}
	}
}

func TestDefaultRejectsCrossFileBuiltinShadow(t *testing.T) {
	_, e := frontend.CompilePackage("fixture", map[string][]byte{
		"type.go":   []byte("package fixture\ntype int string\n"),
		"value.gox": []byte("package fixture\n#[derive(Default)]\nstruct Value { N int }\n"),
	}, nil)
	if e == nil || !strings.Contains(e.Error(), "shadowing") {
		t.Fatalf("builtin identity was inferred from spelling: %v", e)
	}
}

func TestDefaultGroupedTypeMacroBinding(t *testing.T) {
	r, e := frontend.Compile("group.gox", []byte(`package fixture
type (
 #[derive(Default)]
 Settings struct { N int }
)
macro_rules! create {()=>{DefaultSettings()};}
func Construct() Settings {DefaultSettings:=func()Settings{panic("captured")};_=DefaultSettings;return create!()}
`))
	if e != nil {
		t.Fatal(e)
	}
	runGenerated(t, r.Go, `package fixture
import "testing"
func TestGrouped(t *testing.T){if Construct().N!=0{t.Fatal("wrong default")}}
`)
}
