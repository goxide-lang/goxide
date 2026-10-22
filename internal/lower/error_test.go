package lower_test

import (
	"strings"
	"testing"
)

func TestErrorDeriveMessagesSourcesAndExplicitHelper(t *testing.T) {
	module, err := supportModule(t, `package main
use errors;
var cause = errors::New("cause")
#[derive(Error)]
enum Failure {
    #[error("input failed")]
    #[from]
    Input(error),
    #[error("bad count {Value}")]
    Count(int),
    #[error("missing")]
    Missing,
}
type (
    #[derive(Error)]
    #[error("cannot open {Name}")]
    Request struct {
        Name string
        #[source]
        Cause error
    }
)
func load() Result[int,Failure] {
    n := Result[int,Failure]::Err(FromFailure(cause))?
    return Result[int,Failure]::Ok(n)
}

func main() {
    failure := match load() { Result[int,Failure]::Err(err) => err, _ => Failure::Missing }
    counted := Failure::Count(3)
    request := Request{Name:"data",Cause:cause}
    println!("{} {} {} {} {}", failure.Error(), errors::Is(failure,cause), counted.Error(), request.Error(), errors::Is(request,cause))
}
`)
	if err != nil {
		t.Fatal(err)
	}
	runGo(t, module, "build", "./...")
	want := "input failed true bad count 3 cannot open data true\n"
	if got := runGo(t, module, "run", "."); got != want {
		t.Fatalf("Error derive output = %q, want %q", got, want)
	}
}

func TestErrorDeriveHelperIsNotConversionEvidence(t *testing.T) {
	_, err := supportModule(t, `package main
#[derive(Error)] #[error("wrapped")]
type Failure struct { #[from] Cause error }
func load(value Result[int,error]) Result[int,Failure] {
    return Result[int,Failure]::Ok(value?)
}
func main(){}
`)
	if err == nil || !strings.Contains(err.Error(), "canonical github.com/goxide-lang/std/convert::From is unavailable") {
		t.Fatalf("Error helper registered implicit conversion evidence: %v", err)
	}
}

func TestErrorDeriveRejectsInvalidSourceAndMessage(t *testing.T) {
	for _, test := range []struct{ name, declaration, want string }{
		{"source-type", `#[derive(Error)] enum E { #[error("x")] #[source] A(int), }`, "error payload"},
		{"duplicate-from", `#[derive(Error)] enum E { #[error("x")] #[from] A(error), #[error("y")] #[from] B(error), }`, "one from"},
		{"unknown-field", `#[derive(Error)] enum E { #[error("{Missing}")] A(int), }`, "unknown error placeholder"},
		{"from-extra-field", `#[derive(Error)] #[error("x")] type E struct { #[from] Cause error; Extra int }`, "exactly one error field"},
		{"from-enum", `#[derive(Error)] #[from] enum E { #[error("failure")] Failure, }`, "invalid attribute position"},
		{"source-struct", `#[derive(Error)] #[error("x")] #[source] type E struct { Cause error }`, "invalid attribute position"},
		{"message-field", `#[derive(Error)] #[error("x")] type E struct { #[error("field")] Cause error }`, "invalid attribute position"},
		{"existing-error", `#[derive(Error)] #[error("x")] type E struct{}; func (E) Error() string { return "manual" }`, "conflicts with an existing member"},
		{"existing-unwrap", `#[derive(Error)] #[error("x")] type E struct { #[source] Cause error }; func (E) Unwrap() error { return nil }`, "conflicts with an existing member"},
	} {
		t.Run(test.name, func(t *testing.T) {
			module, err := supportModule(t, "package main\n"+test.declaration+"\nfunc main(){}\n")
			if module != "" || err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("module=%q error=%v, want %q", module, err, test.want)
			}
		})
	}
}
