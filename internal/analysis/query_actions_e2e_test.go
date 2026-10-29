package analysis_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/lower"
)

func TestCodingActionEditsGenerateAndRunStableGo(t *testing.T) {
	text := `package main
trait Reader[T any] { func get(self &Self, value T) int; func create() Self }
type Value struct{}
impl Reader[int] for Value { func create() Self {return Value{}} }
enum Event { Idle, Pair(int, string), Data {number int} }
func number(value Event) int {return match value { Event::Idle => 42, // keep
}}
func main(){ _=Value{}; if number(Event::Idle)!=42 {panic("wrong value")}; println("actions-ok") }
`
	input := analysis.Input{Sources: map[string][]byte{"main.gox": []byte(text)}, PackagePath: "fixture"}
	initial, actions := editorActions(t, input, "impl Reader")
	if len(actions) == 0 {
		t.Fatalf("actions missing: %v checked=%#v", initial.Diagnostics, initial.Checked)
	}
	text = applyAction(t, text, actions, "Implement get")
	input.Sources["main.gox"] = []byte(text)
	_, actions = editorActions(t, input, "match value")
	text = applyAction(t, text, actions, "Add missing match arms")
	input.Sources["main.gox"] = []byte(text)
	result, err := analysis.Analyze(t.Context(), input)
	if err != nil {
		t.Fatalf("edited source: %v\n%s", err, text)
	}
	output, err := lower.Generate(t.Context(), result, lower.GenerationOptions{StableGo: true})
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := lower.Generate(t.Context(), result, lower.GenerationOptions{StableGo: true})
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	for name, data := range output {
		if !bytes.Equal(data, repeated[name]) {
			t.Fatal("nondeterministic generation", name)
		}
		filename := filepath.Join(directory, strings.TrimSuffix(strings.TrimPrefix(name, "fixture/"), ".gox")+".go")
		if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(directory, "go.mod"), []byte("module fixture\n\ngo 1.27\n"), 0644); err != nil {
		t.Fatal(err)
	}
	environment, err := config.NewEnvironment(config.HostTarget(), os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(environment.GoTool, "run", "-mod=readonly", ".")
	command.Dir = directory
	command.Env = append(environment.ExecEnv(), "GOTOOLCHAIN=local", "GOWORK=off")
	log, err := command.CombinedOutput()
	if err != nil || string(log) != "actions-ok\n" {
		t.Fatalf("generated Go run: %v\n%s\n%v", err, log, output)
	}
}
