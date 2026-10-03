package frontend

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/equality"
)

func TestManualEqualityParser(t *testing.T) {
	source := []byte("package fixture\ntype T struct{N int}\nimpl PartialEq for T {\n func equals(self, other T) bool {\n // exact body comment\n return self.N == other.N\n }\n}\nimpl Eq for T {};\n")
	lowered, registry, err := lowerManualEquality("manual.hgo", source)
	if err != nil {
		t.Fatal(err)
	}
	if registry["T"].partial == nil || registry["T"].eq == nil {
		t.Fatal(registry)
	}
	if strings.Count(string(lowered), "\n") != strings.Count(string(source), "\n") || !strings.Contains(string(lowered), "// exact body comment\n return self.N == other.N") {
		t.Fatal("body/lines changed", string(lowered))
	}
	if !strings.Contains(string(lowered), "func EqualsT(self, other T)") {
		t.Fatal(string(lowered))
	}
	if _, e := parser.ParseFile(token.NewFileSet(), "out.go", lowered, 0); e != nil {
		t.Fatal(e, string(lowered))
	}
	for name, body := range map[string]string{
		"static":            "impl PartialEq for T {static func equals(self T, other T) bool{return true}}",
		"associated":        "impl PartialEq for T {func equals(a T, other T) bool{return true}}",
		"pointer":           "impl PartialEq for T {func equals(self *T, other *T) bool{return true}}",
		"wrong-type":        "impl PartialEq for T {func equals(self T, other int) bool{return true}}",
		"generic-target":    "impl PartialEq for T[int] {func equals(self T, other T) bool{return true}}",
		"external-target":   "impl PartialEq for pkg.T {func equals(self T, other T) bool{return true}}",
		"generic-method":    "impl PartialEq for T {func equals[U any](self T, other T) bool{return true}}",
		"extra-param":       "impl PartialEq for T {func equals(self T, a T, b T) bool{return true}}",
		"missing-bool":      "impl PartialEq for T {func equals(self T, other T) int{return 1}}",
		"multiple-results":  "impl PartialEq for T {func equals(self T, other T)(bool,bool){return true,true}}",
		"multiple-members":  "impl PartialEq for T {func equals(self T, other T) bool{return true};func other(){}}",
		"eq-member":         "impl Eq for T {func equals(self T, other T) bool{return true}}",
		"duplicate-partial": "impl PartialEq for T {func equals(self T, other T) bool{return true}};impl PartialEq for T {func equals(self T, other T) bool{return true}}",
		"duplicate-eq":      "impl Eq for T {};impl Eq for T {}",
	} {
		t.Run(name, func(t *testing.T) {
			_, _, e := lowerManualEquality("bad.hgo", []byte("package fixture\n"+body))
			if e == nil || !strings.Contains(e.Error(), "bad.hgo:2:") {
				t.Fatal(e)
			}
		})
	}
}
func TestManualEqualityRegistrations(t *testing.T) {
	parse := func(body string) map[string]manualEquality {
		t.Helper()
		_, r, e := lowerManualEquality("manual.hgo", []byte("package fixture\n"+body))
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	file, e := parser.ParseFile(token.NewFileSet(), "types.go", "package fixture;type T struct{N int};type Alias=T;type Generic[U any] struct{}", 0)
	if e != nil {
		t.Fatal(e)
	}
	specs := typeSpecs(file)
	both := parse("impl PartialEq for T {func equals(self,other T)bool{return true}};impl Eq for T {}")
	derives := map[string]equality.Derive{}
	if e = applyManualEquality("manual.hgo", derives, both, specs, map[string]model{}); e != nil {
		t.Fatal(e)
	}
	d := derives["T"]
	if !d.PartialEq || !d.Eq || !d.ManualPartialEq || !d.ManualEq {
		t.Fatal(d)
	}
	if e = mergeManualEquality(both, both); e == nil {
		t.Fatal("duplicate across files accepted")
	}
	for _, name := range []string{"Alias", "Generic", "Missing"} {
		r := parse("impl PartialEq for " + name + " {func equals(self,other " + name + ")bool{return true}}")
		if e = applyManualEquality("manual.hgo", map[string]equality.Derive{}, r, specs, map[string]model{}); e == nil {
			t.Fatal("invalid target", name)
		}
	}
	partial := parse("impl PartialEq for T {func equals(self,other T)bool{return true}}")
	for _, kind := range []string{"PartialEq", "Eq"} {
		models := map[string]model{"T": {attrs: attrs{derive: map[string]bool{kind: true}}}}
		if e = applyManualEquality("manual.hgo", map[string]equality.Derive{}, partial, specs, models); e == nil {
			t.Fatal("derive conflict", kind)
		}
	}
	marker := parse("impl Eq for T {}")
	if e = applyManualEquality("manual.hgo", map[string]equality.Derive{}, marker, specs, map[string]model{}); e == nil {
		t.Fatal("Eq without PartialEq")
	}
	derived := map[string]equality.Derive{"T": {PartialEq: true}}
	if e = applyManualEquality("type.hgo", derived, marker, specs, map[string]model{"T": {attrs: attrs{derive: map[string]bool{"PartialEq": true}}}}); e != nil || !derived["T"].ManualEq {
		t.Fatal(derived, e)
	}
}

func TestManualEqualityCompilePackage(t *testing.T) {
	for _, declaration := range []string{"type T struct{ P *int }", "type T int", "pub enum T { A, B(int), }"} {
		t.Run(declaration, func(t *testing.T) {
			result, err := CompilePackage("fixture", map[string][]byte{
				"types.hgo":  []byte("package fixture\n" + declaration),
				"method.hgo": []byte("package fixture\nimpl PartialEq for T {func equals(self,other T) bool { return true }}"),
				"marker.hgo": []byte("package fixture\nimpl Eq for T {}"),
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.enumMetadata.Equality) != 1 || result.enumMetadata.Equality[0].PartialEqSource != "manual" || result.enumMetadata.Equality[0].EqSource != "manual" {
				t.Fatalf("manual provenance missing: %+v", result.enumMetadata.Equality)
			}
			got := result.Files["method.hgo"].equalityDerives["T"]
			if !got.PartialEq || !got.Eq || !got.ManualPartialEq || !got.ManualEq {
				t.Fatal(got)
			}
			if strings.Contains(declaration, "enum") && !strings.Contains(string(result.Files["method.hgo"].Go), "__hgo_enum_eq_manual_validator_T") {
				t.Fatal("enum method lacks entry validators")
			}
		})
	}
	_, err := CompilePackage("fixture", map[string][]byte{
		"types.hgo":  []byte("package fixture\n#[derive(PartialEq)]\ntype T struct{F float64}"),
		"marker.hgo": []byte("package fixture\nimpl Eq for T {}"),
	}, nil)
	if err != nil {
		t.Fatal("manual Eq author promise", err)
	}
	for _, tc := range []struct{ name, decl, one, two, want string }{
		{"duplicate", "type T int", "impl Eq for T {}", "impl Eq for T {}", "duplicate Eq"},
		{"derive-conflict", "#[derive(PartialEq)]\ntype T int", "impl PartialEq for T {func equals(self,other T)bool{return true}}", "", "duplicate PartialEq"},
		{"derive-eq-manual-partial", "#[derive(Eq)]\ntype T int", "impl PartialEq for T {func equals(self,other T)bool{return true}}", "", "derive Eq cannot"},
		{"helper-impostor", "type T int", "func EqualsT(a,b T)bool{return true}", "impl Eq for T {}", "requires explicit PartialEq"},
		{"helper-conflict", "type T int", "impl PartialEq for T {func equals(self,other T)bool{return true}}", "func EqualsT(a,b T)bool{return true}", "redeclared"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CompilePackage("fixture", map[string][]byte{"types.hgo": []byte("package fixture\n" + tc.decl), "one.hgo": []byte("package fixture\n" + tc.one), "two.hgo": []byte("package fixture\n" + tc.two)}, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %s got %v", tc.want, err)
			}
		})
	}
}

func TestManualEqualityBodySource(t *testing.T) {
	source := []byte("package fixture\ntype T int\nimpl PartialEq for T {func equals(self,other T)bool{\n return self.Missing == other.Missing\n}}\n")
	_, err := Compile("body.hgo", source)
	if err == nil || !strings.Contains(err.Error(), "body.hgo:4:") {
		t.Fatalf("lost body source location: %v", err)
	}
}
