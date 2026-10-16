package expand

import (
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/config"
	"github.com/hxsf/goxide/internal/syntax/ast"
	"github.com/hxsf/goxide/internal/syntax/printer"
)

func TestConfigurationUsageSurvivesSelectionAndMacroExpansion(t *testing.T) {
	for _, test := range []struct {
		name, source     string
		configured, used bool
	}{
		{"ordinary names", `func cfg() bool { return true }; var cfg_attr = cfg()`, false, false},
		{"unused template", `macro_rules! unused { () => { cfg!(all()) }; }`, false, false},
		{"source query", `var enabled = cfg!(any())`, false, true},
		{"removed source item", `#[cfg(any())] func gone() { missing!() }`, true, true},
		{"unselected source attributes", `#[cfg_attr(any(), derive(Debug))] struct Value {}`, true, true},
		{"dynamic query", `macro_rules! query { ($name:ident) => { $name!(any()) }; }; var enabled = query!(cfg)`, false, true},
		{"dynamic removed item", `macro_rules! item { ($name:ident) => { #[$name(any())] func gone() { missing!() } }; }; item!(cfg)`, false, true},
		{"dynamic unselected attributes", `macro_rules! item { ($name:ident) => { #[$name(any(), derive(Debug))] struct Value {} }; }; item!(cfg_attr)`, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := expandFile(t, "cfg.gox", test.source)
			used, err := Configure(file, config.Config{})
			if err != nil || used != test.configured {
				t.Fatalf("Configure used = %v, want %v; error = %v", used, test.configured, err)
			}
			file = expandFile(t, "cfg.gox", test.source)
			plain := expandFile(t, "plain.gox", "var plain = 1")
			result, err := Expand([]*ast.File{file, plain}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if result.UsesConfiguration != test.used {
				t.Fatalf("Expand UsesConfiguration = %v, want %v", result.UsesConfiguration, test.used)
			}
		})
	}
}

func TestConfigurationPredicates(t *testing.T) {
	features := config.Config{Features: config.Features{Enabled: map[string]bool{"on": true}, Declared: map[string]bool{"on": true, "off": true}}}
	for _, test := range []struct {
		predicate string
		want      bool
	}{
		{`feature("on")`, true}, {`feature("off")`, false},
		{`all()`, true}, {`any()`, false}, {`not(feature("on"))`, false},
		{`all(feature("on"), not(feature("off")),)`, true},
		{"any(\n feature(\"off\"),\n feature(\"on\"),\n)", true},
	} {
		t.Run(test.predicate, func(t *testing.T) {
			file := expandFile(t, "cfg.gox", "var value = cfg!("+test.predicate+")")
			out, err := Expand([]*ast.File{file}, nil, map[*ast.File]config.Config{file: features})
			if err != nil {
				t.Fatal(err)
			}
			if got := expandValue(t, out.Files[0], "value").(*ast.BoolLit).Value; got != test.want {
				t.Fatalf("predicate = %v, want %v", got, test.want)
			}
		})
	}
}

func TestConfigurationValidatesUnusedEnabledNames(t *testing.T) {
	file := expandFile(t, "cfg.gox", "var value = 1")
	if _, err := Configure(file, config.Config{Features: config.Features{Enabled: map[string]bool{"bad/name": true}}}); err == nil || !strings.Contains(err.Error(), "invalid feature name") {
		t.Fatalf("unused invalid feature accepted: %v", err)
	}
}

