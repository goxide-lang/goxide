package frontend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/equality"
)

type enumTestImporter map[string]*types.Package

func (i enumTestImporter) Import(path string) (*types.Package, error) { return i[path], nil }
func enumTestPackage(t *testing.T, path, source string, imp types.Importer) *types.Package {
	t.Helper()
	fs := token.NewFileSet()
	f, e := parser.ParseFile(fs, "input.go", source, 0)
	if e != nil {
		t.Fatal(e)
	}
	pkg, e := (&types.Config{Importer: imp}).Check(path, fs, []*ast.File{f}, nil)
	if e != nil {
		t.Fatal(e)
	}
	return pkg
}
func enumTestMetadata() enumMetadata {
	return enumMetadata{Version: enumMetadataVersion, Path: "example.org/producer", Package: "producer", Enums: []enumExport{{Name: "Event", DefinitionPath: "example.org/producer", DefinitionName: "Event", TagField: "tag", Tag: "TagEvent", Validator: "ValidateEvent", Variants: []enumVariantExport{{Name: "Empty", Tag: 1, Constructor: "NewEmpty"}, {Name: "Item", Tag: 2, Field: "item", PayloadAlias: "ItemPayload", Value: "ValueItem", Project: "ProjectItem", Constructor: "NewItem", HasPayload: true}}}}}
}

const enumTestSource = `package producer
type Event struct{tag int;item private}
type private struct{ N int }
type ItemPayload=private
func NewEmpty()Event{return Event{tag:1}}
func NewItem(v private)Event{return Event{tag:2,item:v}}
func TagEvent(v Event)int{return v.tag}
func ValueItem(v Event)private{return v.item}
func ProjectItem(v *Event)*private{return &v.item}
func ValidateEvent(v Event){}
`

func TestEnumMetadataTypedValidation(t *testing.T) {
	producer := enumTestPackage(t, "example.org/producer", enumTestSource, nil)
	imp := enumTestImporter{"example.org/producer": producer}
	if e := validateEnumMetadata(enumTestMetadata(), producer, imp); e != nil {
		t.Fatal(e)
	}
	relay := enumTestPackage(t, "example.org/relay", `package relay;import p "example.org/producer";type Renamed=p.Event`, imp)
	alias := enumTestMetadata()
	alias.Path = "example.org/relay"
	alias.Package = "relay"
	alias.Enums[0].Name = "Renamed"
	if e := validateEnumMetadata(alias, relay, imp); e != nil {
		t.Fatal("transparent alias/private payload", e)
	}
	for _, tc := range []struct{ name, old, new, want string }{
		{"pointer-storage", "type Event struct{tag int;item private}", "type Event struct{tag int;item *private}", "getter payload"},
		{"payload-type", "func NewItem(v private)Event", "func NewItem(v int)Event", "constructor payload"},
		{"wrong-validator", "func ValidateEvent(v Event)", "func ValidateEvent(v any)", "validator signature"},
		{"extra-field", "type Event struct{tag int;item private}", "type Event struct{tag int;item private;extra int}", "payload shape"},
		{"wrong-getter", "func ValueItem(v Event)private", "func ValueItem(v Event)any", "getter payload"},
		{"wrong-projector", "func ProjectItem(v *Event)*private", "func ProjectItem(v *Event)*int", "projector payload"},
		{"exported-storage", "type Event struct{tag int;item private}", "type Event struct{tag int;Item private}", "payload field"},
		{"helper-variable", "func TagEvent(v Event)int{return v.tag}", "var TagEvent=func(v Event)int{return v.tag}", "canonical enum helper"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := strings.Replace(enumTestSource, tc.old, tc.new, 1)
			src = strings.Replace(src, "return Event{tag:2,item:v}", "return Event{tag:2}", 1)
			if tc.name == "pointer-storage" {
				src = strings.Replace(src, "type ItemPayload=private", "type ItemPayload=*private", 1)
			}
			src = strings.Replace(src, "return v.item", "panic(\"stub\")", 1)
			src = strings.Replace(src, "return &v.item", "panic(\"stub\")", 1)
			pkg := enumTestPackage(t, "example.org/producer", src, nil)
			e := validateEnumMetadata(enumTestMetadata(), pkg, enumTestImporter{"example.org/producer": pkg})
			if e == nil || !strings.Contains(e.Error(), tc.want) {
				t.Fatalf("want %s, got %v", tc.want, e)
			}
		})
	}
	t.Run("new-defined-struct-is-not-alias", func(t *testing.T) {
		p := enumTestPackage(t, "example.org/relay", `package relay;import p "example.org/producer";type Renamed p.Event`, imp)
		if e := validateEnumMetadata(alias, p, imp); e == nil || !strings.Contains(e.Error(), "alias identity") {
			t.Fatal(e)
		}
	})
	t.Run("duplicate-variant", func(t *testing.T) {
		m := enumTestMetadata()
		m.Enums[0].Variants[1].Name = "Empty"
		if e := validateEnumMetadata(m, producer, imp); e == nil {
			t.Fatal("duplicate accepted")
		}
	})
	t.Run("same-name-other-package", func(t *testing.T) {
		p := enumTestPackage(t, "example.org/other", enumTestSource, nil)
		if e := validateEnumMetadata(enumTestMetadata(), p, imp); e == nil {
			t.Fatal("wrong package accepted")
		}
	})
}

