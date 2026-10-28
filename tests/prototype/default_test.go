package prototype_test

import (
	"github.com/goxide-lang/goxide/tests/testutil"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConstruction(t *testing.T) {
	source := `package fixture; use std; use std::PartialEq;
#[derive(std::derive::Default)]
pub enum Status {
 Other(int),
 #[default]
 Ready,
}
#[derive(std::derive::Default)]
type Inner struct { Count int; Enabled bool; Label string }
#[derive(std::derive::Default)]
type Config struct { First Inner; State Status; Last uint64; Maybe Option[*int] }
#[derive(std::derive::Default)]
type Empty struct {}
macro_rules! fresh {()=>{Config::initial()};}
func Fresh() Config {DefaultConfig:=func() Config{panic("captured")};_=DefaultConfig;return fresh!()}
func IsReady(value Status) bool {return match value {Status::Ready=>true,Status::Other(_)=>false}}
func IsNone(value Option[*int]) bool {return match value {Option::None=>true,Option::Some(_)=>false}}

impl Status {
 pub func initial() Self { return std::default_value[Self]() }
}

impl Inner {
 pub func initial() Self { return std::default_value[Self]() }
}

impl Config {
 pub func initial() Self { return std::default_value[Self]() }
}

impl Empty {
 pub func initial() Self { return std::default_value[Self]() }
}
`
	r, e := testutil.FormalSource(t, "default.gox", []byte(source))
	if e != nil {
		t.Fatal(e)
	}
	first := strings.Index(string(r.Go), "First:")
	state := strings.Index(string(r.Go), "State:")
	if first < 0 || state < 0 || first > state {
		t.Fatal("field initialization reordered")
	}
	runFormalSerde(t, r, `package fixture
import "testing"
func TestDefaults(t *testing.T){
 v:=Fresh(); if v.First.Count!=0||v.First.Enabled||v.First.Label!=""||v.Last!=0 {t.Fatalf("bad defaults: %#v",v)}
 if !IsReady(v.State){t.Fatal("wrong default variant")};if !IsNone(v.Maybe){t.Fatal("Option default must be None")}
 _=(EmptyOps{}).Initial()
 var raw Status;if raw!=nil{t.Fatal("Go zero value implicitly rewritten")}
 rejected:=false;func(){defer func(){rejected=recover()!=nil}();_=IsReady(raw)}();if !rejected{t.Fatal("invalid zero enum accepted by match")}
}
`)
}

func TestDefaultRejectsUnsupported(t *testing.T) {
	cases := []string{
		`#[derive(std::derive::Default)]
enum E { #[default] A(Result[int,string]), B }`,
		`#[derive(std::derive::Default)]
enum E { #[default] #[default] A }`,
		`enum E { #[default] A }`,
		`#[derive(std::derive::Default)]
struct S { Value *int }`,
		`#[derive(std::derive::Default)]
struct S { Value []int }`,
		`#[derive(std::derive::Default)]
struct S { Value map[string]int }`,
		`#[derive(std::derive::Default)]
struct S { Value Result[int,string] }`,
		`type Child struct{ X int }
func DefaultChild() Child{return Child{}}
#[derive(std::derive::Default)]
struct S { Value Child }`,
	}
	for _, body := range cases {
		_, e := testutil.FormalSource(t, "invalid-default.gox", []byte("package fixture; use std; use std::PartialEq;\n"+body+"\n"))
		if e == nil || !strings.Contains(e.Error(), "invalid-default.gox") {
			t.Errorf("expected source diagnostic for %s; got %v", body, e)
		}
	}
}

func TestDefaultRejectsCrossFileBuiltinShadow(t *testing.T) {
	root := t.TempDir()
	if err := testutil.WriteFile(filepath.Join(root, "goxide.toml"), []byte(testutil.FormalManifest(t, "fixture", "fixture")), 0600); err != nil {
		t.Fatal(err)
	}
	for name, source := range map[string]string{"type.go": "package fixture\ntype int string\n", "value.gox": "package fixture; use std;\n#[derive(std::derive::Default)]\nstruct Value { N int }\n"} {
		if err := testutil.WriteFile(filepath.Join(root, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	_, _, err := testutil.PrepareFormal(t, root)
	if err == nil || !strings.Contains(err.Error(), "missing trait implementation") || !strings.Contains(err.Error(), "fixture::int") {
		t.Fatalf("builtin identity was inferred from spelling: %v", err)
	}
}

func TestDefaultOrdinaryHelperCoexists(t *testing.T) {
	result, err := testutil.FormalSource(t, "ordinary.gox", []byte(`package fixture; use std;
 #[derive(std::derive::Default)] type S struct {Value int}
 func DefaultS() int{return 17}
 func Construct() S{return std::default_value[S]()}
 `))
	if err != nil {
		t.Fatal(err)
	}
	runFormalSerde(t, result, `package fixture
 import "testing"
 func TestCoexists(t *testing.T){if DefaultS()!=17||Construct().Value!=0{t.Fatal("ordinary helper interfered with Default")}}
 `)
}

func TestDefaultGroupedTypeMacroBinding(t *testing.T) {
	r, e := testutil.FormalSource(t, "group.gox", []byte(`package fixture; use std; use std::PartialEq;
type (
 #[derive(std::derive::Default)]
 Settings struct { N int }
)
macro_rules! create {()=>{Settings::initial()};}
func Construct() Settings {DefaultSettings:=func()Settings{panic("captured")};_=DefaultSettings;return create!()}

impl Settings {
 pub func initial() Self { return std::default_value[Self]() }
}
`))
	if e != nil {
		t.Fatal(e)
	}
	runFormalSerde(t, r, `package fixture
import "testing"
func TestGrouped(t *testing.T){if Construct().N!=0{t.Fatal("wrong default")}}
`)
}
