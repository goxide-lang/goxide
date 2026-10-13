package lower_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/lower"
)

func TestEnumAcrossPackages(t *testing.T) {
	source := t.TempDir()
	files := map[string]string{
		"model/model.gox": `package model
pub enum choice[T any] { empty, item(T), record{left T, right int} }
pub enum pair[T, U any] { both(T, U) }
pub func makeValue() choice[int] { return choice[int]::record{left: 4, right: 5} }
`,
		"bridge/bridge.gox": `package bridge
use enumfixture::model;
pub func read(x model::choice[int]) int {
 return match x {
  model::choice::empty => 0,
  model::choice::item(n) => n,
  model::choice::record{left: n, right: m} => n + m,
 }
}
pub func readText(x model::choice[string]) string {
 return match x {
  model::choice::empty => "empty",
  model::choice::item(s) => s,
  model::choice::record{left: s, ..} => s,
 }
}
pub func fromSource() int { return read(model::makeValue()) }
`,
	}
	for name, contents := range files {
		path := filepath.Join(source, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	generated, err := lower.PrepareModule([]string{source}, "enumfixture")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(generated) })
	consumer := `package main
import (
 "fmt"
 "strings"
 "enumfixture/bridge"
 "enumfixture/model"
)
func nilMatch() (message string) {
 defer func(){ message = fmt.Sprint(recover()) }()
 var value model.Choice[int]
 bridge.Read(value)
 return "missing panic"
}
func main() {
 var _ model.Pair[int, string] = &model.Both[int, string]{V0: 1, V1: "two"}
 fmt.Println(bridge.Read(&model.Item[int]{Value: 7}), bridge.FromSource(), bridge.ReadText(&model.Record[string]{Left: "hello", Right: 3}), strings.Contains(nilMatch(), "match on nil choice"))
}
`
	if err := os.WriteFile(filepath.Join(generated, "main.go"), []byte(consumer), 0600); err != nil {
		t.Fatal(err)
	}
	runGo(t, generated, "build", "./...")
	if got := runGo(t, generated, "run", "."); got != "7 9 hello true\n" {
		t.Fatalf("cross-package runtime output = %q", got)
	}
	model, err := os.ReadFile(filepath.Join(generated, "model", "model.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"typeinfo", "ValidateChoice", "TagChoice", "ProjectItem", "ValueItem"} {
		if strings.Contains(string(model), forbidden) {
			t.Fatalf("generated cross-package enum contains %s", forbidden)
		}
	}
	for _, tc := range []struct{ name, declaration, signature string }{
		{"payload", "var _ model.Choice[string] = &model.Item[int]{Value: 42}", "ischoice"},
		{"unit", "var _ model.Choice[string] = &model.Empty[int]{}", "ischoice"},
		{"interface", "var value model.Choice[int]; var _ model.Choice[string] = value", "ischoice"},
		{"second_parameter", "var _ model.Pair[int, string] = &model.Both[int, bool]{}", "ispair"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			invalid := filepath.Join(generated, "invalid.go")
			if err := os.WriteFile(invalid, []byte("package main\nimport \"enumfixture/model\"\n"+tc.declaration+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			defer os.Remove(invalid)
			cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "./...")
			cmd.Dir = generated
			cmd.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off")
			output, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(output), "wrong type for method "+tc.signature) {
				t.Fatalf("cross-instance assignment must fail on marker signature: %v\n%s", err, output)
			}
		})
	}
}