type enumArtifactTestImporter struct {
	enumTestImporter
	dir, content string
}

func (i *enumArtifactTestImporter) PackageFiles(string) (string, []string, error) {
	return i.dir, []string{"producer.go"}, nil
}
func (i *enumArtifactTestImporter) PackageFingerprint(string) (string, error) { return i.content, nil }
func TestEnumArtifactBindings(t *testing.T) {
	m := enumTestMetadata()
	source := []byte(enumTestSource + fmt.Sprintf("\nconst %s=%q\n", enumMetadataMarker, m.Marker()))
	dir := t.TempDir()
	file := filepath.Join(dir, "producer.go")
	if e := os.WriteFile(file, source, 0600); e != nil {
		t.Fatal(e)
	}
	imp := &enumArtifactTestImporter{enumTestImporter: enumTestImporter{m.Path: enumTestPackage(t, m.Path, string(source), nil)}, dir: dir, content: "selected-build-and-dependency-closure"}
	data, e := marshalEnumArtifact(m, map[string][]byte{"producer.go": source}, imp)
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, enumMetadataFilename)
	write := func(data []byte) {
		t.Helper()
		if e := os.WriteFile(path, data, 0600); e != nil {
			t.Fatal(e)
		}
	}
	write(data)
	got, e := loadEnumArtifact(m.Path, imp)
	if e != nil || got.Marker() != m.Marker() {
		t.Fatal(got, e)
	}
	repeated, e := marshalEnumArtifact(m, map[string][]byte{"producer.go": source}, imp)
	if e != nil || !bytes.Equal(data, repeated) {
		t.Fatal("nondeterministic artifact", e)
	}
	for _, tc := range []struct {
		name, want string
		mutate     func(*enumArtifact)
	}{
		{"version", "unsupported metadata version", func(a *enumArtifact) { a.ArtifactVersion = 2 }},
		{"retired-interface-schema", "unsupported metadata version", func(a *enumArtifact) { a.Metadata.Version = 8 }},
		{"path", "package path mismatch", func(a *enumArtifact) { a.Metadata.Path = "wrong.example/pkg" }},
		{"marker", "marker digest mismatch", func(a *enumArtifact) { a.Metadata.Enums[0].Variants[0].Name = "Changed" }},
		{"files", "source set mismatch", func(a *enumArtifact) { a.Files = map[string]string{} }},
		{"configuration", "build configuration changed", func(a *enumArtifact) { a.Content = "other" }},
		{"dependencies", "dependency set mismatch", func(a *enumArtifact) { a.Dependencies = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var a enumArtifact
			if e := json.Unmarshal(data, &a); e != nil {
				t.Fatal(e)
			}
			tc.mutate(&a)
			changed, e := json.Marshal(a)
			if e != nil {
				t.Fatal(e)
			}
			write(changed)
			_, e = loadEnumArtifact(m.Path, imp)
			if e == nil || !strings.Contains(e.Error(), tc.want) {
				t.Fatalf("want %s got %v", tc.want, e)
			}
		})
	}
	t.Run("duplicate-json-key", func(t *testing.T) {
		write(bytes.Replace(data, []byte(`"ArtifactVersion": 1`), []byte(`"ArtifactVersion": 1, "ArtifactVersion": 1`), 1))
		if _, e := loadEnumArtifact(m.Path, imp); e == nil || !strings.Contains(e.Error(), "duplicate object key") {
			t.Fatal(e)
		}
	})
	t.Run("unknown-json-field", func(t *testing.T) {
		write(append([]byte(`{"Unknown":true,`), data[1:]...))
		if _, e := loadEnumArtifact(m.Path, imp); e == nil || !strings.Contains(e.Error(), "unknown field") {
			t.Fatal(e)
		}
	})
	t.Run("source-drift", func(t *testing.T) {
		write(data)
		if e := os.WriteFile(file, append(source, []byte("\n// changed\n")...), 0600); e != nil {
			t.Fatal(e)
		}
		if _, e := loadEnumArtifact(m.Path, imp); e == nil || !strings.Contains(e.Error(), "source content mismatch") {
			t.Fatal(e)
		}
	})
}

