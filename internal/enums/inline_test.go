package enums

import (
	"github.com/hxsf/goxide/internal/testdeps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInlinePayloadSlotsAndPublicProjection(t *testing.T) {
	source := []byte(`package slots
pub enum Item { A(int), B(int), Empty, }
`)
	collected, err := CollectPackage("slots.gox", source)
	if err != nil {
		t.Fatal(err)
	}
	lowered, err := LowerPackage("slots.gox", source, nil, collected.Enums, collected.Types)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(lowered.Source), "GoxideCase") || strings.Contains(string(lowered.Source), "interface {") {
		t.Fatal("old case ABI emitted")
	}
	dir := t.TempDir()
	if err := testdeps.WriteFile(filepath.Join(dir, "go.mod"), []byte("module slots\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "slots.go"), lowered.Source, 0600); err != nil {
		t.Fatal(err)
	}
	test := `package slots_test
import("testing";s "slots";"runtime")
func panicCall(t *testing.T,f func()){t.Helper();defer func(){if recover()==nil {t.Fatal("missing projection panic")}}();f()}
func TestSlots(t *testing.T){
 a:=s.NewItemA(3); b:=a; p:=s.GoxideProject4_Item_A(&a); *p=4
 if s.GoxideValue4_Item_A(a)!=4||s.GoxideValue4_Item_A(b)!=3 {t.Fatal("copy changed into shared payload")}
 a=s.NewItemA(8); if *p!=8 {t.Fatal("same variant did not reuse original slot")}
 a=s.NewItemB(12);if *p!=0 {t.Fatal("inactive A slot was not zeroed by whole assignment")};*p=40
 if s.GoxideValue4_Item_B(a)!=12 {t.Fatal("same-typed variant slots overlap")};s.ValidateItem(a)
 a=s.NewItemA(21);if *p!=21 {t.Fatal("A/B/A slot identity changed")}
 retained:=func()*int{v:=s.NewItemA(50);return s.GoxideProject4_Item_A(&v)}();runtime.GC();*retained=51;if *retained!=51 {t.Fatal("escaped slot lost")}
 panicCall(t,func(){s.GoxideProject4_Item_A(nil)});panicCall(t,func(){s.GoxideProject4_Item_B(&a)})
 panicCall(t,func(){s.GoxideValue4_Item_B(a)});panicCall(t,func(){s.ValidateItem(s.Item{})})
 var alias s.GoxidePayload4_Item_A=2;*p=alias
}
`
	if err := os.WriteFile(filepath.Join(dir, "slots_test.go"), []byte(test), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(goTool(t), "test", "-race", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s\n%s", err, out, lowered.Source)
	}
}

func TestBorrowBindingsCaptureBeforeGuards(t *testing.T) {
	source := []byte(`package p
enum E { A(int), B(int), }
func f(e E){ match e {
 E::A(ref p) if mutate(&e) => { _=*p },
 E::A(ref mut p) => { *p=9 },
 E::B(_) => {},
}}
func mutate(e *E)bool{*e=E::B(8);return false}
`)
	r, err := Lower("borrow.gox", source)
	if err != nil {
		t.Fatal(err)
	}
	text := string(r.Source)
	if strings.Count(text, "__goxide_enum_project_write(") != 1 {
		t.Fatalf("expected one shared projection before guards\n%s", text)
	}
	projection := strings.Index(text, "__goxide_enum_project_write(")
	guard := strings.Index(text, "if mutate(")
	if projection < 0 || guard < projection {
		t.Fatal("projection occurs after guard")
	}
	if strings.Count(text, "&mut ( e )") != 1 {
		t.Fatalf("place was not captured once\n%s", text)
	}
	if !strings.Contains(text, "__goxide_enum_value_1_E_A(__goxide_enum_saved_") && !strings.Contains(text, "p := &(*__goxide_enum_ref_") {
		t.Fatal("missing checked read narrowing")
	}
}

func TestBasicAutomaticBindingModes(t *testing.T) {
	for _, mode := range []string{"&", "&mut "} {
		src := []byte("package p; enum E { A(int), B, };func f(e E){match " + mode + "e {E::A(p)=>{_=*p},E::B=>{}}}")
		r, err := Lower("auto.gox", src)
		if err != nil {
			t.Fatal(err)
		}
		want := "__goxide_enum_project_read("
		if mode == "&mut " {
			want = "__goxide_enum_project_write("
		}
		if !strings.Contains(string(r.Source), want) {
			t.Fatalf("missing mode %s\n%s", mode, r.Source)
		}
	}
	_, err := Lower("upgrade.gox", []byte("package p;enum E{A(int)};func f(e E){match &e {E::A(ref mut p)=>{*p=1}}}"))
	if err == nil || !strings.Contains(err.Error(), "writable match scrutinee") {
		t.Fatal("read mode upgraded", err)
	}
}

func TestVariantNamedRefIsAValuePattern(t *testing.T) {
	r, err := Lower("ref-name.gox", []byte(`package p;enum E{ref(int)};func f(){match E::ref(1){E::ref(n)=>{_=n}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(r.Source), "__goxide_enum_owner_") {
		t.Fatal("variant spelling changed match into a borrow")
	}
}

func TestWildcardLabelsAreNotDuplicated(t *testing.T) {
	source := []byte(`package main
enum E{A,B}
func value(e E)int{match e{E::A if false=>{},_=>{Loop: for {break Loop};return 7}};return 0}
func main(){if value(E::A)!=7||value(E::B)!=7 {panic("labels")}}
`)
	r, err := Lower("labels.gox", source)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(r.Source), "Loop:") != 1 {
		t.Fatal("wildcard label duplicated")
	}
	dir := t.TempDir()
	if err := testdeps.WriteFile(filepath.Join(dir, "go.mod"), []byte("module labels\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), r.Source, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(goTool(t), "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	borrowed := []byte(`package p;enum E{A(int),B};func f(e E){match e{E::A(ref p)if *p>0=>{},_=>{Loop:for{break Loop}}}}`)
	r, err = Lower("borrowed-labels.gox", borrowed)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(r.Source), "Loop:") != 1 || len(r.ReferenceTemps) != 1 {
		t.Fatal("borrowed wildcard duplicated or lost projection evidence")
	}
}

func TestUnitAutomaticPatternDoesNotProject(t *testing.T) {
	for _, mode := range []string{"&", "&mut "} {
		r, err := Lower("unit.gox", []byte("package p;func f(v Option[int]){match "+mode+"v{Option[int]::Some(p)=>{_=*p},Option[int]::None=>{}}}"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(r.Source), "__goxide_enum_project_Option_None") {
			t.Fatal("unit variant acquired a payload projector")
		}
		if len(r.ReferenceTemps) != 1 {
			t.Fatalf("payload slots: %v", r.ReferenceTemps)
		}
	}
}

func TestGeneratedTagDoesNotCaptureUnrelatedBuiltinBindings(t *testing.T) {
	source := []byte(`package main
type int=uint64
var nil=5
enum E{V(string)}
func wide()int{return 1<<63}
func main(){v:=E::V("ok");match v{E::V(s)=>{if s!="ok" {panic(s)}}};if wide()!=1<<63||nil!=5{panic("changed ordinary bindings")}}
`)
	r, err := Lower("hygiene.gox", source)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := testdeps.WriteFile(filepath.Join(dir, "go.mod"), []byte("module hygienic\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), r.Source, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(goTool(t), "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s\n%s", err, out, r.Source)
	}
}

func TestReadonlyPayloadHandleIsCopiedWithoutDereference(t *testing.T) {
	r, err := Lower("read-handle.gox", []byte(`package p;enum E{Value(int)};func read(e E){match &e{E::Value(p)=>{_=p}}}`))
	if err != nil {
		t.Fatal(err)
	}
	source := string(r.Source)
	if !strings.Contains(source, "p := __goxide_enum_ref_") || strings.Contains(source, "p := &(*__goxide_enum_ref_") {
		t.Fatalf("readonly reference copy introduced a payload read\n%s", source)
	}
}
