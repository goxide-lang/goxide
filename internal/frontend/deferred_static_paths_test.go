package frontend

import (
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/typecheck"
)

func TestDeferredMatchStaticTypePaths(t *testing.T) {
	const prefix = `package subject
type T struct{}
func(T)M()int{return 9}
func item() Option[int]{return Option::Some(3)}
`
	for _, expression := range []bool{false, true} {
		for _, path := range []string{"T::M(T{})", "(*T)::M(&T{})", "v.M()"} {
			name := "statement/" + path
			body := `result:=0;match item(){Option::Some(n)=>{result=CALL+n},Option::None=>{},};return result`
			if expression {
				name = "expression/" + path
				body = `return match item(){Option::Some(n)=>CALL+n,Option::None=>0,}`
			}
			t.Run(name, func(t *testing.T) {
				source := prefix + `func F()int{v:=T{};_=v.M;` + strings.ReplaceAll(body, "CALL", path) + `}`
				result, err := CompileWithOptions("deferred.gox", []byte(source), FileOptions{Importer: typecheck.NewImporter("deferred.gox")})
				if err != nil {
					t.Fatal(err)
				}
				dir := t.TempDir()
				useWrite(t, dir, map[string]string{
					"go.mod":          "module example.org/deferredstatic\ngo 1.27\n",
					"subject.go":      string(result.Go),
					"subject_test.go": `package subject;import "testing";func TestValue(t *testing.T){if F()!=12{t.Fatal(F())}}`,
				})
				useGo(t, dir, "test", "-mod=readonly", "-count=1", "./...")
			})
		}
	}
}

func TestDeferredMatchRejectsWrongSelectorOperator(t *testing.T) {
	const prefix = `package subject
type T struct{}
func(T)M()int{return 9}
func item() Option[int]{return Option::Some(3)}
`
	for _, expression := range []bool{false, true} {
		for _, tc := range []struct{ call, diagnostic string }{
			{"T.M(T{})", "static package and type paths"},
			{"(*T).M(&T{})", "static package and type paths"},
			{"v::M()", "value members"},
		} {
			name := "statement/" + tc.call
			body := `result:=0;match item(){Option::Some(n)=>{result=CALL+n},Option::None=>{},};return result`
			if expression {
				name = "expression/" + tc.call
				body = `return match item(){Option::Some(n)=>CALL+n,Option::None=>0,}`
			}
			t.Run(name, func(t *testing.T) {
				// The same member spelling occurs before the deferred fragment with
				// a different, valid operator. It cannot supply the arm's origin.
				source := prefix + `func F()int{v:=T{};_=v.M;` + strings.ReplaceAll(body, "CALL", tc.call) + `}`
				_, err := CompileWithOptions("deferred.gox", []byte(source), FileOptions{Importer: typecheck.NewImporter("deferred.gox")})
				if err == nil || !strings.Contains(err.Error(), tc.diagnostic) || !strings.Contains(err.Error(), "deferred.gox:5:") {
					t.Fatalf("got %v, want source-positioned %q", err, tc.diagnostic)
				}
			})
		}
	}
}

func TestDeferredMatchSelectorOriginsWithReferencePreparation(t *testing.T) {
	const source = `package subject
type T struct{}
func(T)M()int{return 9}
func item() Option[int]{return Option::Some(3)}
func F()int{result:=0;match item(){Option::Some(n)=>{var r &int = &n;_=r;result=CALL+n},Option::None=>{},};return result}
`
	for _, call := range []string{"T::M(T{})", "T.M(T{})"} {
		for _, packageMode := range []bool{false, true} {
			name := call + "/single"
			if packageMode {
				name = call + "/package"
			}
			t.Run(name, func(t *testing.T) {
				input := []byte(strings.ReplaceAll(source, "CALL", call))
				imp := typecheck.NewImporter("deferred.gox")
				var err error
				if packageMode {
					_, err = CompilePackageWithOptions("example.org/deferredstatic", map[string][]byte{"deferred.gox": input}, PackageOptions{Importer: imp})
				} else {
					_, err = CompileWithOptions("deferred.gox", input, FileOptions{Importer: imp})
				}
				if strings.Contains(call, "::") {
					if err != nil {
						t.Fatal(err)
					}
				} else if err == nil || !strings.Contains(err.Error(), "static package and type paths") || !strings.Contains(err.Error(), "deferred.gox:5:") {
					t.Fatalf("got %v, want source-positioned static operator error", err)
				}
			})
		}
	}
}