const enumFamilyTestSource = `package producer
type Option[T any] struct{tag uint8;some T}
func NewSome[T any](v T)Option[T]{return Option[T]{tag:1,some:v}}
func NewNone[T any]()Option[T]{return Option[T]{}}
func TagOption[T any](v Option[T])int{return int(v.tag)}
func ValidateOption[T any](v Option[T]){}
func ValueSome[T any](v Option[T])T{return v.some}
func ProjectSome[T any](v *Option[T])*T{return &v.some}
type private struct{N int}
func CheckOption(v Option[private]){}
type Result[T any,E any] struct{tag uint8;ok T;err E}
func NewOk[T any,E any](v T)Result[T,E]{return Result[T,E]{tag:1,ok:v}}
func NewErr[T any,E any](v E)Result[T,E]{return Result[T,E]{tag:2,err:v}}
func TagResult[T any,E any](v Result[T,E])int{return int(v.tag)}
func ValidateResult[T any,E any](v Result[T,E]){}
func ValueOk[T any,E any](v Result[T,E])T{return v.ok}
func ValueErr[T any,E any](v Result[T,E])E{return v.err}
func ProjectOk[T any,E any](v *Result[T,E])*T{return &v.ok}
func ProjectErr[T any,E any](v *Result[T,E])*E{return &v.err}
func CheckResult(v Result[int,error]){}
`

func enumFamilyTestMetadata() enumMetadata {
	option, result := sharedSumFamily("Option"), sharedSumFamily("Result")
	for _, f := range []*enumFamilyExport{&option, &result} {
		f.DefinitionPath = "example.org/producer"
		f.Instances = []enumFamilyInstanceExport{{ValidatorPath: "example.org/producer", Validator: "Check" + f.Family}}
	}
	option.Variants[1].Constructor = "NewNone"
	return enumMetadata{Version: enumMetadataVersion, Path: "example.org/producer", Package: "producer", Families: []enumFamilyExport{option, result}}
}

