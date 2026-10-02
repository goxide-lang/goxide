package refs

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"
)

type metadataImporter map[string]*types.Package

func (i metadataImporter) Import(path string) (*types.Package, error) {
	if p := i[path]; p != nil {
		return p, nil
	}
	return nil, fmt.Errorf("unknown %s", path)
}
func TestMetadataIdentityAndValidation(t *testing.T) {
	base := func() Metadata {
		return Metadata{MetadataVersion, "example/api", map[string]Signature{"View": {Params: []Slot{{Type: "*int", Permission: Read}}, Results: []Slot{{Type: "*int", Permission: Read}}}}}
	}
	for _, name := range []string{"valid", "missing", "path", "version", "digest", "signature", "permission", "marker variable"} {
		t.Run(name, func(t *testing.T) {
			m := base()
			marker := m.Marker()
			switch name {
			case "path":
				m.Path = "elsewhere/api"
				marker = m.Marker()
			case "version":
				m.Version++
				marker = m.Marker()
			case "digest":
				m.Functions = map[string]Signature{}
			case "signature":
				s := m.Functions["View"]
				s.Params[0].Type = "*int32"
				m.Functions["View"] = s
				marker = m.Marker()
			case "permission":
				s := m.Functions["View"]
				s.Params[0].Permission = "exclusive"
				m.Functions["View"] = s
				marker = m.Marker()
			}
			fs := token.NewFileSet()
			kind := "const"
			if name == "marker variable" {
				kind = "var"
			}
			f, e := parser.ParseFile(fs, "api.go", fmt.Sprintf("package api\n%s %s=%q\nfunc View(p *int)*int{return p}\n", kind, MetadataMarker, marker), 0)
			if e != nil {
				t.Fatal(e)
			}
			pkg, e := (&types.Config{}).Check("example/api", fs, []*ast.File{f}, nil)
			if e != nil {
				t.Fatal(e)
			}
			deps := map[string]Metadata{"example/api": m}
			if name == "missing" {
				deps = nil
			}
			_, _, e = BindImports(map[string][]byte{"use.go": []byte("package use\nimport x \"example/api\"\nfunc Read()int{n:=1;return *x.View(&n)}\n")}, metadataImporter{"example/api": pkg}, deps)
			if (e == nil) != (name == "valid") {
				t.Fatalf("unexpected %v", e)
			}
			if e != nil && !strings.Contains(e.Error(), "use.go:") {
				t.Fatalf("lost consumer origin: %v", e)
			}
		})
	}
}
