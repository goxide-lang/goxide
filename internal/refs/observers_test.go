package refs

import (
	"strings"
	"testing"
)

func TestEnumObserversPreserveSharedPayloadQualification(t *testing.T) {
	const declarations = `package p
type Data struct{N int;Items []int;ByName map[string]int}
type E struct{tag int;data Data}
func Validate(e E){if e.tag!=1{panic("tag")}}
func Tag(e E)int{return e.tag}
func Value(e E)Data{return e.data}
func Project(e *E)*Data{return &e.data}
`
	registry := []Projection{{PackagePath: "example/projection", Function: "Project", EnumTypeID: "example/projection.E", PayloadTypeID: "example/projection.Data"}}
	observers := []EnumObserver{
		{PackagePath: "example/projection", Function: "Validate", EnumTypeID: "example/projection.E", Kind: "validate"},
		{PackagePath: "example/projection", Function: "Tag", EnumTypeID: "example/projection.E", Kind: "tag", ResultTypeID: "int"},
		{PackagePath: "example/projection", Function: "Value", EnumTypeID: "example/projection.E", Kind: "value", Field: "data", ResultTypeID: "example/projection.Data"},
	}
	for _, body := range []string{
		`func Read(e &E)&Data{saved:=*e;Validate(saved);if Tag(saved)==1{return __goxide_enum_project_read(Project,e)};panic("tag")}`,
		`func Scalar(e &E)int{saved:=*e;Validate(saved);d:=Value(saved);d.N++;return d.N}`,
	} {
		if _, err := compileProjectionFixture(declarations+body, registry, observers...); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range map[string]string{
		"slice-write":   `func Bad(e &E){saved:=*e;d:=Value(saved);d.Items[0]=2}`,
		"map-write":     `func Bad(e &E){saved:=*e;d:=Value(saved);d.ByName["a"]=2}`,
		"shared-return": `func Bad(e &E)[]int{saved:=*e;return Value(saved).Items}`,
		"raw-boundary":  `func Sink(e E){};func Bad(e &E){saved:=*e;Sink(saved)}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := compileProjectionFixture(declarations+body, registry, observers...)
			if err == nil || !strings.Contains(err.Error(), "readonly") {
				t.Fatalf("lost readonly qualification: %v", err)
			}
		})
	}
	wrong := append([]EnumObserver(nil), observers...)
	wrong[0].PackagePath = "wrong/owner"
	if _, err := compileProjectionFixture(declarations+`func Bad(e &E){Validate(*e)}`, registry, wrong...); err == nil {
		t.Fatal("wrong owner observer accepted")
	}
}

func TestGenericReferenceTypeKeepsCommaArguments(t *testing.T) {
	source := `package p;type Pair[A,B any]struct{First A;Second B};func Keep(p &mut Pair[int,error])&mut Pair[int,error]{return p}`
	prepared, err := Prepare("pair.gox", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := FinishWithEnumTypes("example/p", "pair.gox", prepared.Source, nil, map[string]bool{"example/p.Pair[int, error]": true}); err != nil {
		t.Fatal(err)
	}
}