func TestEnumFamilyMetadataValidation(t *testing.T) {
	producer := enumTestPackage(t, "example.org/producer", enumFamilyTestSource, nil)
	imp := enumTestImporter{"example.org/producer": producer}
	if e := validateEnumFamilies(enumFamilyTestMetadata(), producer, imp); e != nil {
		t.Fatal(e)
	}
	relay := enumTestPackage(t, "example.org/relay", `package relay;import p "example.org/producer";type Concrete=p.Result[int,error];type Reordered[A any,B any]=p.Result[B,A];type Partial[T any]=p.Result[T,error]`, imp)
	for _, name := range []string{"Concrete", "Reordered", "Partial"} {
		t.Run(name, func(t *testing.T) {
			m := enumFamilyTestMetadata()
			m.Path = relay.Path()
			m.Package = relay.Name()
			m.Families = m.Families[1:]
			m.Families[0].Name = name
			if e := validateEnumFamilies(m, relay, imp); e != nil {
				t.Fatal(e)
			}
		})
	}
	for _, tc := range []struct {
		name   string
		change func(*enumMetadata)
		want   string
	}{
		{"variant-order", func(m *enumMetadata) { v := m.Families[1].Variants; v[0], v[1] = v[1], v[0] }, "variant protocol"},
		{"payload-index", func(m *enumMetadata) { m.Families[1].Variants[1].PayloadIndex = 0 }, "variant protocol"},
		{"none-projector", func(m *enumMetadata) { m.Families[0].Variants[1].Project = "ProjectSome" }, "None must not"},
		{"missing-instance", func(m *enumMetadata) { m.Families[0].Instances = nil }, "invalid or duplicate"},
		{"wrong-instance-family", func(m *enumMetadata) { m.Families[0].Instances[0].Validator = "CheckResult" }, "validator family"},
		{"wrong-tag", func(m *enumMetadata) { m.Families[0].Tag = "ValueSome" }, "tag signature"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := enumFamilyTestMetadata()
			tc.change(&m)
			e := validateEnumFamilies(m, producer, imp)
			if e == nil || !strings.Contains(e.Error(), tc.want) {
				t.Fatalf("want %s got %v", tc.want, e)
			}
		})
	}
	t.Run("projector-parameter-order", func(t *testing.T) {
		src := strings.Replace(enumFamilyTestSource, "func ProjectOk[T any,E any](v *Result[T,E])*T{return &v.ok}", "func ProjectOk[T any,E any](v *Result[E,T])*T{return &v.err}", 1)
		p := enumTestPackage(t, producer.Path(), src, nil)
		e := validateEnumFamilies(enumFamilyTestMetadata(), p, enumTestImporter{p.Path(): p})
		if e == nil || !strings.Contains(e.Error(), "projector payload") {
			t.Fatal(e)
		}
	})
	t.Run("new-defined-not-alias", func(t *testing.T) {
		p := enumTestPackage(t, "example.org/relay", `package relay;import p "example.org/producer";type Fake[T any,E any] p.Result[T,E]`, imp)
		m := enumFamilyTestMetadata()
		m.Path = p.Path()
		m.Package = p.Name()
		m.Families = m.Families[1:]
		m.Families[0].Name = "Fake"
		if e := validateEnumFamilies(m, p, imp); e == nil || !strings.Contains(e.Error(), "alias identity") {
			t.Fatal(e)
		}
	})
}

func TestEnumFamilyArtifactRoundTrip(t *testing.T) {
	m := enumFamilyTestMetadata()
	source := []byte(enumFamilyTestSource + fmt.Sprintf("\nconst %s=%q\n", enumMetadataMarker, m.Marker()))
	dir := t.TempDir()
	if e := os.WriteFile(filepath.Join(dir, "producer.go"), source, 0600); e != nil {
		t.Fatal(e)
	}
	imp := &enumArtifactTestImporter{enumTestImporter: enumTestImporter{m.Path: enumTestPackage(t, m.Path, string(source), nil)}, dir: dir, content: "family-build"}
	data, e := marshalEnumArtifact(m, map[string][]byte{"producer.go": source}, imp)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dir, enumMetadataFilename), data, 0600); e != nil {
		t.Fatal(e)
	}
	got, e := loadEnumArtifact(m.Path, imp)
	if e == nil || !strings.Contains(e.Error(), "origin is not shared std") {
		t.Fatalf("legacy family origin accepted under new schema: %v %v", got, e)
	}
	for _, tc := range []struct{ name, source, want string }{
		{"getter-parameter-order", strings.Replace(enumFamilyTestSource, "func ValueOk[T any,E any](v Result[T,E])T{return v.ok}", "func ValueOk[T any,E any](v Result[E,T])T{return v.err}", 1), "getter payload"},
		{"constructor-result-order", strings.Replace(enumFamilyTestSource, "func NewOk[T any,E any](v T)Result[T,E]{return Result[T,E]{tag:1,ok:v}}", "func NewOk[T any,E any](v T)Result[E,T]{return Result[E,T]{}}", 1), "constructor payload"},
		{"generic-instance-validator", strings.Replace(enumFamilyTestSource, "func CheckResult(v Result[int,error])", "func CheckResult[T any](v Result[T,error])", 1), "helper signature"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := enumTestPackage(t, m.Path, tc.source, nil)
			e := validateEnumFamilies(m, p, enumTestImporter{m.Path: p})
			if e == nil || !strings.Contains(e.Error(), tc.want) {
				t.Fatalf("want %s got %v", tc.want, e)
			}
		})
	}
}