func TestConfigurationPredicateDiagnostics(t *testing.T) {
	for _, test := range []struct{ source, want string }{
		{`cfg!()`, "exactly one predicate"},
		{`cfg!(all(), any())`, "exactly one predicate"},
		{`cfg!(feature())`, "exactly one string literal"},
		{`cfg!(feature(value))`, "exactly one string literal"},
		{`cfg!(feature("bad/name"))`, "invalid feature name"},
		{`cfg!(not())`, "exactly one predicate"},
		{`cfg!(not(all(), any()))`, "exactly one predicate"},
		{`cfg!(any(all(), unknown()))`, "unknown configuration predicate"},
		{`cfg!(all(any(), feature("typo")))`, "unknown feature"},
		{`cfg!(all(,))`, "empty argument"},
		{`cfg!(test)`, "not supported"},
		{`cfg![all()]`, "parenthesized arguments"},
	} {
		t.Run(test.source, func(t *testing.T) {
			file := expandFile(t, "cfg.gox", "var value = "+test.source)
			_, err := Expand([]*ast.File{file}, nil, map[*ast.File]config.Config{file: {Features: config.Features{Declared: map[string]bool{}}}})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %s", err, test.want)
			}
		})
	}
}

func TestConfigurationSelectsBeforeDiscovery(t *testing.T) {
	file := expandFile(t, "cfg.gox", `
#[cfg(any())] use missing::dependency;
#[cfg(any())] macro_rules! duplicate { () => { missing!() }; }
macro_rules! duplicate { () => { 9 }; }
#[cfg(any())] func gone() { missing!(); }
#[cfg(any())] struct Gone { #[cfg(unknown())] Value Missing }
#[cfg(any())] duplicate!()
#[cfg_attr(any(), cfg(), cfg_attr(), unknown(bad arguments))]
#[cfg_attr(all(), cfg(all()), derive(Debug), cfg_attr(all(), derive(Clone)))]
struct Kept { Value int }
var value = duplicate!()
`)
	result, err := Expand([]*ast.File{file}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files[0].Items) != 2 || len(result.Expansions) != 1 {
		t.Fatalf("items=%d expansions=%d", len(result.Files[0].Items), len(result.Expansions))
	}
	item := result.Files[0].Items[0].(*ast.TypeDecl)
	if len(item.Attrs) != 2 || strings.TrimSpace(printer.Source(item.Attrs[0])) != "#[derive(Debug)]" || strings.TrimSpace(printer.Source(item.Attrs[1])) != "#[derive(Clone)]" {
		t.Fatalf("attribute order: %s", printer.Source(item))
	}
}

func TestConfigurationValidatesAllActiveAttributes(t *testing.T) {
	for _, attributes := range []string{
		`#[cfg(any())] #[cfg(unknown())]`,
		`#[cfg(unknown())] #[cfg(any())]`,
		`#[cfg(any())] #[cfg_attr(any())]`,
		`#[cfg_attr(any(), derive(Debug) derive(Clone))]`,
		`#[cfg_attr(all(), cfg_attr(any()))]`,
		`#[cfg_attr(all(), cfg(any()))] #[cfg(feature("typo"))]`,
	} {
		file := expandFile(t, "cfg.gox", attributes+" struct Value { Field int }")
		if _, err := Configure(file, config.Config{Features: config.Features{Declared: map[string]bool{}}}); err == nil {
			t.Fatalf("accepted invalid current attributes %s", attributes)
		}
	}
}

func TestConfigurationDoesNotLoseOrdinaryMacroAttributes(t *testing.T) {
	for _, source := range []string{
		`#[cfg_attr(all(), unknown)] macro_rules! value { () => { 1 }; }`,
		`macro_rules! value { () => { func generated() {} }; }; #[cfg_attr(all(), unknown)] value!()`,
	} {
		file := expandFile(t, "cfg.gox", source)
		if _, err := Expand([]*ast.File{file}, nil); err == nil || !strings.Contains(err.Error(), "unsupported attribute") {
			t.Fatalf("ordinary macro attribute disappeared: %v", err)
		}
	}
}

func TestConfigurationRejectsNestedPositions(t *testing.T) {
	for _, source := range []string{
		`struct Value { #[cfg(all())] Field int }`,
		`enum Value { #[cfg(all())] First }`,
		`trait Value { #[cfg(all())] func read() int }`,
		`impl Value { #[cfg(all())] func read() int { return 1 } }`,
		`func value() { #[cfg(all())] return }`,
		`const ( #[cfg(all())] Value = 1 )`,
		`type ( #[cfg(all())] Value int )`,
	} {
		file := expandFile(t, "cfg.gox", source)
		_, err := Configure(file, config.Config{})
		if err == nil || !strings.Contains(err.Error(), "complete top-level item") {
			t.Errorf("%s: error = %v", source, err)
		}
	}
}

