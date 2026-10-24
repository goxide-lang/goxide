package acceptance_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

func TestT0FinalAuditBusinessRace(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	source := filepath.Join(root, "examples/prototype/final_audit.gox")
	input, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	authored := t.TempDir()
	writeStdFormalBinaryManifest(t, authored, "finalaudit.example/business", "business")
	packagesWrite(t, authored, "final_audit.gox", string(input))
	result, _, err := testdeps.PrepareFormalBinary(t, authored)
	if err != nil {
		t.Fatal(err)
	}
	generated := result.Root
	packagesWrite(t, generated, "audit_test.go", finalAuditAssertions)
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	testdeps.RunFormalGo(t, result, "test", "-mod=readonly", "-race", "-count=1", "-v", ".")
	testdeps.RunFormalGo(t, result, "vet", "-mod=readonly", ".")
	cli := filepath.Join(t.TempDir(), "goxide")
	deliveryCommand(t, root, tool, true, nil, "run", "./scripts/build.go", "-o", cli)
	got := deliveryCommand(t, t.TempDir(), cli, true, nil, "run", "--go", tool, authored)
	if got != "audit webhook done\n" {
		t.Fatalf("unexpected business output: %q", got)
	}
}

func TestT0FinalAuditBoundaries(t *testing.T) {
	cases := []struct{ name, source, want string }{
		{"plain-int", "func f(v int){match v {_=>{},}}", "enum"},
		{"any-erases-identity", "enum E { A, }\nfunc f(v any){match v {_=>{},}}", "enum"},
		{"missing-clone", "use std; enum S { A, }\n#[derive(std::derive::Clone)]\ntype Record struct { Value S }", "missing trait implementation"},
		{"helper-not-protocol", "use std; enum S { A, }\nfunc CloneS(v *S) S{return *v}\n#[derive(std::derive::Clone)]\ntype Record struct { Value S }", "missing trait implementation"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			name := filepath.Join(t.TempDir(), tc.name+".gox")
			var err error
			if tc.name == "missing-clone" || tc.name == "helper-not-protocol" {
				_, err = testdeps.FormalSource(t, name, []byte("package boundary\n"+tc.source+"\n"))
			} else {
				_, err = compileAcceptanceLanguageFixture(name, []byte("package boundary\n"+tc.source+"\n"))
			}
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), tc.want) || !strings.Contains(err.Error(), filepath.Base(name)+":") {
				t.Fatalf("want located %s rejection, got %v", tc.want, err)
			}
			if tc.want == "missing trait implementation" && !strings.Contains(strings.ToLower(err.Error()), "clone") {
				t.Fatalf("missing Clone prerequisite: %v", err)
			}
		})
	}
}

const finalAuditAssertions = `package main
import("reflect";"testing")
func TestAuditClonesClosedVariants(t *testing.T){
 for _,state:=range []AuditState{ready(),done()}{
  original:=state;cloned:=(AuditStateOps{}).Copy(&original)
  if isReady(cloned)!=isReady(state) {t.Fatal("Clone changed variant")}
  record:=copyAudit(state)
  if isReady(record.Current.State)!=isReady(state) || record.Current.Revision!=3 || record.Label!="webhook" {t.Fatal(record)}
  changed:=(AuditRecordOps{}).Copy(&record);changed.Current.State=ready()
  if isReady(record.Current.State)!=isReady(state) {t.Fatal("nested value clone aliases source")}
  trace:=[]string{};calls:=0
  got:=archive(func()AuditState{calls++;trace=append(trace,"read");return cloned},&trace)
  want:="done";wantTrace:=[]string{"read","archived","closed"}
  if isReady(state){want="known";wantTrace=[]string{"read","closed"}}
  if got!=want || calls!=1 || !reflect.DeepEqual(trace,wantTrace){t.Fatalf("got %q calls=%d trace=%v",got,calls,trace)}
  discarded:=0;stateCalls:=0
  discardState(func()AuditState{stateCalls++;return cloned},&discarded)
  if stateCalls!=1||discarded!=1 {t.Fatalf("wildcard calls %d/%d",stateCalls,discarded)}
 }
}
func TestReceiptValidationPrecedesEffects(t *testing.T){
 // Both the nil interface and typed nil variant remain invalid.
 reflectedZero:=reflect.Zero(reflect.TypeOf(accepted())).Interface().(Receipt)
 for _,tc:=range []struct{valid bool;value Receipt}{{true,accepted()},{false,nil},{false,reflectedZero}}{
  valid,value:=tc.valid,tc.value
  trace:=[]string{};reads,guards:=0,0
  panicked:=false
  func(){defer func(){panicked=recover()!=nil}();acknowledge(func()Receipt{reads++;return value},func()bool{guards++;return false},&trace)}()
  if reads!=1 {t.Fatal("scrutinee evaluated more than once",reads)}
  if valid {if panicked||guards!=1||!reflect.DeepEqual(trace,[]string{"fallback"}){t.Fatalf("valid receipt: %v %d %v",panicked,guards,trace)}} else {
   if !panicked||guards!=0||len(trace)!=0 {t.Fatalf("invalid receipt reached effects: %v %d %v",panicked,guards,trace)}
  }
  effects:=0;panicked=false
  func(){defer func(){panicked=recover()!=nil}();discard(func()Receipt{return value},&effects)}()
  if valid && (panicked||effects!=1) {t.Fatal("valid wildcard failed")}
  if !valid && (!panicked||effects!=0) {t.Fatal("invalid wildcard swallowed receipt")}
 }
}
`
