package acceptance_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestT0FinalAuditBusinessRace(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	source := filepath.Join(root, "examples/prototype/final_audit.gox")
	input, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	input = []byte(strings.NewReplacer("enum AuditState", "pub enum AuditState", "enum Receipt", "pub enum Receipt", "    panic(\"unreachable\")\n", "").Replace(string(input)))
	source = filepath.Join(t.TempDir(), "final_audit.gox")
	if err := os.WriteFile(source, input, 0600); err != nil {
		t.Fatal(err)
	}
	result, err := compileAcceptanceLanguageFixture(source, input)
	if err != nil {
		t.Fatal(err)
	}
	generated := t.TempDir()
	packagesWrite(t, generated, "go.mod", "module finalaudit.example/business\ngo 1.27\n")
	packagesWrite(t, generated, "main.go", string(result.Go))
	packagesWrite(t, generated, "audit_test.go", finalAuditAssertions)
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	deliveryCommand(t, generated, tool, true, nil, "test", "-race", "-count=1", "-v", ".")
	cli := filepath.Join(t.TempDir(), "goxide")
	deliveryCommand(t, root, tool, true, nil, "build", "-o", cli, "./cmd/goxide")
	got := deliveryCommand(t, t.TempDir(), cli, true, nil, "run", "--roots", filepath.Join(root, "examples/prototype/goxide.roots.toml"), "--go", tool, "--runtime", root, source)
	if got != "audit webhook done\n" {
		t.Fatalf("unexpected business output: %q", got)
	}
}

func TestT0FinalAuditBoundaries(t *testing.T) {
	cases := []struct{ name, source, want string }{
		{"plain-int", "func f(v int){match v {_=>{},}}", "enum"},
		{"any-erases-identity", "enum E { A, }\nfunc f(v any){match v {_=>{},}}", "enum"},
		{"missing-clone", "enum S { A, }\n#[derive(Clone)]\ntype Record struct { Value S }", "no explicit clone implementation"},
		{"helper-not-protocol", "enum S { A, }\nfunc CloneS(v *S) S{return *v}\n#[derive(Clone)]\ntype Record struct { Value S }", "no explicit clone implementation"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			name := filepath.Join(t.TempDir(), tc.name+".gox")
			_, err := compileAcceptanceLanguageFixture(name, []byte("package boundary\n"+tc.source+"\n"))
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), tc.want) || !strings.Contains(err.Error(), filepath.Base(name)+":") {
				t.Fatalf("want located %s rejection, got %v", tc.want, err)
			}
		})
	}
}

const finalAuditAssertions = `package main
import("reflect";"testing")
func TestAuditClonesClosedVariants(t *testing.T){
 for _,state:=range []AuditState{ready(),done()}{
  original:=state;cloned:=CloneAuditState(&original)
  if isReady(cloned)!=isReady(state) {t.Fatal("Clone changed variant")}
  record:=copyAudit(state)
  if isReady(record.Current.State)!=isReady(state) || record.Current.Revision!=3 || record.Label!="webhook" {t.Fatal(record)}
  changed:=CloneAuditRecord(&record);changed.Current.State=ready()
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
