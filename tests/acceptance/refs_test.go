package acceptance_test

import (
	testdeps "github.com/hxsf/goxide/tests/testutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReferenceConsumerRejections(t *testing.T) {
	cases := map[string]string{
		"range-write":               `func f(r &int){for *r = range 3 {}}`,
		"explicit-write-conversion": `func f(){w:=(&mut int)(nil);*w=1}`,
		"generic-erase":             `func erase[T ~*int](v T)*int{return v};func f(r &int){p:=erase(r);*p=2}`,
		"write-read":                `func f(r &int){*r=2}`,
		"increment-read":            `func f(r &int){(*r)++}`,
		"raw-assignment":            `func f(r &int){var p *int=r;*p=2}`,
		"raw-return":                `func f(r &int)*int{return r}`,
		"raw-call":                  `func sink(p *int){*p=2};func f(r &int){sink(r)}`,
		"raw-conversion":            `func f(r &int){p:=(*int)(r);*p=2}`,
		"interface-box":             `func f(r &int){var p any=r;*p.(*int)=2}`,
		"interface-call":            `func sink(v any){};func f(r &int){sink(r)}`,
		"variadic-interface":        `func sink(v ...any){};func f(r &int){sink(r)}`,
		"read-reborrow-write":       `func f(r &int){p:=&((*r));*p=2}`,
		"read-to-write-reborrow":    `func f(r &int){p:=&mut (*r);*p=2}`,
		"slice-box":                 `func f(r &int){p:=[]*int{r};*p[0]=2}`,
		"map-box":                   `func f(r &int){p:=map[int]*int{0:r};*p[0]=2}`,
		"channel-send":              `func f(r &int,c chan *int){c<-r}`,
		"closure-raw-return":        `func f(r &int)func()*int{return func()*int{return r}}`,
		"closure-any-return":        `func f(r &int)func()any{return func()any{return r}}`,
		"closure-write":             `func f(r &int)func(){return func(){*r=2}}`,
		"raw-tuple-return":          `func f(r &int)(int,*int){return 1,r}`,
		"compound-element":          `type S struct{N int};func f(r &S){r.N=2}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := compileAcceptanceLanguageFixture("reference_reject.gox", []byte("package fixture\n"+body+"\n"))
			if err == nil {
				t.Fatal("accepted permission escape or unsupported reference form")
			}
			if !strings.Contains(err.Error(), "reference_reject.gox") {
				t.Fatalf("missing source location: %v", err)
			}
		})
	}
}

func TestReferenceGeneratedModuleRace(t *testing.T) {
	source := `package fixture
func makeValue() &mut int { n:=4;return &mut n }
func read(r &int)int{return *r}
func capture(r &int)func()int{return func()int{return *r}}
func increment(w &mut int){(*w)++}
func firstClass(r &int)int{g:=read;closure:=func() &int{return r};return g(closure())}
func exercise()int{
 w:=makeValue()
 copy:=w
 increment(copy)
 var r &int = &(*w)
 reader:=capture(r)
 n:=*r
 n++
 {r:=2;r++;n+=r}
 increment(w)
 return firstClass(r)*100+n+reader()-6
}
`
	result, err := compileAcceptanceLanguageFixture("reference_run.gox", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range map[string]string{"go.mod": "module referencefixture\n\ngo 1.27\n", "generated.go": string(result.Go), "generated_test.go": "package fixture\nimport \"testing\"\nfunc TestReferenceLifetimeIdentity(t *testing.T){if n:=exercise();n!=609{t.Fatalf(\"got %d\",n)}}\n"} {
		if err := testdeps.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin/go"), "test", "-race", "-count=1", "-v", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated race module: %v\n%s\n%s", err, output, result.Go)
	}
	t.Logf("generated go test -race -count=1 -v .:\n%s", output)
}

func TestReferenceFrontendRejectsEscapes(t *testing.T) {
	for name, body := range map[string]string{
		"range":   "func f(r &int){for *r=range 3{}}",
		"generic": "func erase[T ~*int](v T)*int{return v};func f(r &int){p:=erase(r);*p=2}",
		"closure": "func f(r &int)func()any{return func()any{return r}}",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := compileAcceptanceLanguageFixture("reference_frontend.gox", []byte("package fixture\n"+body)); err == nil {
				t.Fatal("frontend accepted reference escape")
			}
		})
	}
}
