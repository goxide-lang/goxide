package lower_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/expand"
	"github.com/hxsf/goxide/internal/lower"
)

func TestConfigurationConstantsAndCommentAttachment(t *testing.T) {
	const source = `// Copyright configuration fixture.
package main
use fmt;
//go:embed missing.txt
#[cfg(any())]
var removed string
#[cfg(any())]
func disabled() { /* disabled body comment */ missing!() }
macro_rules! remove { () => { #[cfg(any())] func ghost() { missing!() } }; }
//go:noinline
remove!()
const active = cfg!(feature("active"))
struct State { Enabled bool }
func returned() bool { return cfg!(all()) }
func main() {
    true := false
    false := !true
    const yes = cfg!(all())
    const no = cfg!(any())
    state := State{Enabled: cfg!(all()) && !cfg!(any())}
    count := 0
    if cfg!(all()) { count++ } else { count += 100 }
    for cfg!(any()) { count += 100 }
    switch cfg!(all()) { case cfg!(all()): count++ }
    fmt::Println(active, returned(), yes, no, state.Enabled, count, true, false)
}
`
	for _, enabled := range []bool{false, true} {
		options := lower.PackageOptions{Features: expand.Features{Enabled: map[string]bool{"active": enabled}, Declared: map[string]bool{"active": true}}}
		inputs := map[string][]byte{"main.gox": []byte(source)}
		output, err := lower.CompilePackage(inputs, "cfgfixture", nil, nil, options)
		if err != nil {
			t.Fatal(err)
		}
		again, err := lower.CompilePackage(inputs, "cfgfixture", nil, nil, options)
		if err != nil || !bytes.Equal(output["main.gox"], again["main.gox"]) {
			t.Fatalf("repeated generation differs: %v", err)
		}
		code := output["main.gox"]
		for _, removed := range []string{"go:embed", "go:noinline", "disabled body", "func disabled", "func ghost"} {
			if bytes.Contains(code, []byte(removed)) {
				t.Fatalf("removed item content %q survived:\n%s", removed, code)
			}
		}
		if !bytes.Contains(code, []byte("Copyright configuration fixture")) || !bytes.Contains(code, []byte("0 == 0")) || !bytes.Contains(code, []byte("0 != 0")) {
			t.Fatalf("header or unshadowable constants lost:\n%s", code)
		}
		directory := t.TempDir()
		for name, data := range map[string][]byte{"go.mod": []byte("module cfgfixture\n\ngo 1.27\n"), "main.go": code} {
			if err := os.WriteFile(filepath.Join(directory, name), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
		runGo(t, directory, "build", "./...")
		want := "false true true false true 2 false true\n"
		if enabled {
			want = strings.Replace(want, "false", "true", 1)
		}
		if got := runGo(t, directory, "run", "."); got != want {
			t.Fatalf("output = %q, want %q", got, want)
		}
	}
}