func TestConfigurationMacroOutputAndOrdinaryIf(t *testing.T) {
	file := expandFile(t, "cfg.gox", `
macro_rules! removed { () => { #[cfg(any())] func gone() { missing!() } }; }
macro_rules! relay { ($item:item) => { $item }; }
removed!()
relay!(#[cfg(any())] func gone2() { missing!() })
var value = cfg!(all())
`)
	result, err := Expand([]*ast.File{file}, nil)
	if err != nil || len(result.Files[0].Items) != 1 || len(result.Expansions) != 2 {
		t.Fatalf("retained output = %#v, error = %v", result, err)
	}
	file = expandFile(t, "cfg.gox", `func value() { if cfg!(any()) { missing!() } }`)
	if _, err = Expand([]*ast.File{file}, nil); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("ordinary if hid invalid macro: %v", err)
	}
}

func TestConfigurationMacroConstructOwnership(t *testing.T) {
	definition := expandFile(t, "definition.gox", `
macro_rules! own { () => { cfg!(feature("local")) }; }
macro_rules! capture { ($value:expr) => { $value }; }
macro_rules! name { ($value:tt) => { cfg!(feature($value)) }; }
macro_rules! predicate { ($($value:tt)*) => { cfg!($($value)*) }; }
macro_rules! own_item { ($name:ident) => { #[cfg(feature("local"))] var $name = 1 }; }
macro_rules! capture_item { ($item:item) => { $item }; }
macro_rules! attribute { ($name:ident, $($attr:tt)*) => { #[cfg_attr(all(), $($attr)*)] var $name = 1 }; }
`)
	caller := expandFile(t, "caller.gox", `
var own = own!()
var captured = capture!(cfg!(feature("local")))
var string_arg = name!("local")
var predicate_arg = predicate!(feature("local"))
own_item!(defined)
capture_item!(#[cfg(feature("local"))] var omitted = 1)
attribute!(captured_attr, cfg(feature("local")))
`)
	features := map[*ast.File]config.Config{
		definition: {Features: config.Features{Enabled: map[string]bool{"local": true}, Declared: map[string]bool{"local": true}}},
		caller:     {Features: config.Features{Declared: map[string]bool{"local": true}}},
	}
	result, err := Expand([]*ast.File{definition, caller}, nil, features)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]bool{"own": true, "captured": false, "string_arg": true, "predicate_arg": true} {
		if got := expandValue(t, result.Files[1], name).(*ast.BoolLit).Value; got != want {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
	if len(result.Files[1].Items) != 5 {
		t.Fatalf("wrong item origin selection: %s", printer.Source(result.Files[1]))
	}
}

func TestConfigurationRemovedCommentsAndReservedNames(t *testing.T) {
	file := expandFile(t, "cfg.gox", `
//go:noinline
#[cfg(any())]
func gone() { /* disabled body */ }
// Kept documents the retained item.
func Kept() {}
`)
	if _, err := Configure(file, config.Config{}); err != nil {
		t.Fatal(err)
	}
	if len(file.Directives) != 0 || len(file.Comments) != 1 || !strings.Contains(file.Comments[0].List[0].Text, "Kept") {
		t.Fatalf("removed item comments survived: %s", printer.Source(file))
	}
	for _, source := range []string{
		`macro_rules! cfg { () => { 1 }; }`,
		`macro_rules! value { () => { 1 }; }; use value as cfg;`,
	} {
		file := expandFile(t, "cfg.gox", source)
		if _, err := Expand([]*ast.File{file}, nil); err == nil || !strings.Contains(err.Error(), "reserved built-in macro") {
			t.Fatalf("reserved macro name accepted: %v", err)
		}
	}
}
