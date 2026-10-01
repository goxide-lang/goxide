package prototype_test

import (
	"github.com/hxsf/goxide/internal/frontend"
	"strings"
	"testing"
)

func TestRawEnumClonePreservesAllValues(t *testing.T) {
	r, e := frontend.Compile("raw-clone.hgo", []byte(`package fixture
#[derive(Clone)]
enum State string { Ready = "ready", Done = "done" }
#[derive(Clone)]
enum Code uint64 { First = 1, Last = 18446744073709551615 }
#[derive(Clone)]
struct Record { State State; Code Code }
macro_rules! clone_state {($value:expr)=>{CloneState($value)};}
func Copy(value *State) State {CloneState:=func(*State)State{panic("captured")};_=CloneState;return clone_state!(value)}
`))
	if e != nil {
		t.Fatal(e)
	}
	runGenerated(t, r.Go, `package fixture
import "testing"
func TestRawCopies(t *testing.T){
 for _,v:=range []State{"ready","done","","future","\x00"}{got:=Copy(&v);if got!=v{t.Fatalf("raw value changed %q => %q",v,got)}}
 for _,v:=range []Code{0,1,17,^Code(0)}{got:=CloneCode(&v);if got!=v{t.Fatalf("raw number changed %d => %d",v,got)}}
 v:=Record{State:"future",Code:42};copied:=CloneRecord(&v);if copied!=v{t.Fatal("nested clone changed raw values")};copied.State="changed";if v.State!="future"{t.Fatal("copy mutated source")}
}
`)
}

func TestRawEnumDeriveBoundaries(t *testing.T) {
	for _, kind := range []string{"Debug", "Serialize", "Deserialize", "Default", "Error", "Clone, Debug"} {
		_, e := frontend.Compile("raw-reject.hgo", []byte("package fixture\n#[derive("+kind+")]\nenum State string { Ready = \"ready\" }\n"))
		if e == nil || !strings.Contains(e.Error(), "raw-reject.hgo") || !strings.Contains(e.Error(), "only Clone") {
			t.Fatalf("%s: %v", kind, e)
		}
	}
	_, e := frontend.Compile("nested-raw.hgo", []byte("package fixture\nenum State string { Ready = \"ready\" }\n#[derive(Clone)]\nstruct Record { State State }\n"))
	if e == nil || !strings.Contains(e.Error(), "requires Clone derive") {
		t.Fatalf("unregistered raw Clone: %v", e)
	}
}

func TestRawCloneCrossFile(t *testing.T) {
	r, e := frontend.CompilePackage("fixture", map[string][]byte{
		"status.hgo": []byte("package fixture\n#[derive(Clone)]\npub enum Status string { Ready=\"ready\" }\n"),
		"record.hgo": []byte("package fixture\n#[derive(Clone)]\nstruct Record { Status Status }\n"),
	}, nil)
	if e != nil {
		t.Fatal(e)
	}
	// No imports in this fixture: combine emitted declarations only for the
	// independent Go execution helper, after package-level compilation succeeds.
	output := "package fixture\n"
	for _, name := range []string{"status.hgo", "record.hgo"} {
		output += strings.TrimPrefix(string(r.Files[name].Go), "package fixture\n")
	}
	runGenerated(t, []byte(output), `package fixture
import "testing"
func TestCrossFile(t *testing.T){v:=Record{Status:Status("future")};copied:=CloneRecord(&v);if copied!=v{t.Fatal("cross-file clone changed value")}}
`)
}
