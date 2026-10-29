package analysis_test

import (
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/analysis"
)

func TestSignatureHelpTracksNestedArgumentsAndSourceReceivers(t *testing.T) {
	text := `package fixture
func join(first, second int, rest ...string) int {return first+second}
type Value struct{}
impl Value { func read(self &Self, number int, label string) int {return number} }
func f(v Value) { _=join(join(1, 2), 3, "a,b"); _=v.read(7, "x") }
`
	_, queries := analyzeQueries(t, map[string][]byte{"main.gox": []byte(text)})
	for _, test := range []struct {
		needle string
		active int
		label  string
	}{
		{"1, 2", 1, "join(first int, second int, rest ...string) int"},
		{"3, \"a,b\"", 2, "join(first int, second int, rest ...string) int"},
		{"7, \"x\"", 1, "v.read(number int, label string) int"},
	} {
		offset := uint32(strings.Index(text, test.needle) + len(test.needle) - 1)
		help, err := queries.SignatureHelp("fixture/main.gox", offset)
		if err != nil || help == nil || help.ActiveParameter != test.active || help.Label != test.label {
			t.Fatalf("%s: %#v, %v", test.needle, help, err)
		}
	}
}
func TestSignatureHelpRecoversAnUnfinishedCurrentCall(t *testing.T) {
	text := "package fixture\nfunc combine(value int, label string) int {return value}\nfunc f(){ _ = combine(1, "
	result, err := analysis.Analyze(t.Context(), analysis.Input{Sources: map[string][]byte{"main.gox": []byte(text)}, PackagePath: "fixture", Options: analysis.Options{Editor: true}})
	if err != nil {
		t.Fatal(err)
	}
	help, err := analysis.NewQueries(result).SignatureHelp("fixture/main.gox", uint32(len(text)))
	if err != nil || help == nil || help.ActiveParameter != 1 || help.Label != "combine(value int, label string) int" {
		t.Fatalf("help=%#v, %v; diagnostics=%v", help, err, result.Diagnostics)
	}
}
func TestSignatureHelpDoesNotReusePreviousOrUnknownCallees(t *testing.T) {
	first := "package fixture\nfunc f(value int){}\nfunc g(){ f(1) }\n"
	input := analysis.Input{Sources: map[string][]byte{"main.gox": []byte(first)}, PackagePath: "fixture", Options: analysis.Options{Editor: true}}
	previous, err := analysis.Analyze(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	next := strings.Replace(first, "func f(value int)", "func f(value string)", 1)
	input.Sources["main.gox"] = []byte(next)
	input.Options.Previous = previous
	result, err := analysis.Analyze(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	help, err := analysis.NewQueries(result).SignatureHelp("fixture/main.gox", uint32(strings.LastIndex(next, "f(1)")+2))
	if err != nil || help == nil || !strings.Contains(help.Label, "value string") {
		t.Fatalf("current help=%#v, %v", help, err)
	}
	input.Sources["main.gox"] = []byte(strings.Replace(next, "f(1)", "missing(1)", 1))
	result, err = analysis.Analyze(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	help, err = analysis.NewQueries(result).SignatureHelp("fixture/main.gox", uint32(strings.Index(string(input.Sources["main.gox"]), "missing(1)")+8))
	if help != nil || err != nil {
		t.Fatalf("unknown help=%#v %v", help, err)
	}
}