func equalityTestMetadata() enumMetadata {
	return enumMetadata{Version: enumMetadataVersion, Path: "example.org/equality", Package: "equality", Equality: []equalityExport{{Name: "Value", DefinitionPath: "example.org/equality", DefinitionName: "Value", Helper: "EqualsValue", Eq: true, PartialEqSource: "derive", EqSource: "derive"}}}
}

const equalityTestSource = `package equality
type Value int
func EqualsValue(a,b Value)bool{return a==b}
`

func TestEqualityMetadataValidation(t *testing.T) {
	p := enumTestPackage(t, "example.org/equality", equalityTestSource, nil)
	imp := enumTestImporter{p.Path(): p}
	m := equalityTestMetadata()
	if !m.present() {
		t.Fatal("equality-only metadata absent")
	}
	if e := validateEnumMetadata(m, p, imp); e != nil {
		t.Fatal(e)
	}
	relay := enumTestPackage(t, "example.org/relay", `package relay;import p "example.org/equality";type Alias=p.Value`, imp)
	m.Path = relay.Path()
	m.Package = relay.Name()
	m.Equality[0].Name = "Alias"
	if e := validateEnumMetadata(m, relay, imp); e != nil {
		t.Fatal("transparent alias", e)
	}
	defined := enumTestPackage(t, relay.Path(), `package relay;import p "example.org/equality";type Alias p.Value`, imp)
	if e := validateEnumMetadata(m, defined, imp); e == nil || !strings.Contains(e.Error(), "alias identity") {
		t.Fatal("new defined type", e)
	}
	for _, tc := range []struct{ name, helper string }{
		{"wrong-left", "func EqualsValue(a int,b Value)bool{return true}"},
		{"wrong-right", "func EqualsValue(a Value,b int)bool{return true}"},
		{"pointer", "func EqualsValue(a,b *Value)bool{return true}"},
		{"named-bool", "type Boolean bool;func EqualsValue(a,b Value)Boolean{return true}"},
		{"variadic", "func EqualsValue(a Value,b ...Value)bool{return true}"},
		{"generic", "func EqualsValue[T any](a,b Value)bool{return true}"},
		{"function-variable", "var EqualsValue=func(a,b Value)bool{return true}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := enumTestPackage(t, "example.org/equality", "package equality;type Value int;"+tc.helper, nil)
			if e := validateEnumMetadata(equalityTestMetadata(), p, enumTestImporter{p.Path(): p}); e == nil {
				t.Fatal("invalid helper accepted")
			}
		})
	}
	for _, sources := range [][2]string{{"", "derive"}, {"native", "derive"}, {"manual", "derive"}, {"derive", ""}, {"derive", "native"}} {
		m := equalityTestMetadata()
		m.Equality[0].PartialEqSource = sources[0]
		m.Equality[0].EqSource = sources[1]
		if e := validateEnumMetadata(m, p, imp); e == nil {
			t.Fatalf("accepted invalid sources %v", sources)
		}
	}
	for _, sources := range [][2]string{{"manual", "manual"}, {"derive", "manual"}} {
		m := equalityTestMetadata()
		m.Equality[0].PartialEqSource = sources[0]
		m.Equality[0].EqSource = sources[1]
		if e := validateEnumMetadata(m, p, imp); e != nil {
			t.Fatalf("valid sources %v: %v", sources, e)
		}
	}
	m = equalityTestMetadata()
	m.Equality[0].Eq = false
	if e := validateEnumMetadata(m, p, imp); e == nil {
		t.Fatal("accepted Eq source without Eq")
	}
	t.Run("duplicate", func(t *testing.T) {
		m := equalityTestMetadata()
		m.Equality = append(m.Equality, m.Equality[0])
		if e := validateEnumMetadata(m, p, imp); e == nil {
			t.Fatal("duplicate accepted")
		}
	})
	t.Run("no-registration-from-helper", func(t *testing.T) {
		got, e := collectEnumExports(p.Path(), PackageResult{}, p)
		if e != nil || got.present() {
			t.Fatal(got, e)
		}
	})
}

