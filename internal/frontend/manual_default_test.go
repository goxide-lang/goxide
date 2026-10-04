package frontend

import (
	"strings"
	"testing"
)

func TestManualDefaultSingleFileAndMacroBinding(t *testing.T) {
	source := `package fixture
type Settings struct { Port int }
impl Default for Settings { func default() Settings { return Settings{Port:8080} } }
#[derive(Default)]
struct Server { Config Settings }
macro_rules! fresh {()=>{DefaultSettings()};}
func Fresh() Settings { DefaultSettings:=func()Settings{panic("captured")};_=DefaultSettings;return fresh!() }
`
	r, err := Compile("manual.gox", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(r.Go), "out.Config = DefaultSettings()") {
		t.Fatal("outer derive did not use registered impl")
	}
	if strings.Contains(string(r.Go), "impl Default") {
		t.Fatal("impl syntax leaked into Go")
	}
}

func TestManualDefaultPackageDiagnostics(t *testing.T) {
	cases := []struct{ name, types, first, second, want string }{
		{"duplicate", `type Settings struct{}`, `impl Default for Settings {func default() Settings{return Settings{}}}`, `impl Default for Settings {func default() Settings{return Settings{}}}`, "duplicate"},
		{"derive-conflict", "#[derive(Default)]\ntype Settings struct{}", `impl Default for Settings {func default() Settings{return Settings{}}}`, "", "duplicate"},
		{"helper-conflict", `type Settings struct{}`, `impl Default for Settings {func default() Settings{return Settings{}}}`, `func DefaultSettings() Settings{return Settings{}}`, "redeclared"},
		{"alias-target", `type Settings = struct{}`, `impl Default for Settings {func default() Settings{return Settings{}}}`, "", "defined non-generic struct"},
		{"scalar-target", `type Settings int`, `impl Default for Settings {func default() Settings{return 1}}`, "", "must be a struct"},
		{"undefined-target", `type Other struct{}`, `impl Default for Settings {func default() Settings{return Settings{}}}`, "", "local defined"},
		{"body-type", `type Settings struct{}`, `impl Default for Settings {func default() Settings{return 42}}`, "", "cannot use"},
		{"mutual-cycle", `type Settings struct{}` + "\n" + `type Other struct{}`, `impl Default for Settings {func default() Settings{DefaultOther();return Settings{}}}`, `impl Default for Other {func default() Other{DefaultSettings();return Other{}}}`, "recursive Default"},
		{"derive-cycle", `type Settings struct{}` + "\n#[derive(Default)]\ntype Outer struct{ S Settings }", `impl Default for Settings {func default() Settings{DefaultOuter();return Settings{}}}`, "", "recursive Default"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string][]byte{"types.gox": []byte("package fixture\n" + tc.types), "impl.gox": []byte("package fixture\n" + tc.first), "other.gox": []byte("package fixture\n" + tc.second)}
			_, err := CompilePackage("fixture", files, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}

func TestManualDefaultMemberBoundaries(t *testing.T) {
	for _, member := range []string{
		`static func default() Settings { return Settings{} }`,
		`func default(self Settings) Settings { return self }`,
		`func default[T any]() Settings { return Settings{} }`,
		`func default() Self { return Self{} }`,
		`func default() (out Settings) { return }`,
		`func default() Settings {return Settings{}}; func extra(){}`,
		"#[default]\nfunc default() Settings {return Settings{}}",
	} {
		t.Run(member, func(t *testing.T) {
			_, err := Compile("members.gox", []byte("package fixture\ntype Settings struct{}\nimpl Default for Settings {"+member+"}\n"))
			if err == nil || !strings.Contains(err.Error(), "manual Default") {
				t.Fatalf("want manual Default rejection, got %v", err)
			}
		})
	}
}

func TestManualDefaultNativeTargetAndResolvedShadow(t *testing.T) {
	_, err := CompilePackage("fixture", map[string][]byte{
		"types.go": []byte("package fixture\ntype Settings struct{Port int}\n"),
		"impl.gox": []byte(`package fixture
impl Default for Settings {func default() Settings { DefaultSettings:=func()Settings{return Settings{Port:8}};return DefaultSettings() }}
#[derive(Default)]
struct Server { Config Settings }
`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDefaultNestedEnumPayload(t *testing.T) {
	_, err := Compile("enum-default.gox", []byte(`package fixture
type NoDefault struct{ N int }
#[derive(Default)]
enum Inner { Other(NoDefault), #[default] Chosen(int), }
#[derive(Default)]
enum Outer { Other(NoDefault), #[default] Chosen(Inner), }
#[derive(Default)]
struct Config { Value Outer }
`))
	if err != nil {
		t.Fatal(err)
	}
}

func TestManualDefaultRecursionUsesSourcePosition(t *testing.T) {
	source := []byte("package fixture\ntype Settings struct{}; impl Default for Settings {func default() Settings{return DefaultSettings()}}\n")
	_, err := Compile("compact.gox", source)
	if err == nil || !strings.Contains(err.Error(), "compact.gox:2:") || !strings.Contains(err.Error(), "recursive Default") {
		t.Fatalf("lost original recursion position: %v", err)
	}
	_, err = CompilePackage("fixture", map[string][]byte{"compact.gox": source}, nil)
	if err == nil || !strings.Contains(err.Error(), "compact.gox:2:") || !strings.Contains(err.Error(), "recursive Default") {
		t.Fatalf("lost package recursion position: %v", err)
	}
}