type equalityArtifactImporter struct {
	enumTestImporter
	dirs map[string]string
}

func (i equalityArtifactImporter) PackageFiles(path string) (string, []string, error) {
	return i.dirs[path], []string{"source.go"}, nil
}
func (i equalityArtifactImporter) PackageFingerprint(path string) (string, error) {
	return "stable:" + path, nil
}
func TestEqualityArtifactOriginAuthorization(t *testing.T) {
	imp := equalityArtifactImporter{enumTestImporter: enumTestImporter{}, dirs: map[string]string{}}
	write := func(m enumMetadata, body string) {
		t.Helper()
		src := []byte(body + fmt.Sprintf("\nconst %s=%q\n", enumMetadataMarker, m.Marker()))
		pkg := enumTestPackage(t, m.Path, string(src), imp)
		imp.enumTestImporter[m.Path] = pkg
		dir := t.TempDir()
		imp.dirs[m.Path] = dir
		if e := os.WriteFile(filepath.Join(dir, "source.go"), src, 0600); e != nil {
			t.Fatal(e)
		}
		data, e := marshalEnumArtifact(m, map[string][]byte{"source.go": src}, imp)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(dir, enumMetadataFilename), data, 0600); e != nil {
			t.Fatal(e)
		}
	}
	origin := equalityTestMetadata()
	origin.Equality[0].Eq = false
	origin.Equality[0].EqSource = ""
	write(origin, equalityTestSource)
	if _, e := loadEnumArtifact(origin.Path, imp); e != nil {
		t.Fatal("equality-only artifact", e)
	}
	relay := origin
	relay.Equality = append([]equalityExport(nil), origin.Equality...)
	relay.Path = "example.org/relay"
	relay.Package = "relay"
	relay.Equality[0].Name = "Alias"
	source := `package relay;import p "example.org/equality";type Alias=p.Value`
	write(relay, source)
	if _, e := loadEnumArtifact(relay.Path, imp); e != nil {
		t.Fatal("authorized alias", e)
	}
	relay.Equality[0].Eq = true
	relay.Equality[0].EqSource = "derive"
	write(relay, source)
	if _, e := loadEnumArtifact(relay.Path, imp); e == nil || !strings.Contains(e.Error(), "equality alias does not match") {
		t.Fatal("relay elevated Eq", e)
	}
	relay.Equality[0].Eq = false
	relay.Equality[0].EqSource = ""
	relay.Equality[0].PartialEqSource = "manual"
	write(relay, source)
	if _, e := loadEnumArtifact(relay.Path, imp); e == nil || !strings.Contains(e.Error(), "equality alias does not match") {
		t.Fatal("relay changed protocol source", e)
	}
}

func TestCollectEqualityRegistrations(t *testing.T) {
	source := equalityTestSource + "\ntype Alias=Value;type Fake Value\n"
	p := enumTestPackage(t, "example.org/equality", source, nil)
	result := PackageResult{Files: map[string]Result{"a.gox": {equalityDerives: map[string]equality.Derive{"Value": {PartialEq: true, Eq: true}}}}}
	m, e := collectEnumExports(p.Path(), result, p)
	if e != nil {
		t.Fatal(e)
	}
	if len(m.Equality) != 2 || m.Equality[0].Name != "Alias" || m.Equality[1].Name != "Value" || !m.Equality[0].Eq {
		t.Fatal(m.Equality)
	}
	if e = validateEnumMetadata(m, p, enumTestImporter{p.Path(): p}); e != nil {
		t.Fatal(e)
	}
	relay := enumTestPackage(t, "example.org/relay", `package relay;import p "example.org/equality";type Another=p.Value;type Fake p.Value`, enumTestImporter{p.Path(): p})
	exported, e := collectEnumExports(relay.Path(), PackageResult{enumImports: map[string]enumMetadata{p.Path(): m}}, relay)
	if e != nil || len(exported.Equality) != 1 || exported.Equality[0].Name != "Another" || exported.Equality[0].DefinitionPath != p.Path() {
		t.Fatal(exported, e)
	}
}
