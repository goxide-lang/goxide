package resolve_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/resolve"
)

type sourceImporter struct {
	testImporter
	packages map[string]bool
}

func (i sourceImporter) SourcePackage(path []string) bool {
	return i.packages[strings.Join(path, "::")]
}

func TestExternalReexportPreservesCanonicalIdentity(t *testing.T) {
	const source = `package main
use fixture::facade::renamed as imported;
use fixture::first::*;
use fixture::second::*;
func main(){_=imported();_=answer()}
`
	canonical := []string{"fixture", "model", "answer"}
	item := func(name string) resolve.External {
		return resolve.External{Name: resolve.Key{Name: name}, Kind: resolve.Function, GoName: "Answer", Visibility: resolve.Public, Target: canonical}
	}
	imports := testImporter{
		"fixture::facade": {item("renamed")},
		"fixture::model":  {item("answer")},
		"fixture::first":  {item("answer")},
		"fixture::second": {item("answer")},
	}
	files := parseFiles(t, map[string]string{"fixture/main.gox": source})
	result := resolve.Resolve(files, imports)
	noDiagnostics(t, result)
	for _, name := range []string{"imported", "answer"} {
		id := named(t, files["fixture/main.gox"], source, "_="+name+"()", name)
		symbol := result.Files["fixture/main.gox"].References[id.ID()].Symbol
		if symbol == nil || symbol.GoName != "Answer" || !reflect.DeepEqual(symbol.Target, canonical) {
			t.Fatalf("%s lost original identity: %#v", name, symbol)
		}
	}
}

func TestSelectedSourceModulePaths(t *testing.T) {
	const source = `package parent
pub mod child;
use child;
use child::answer as read;
func f(){_=child::answer();_=read()}
`
	imports := sourceImporter{
		testImporter: testImporter{"fixture::parent::child": {{Name: resolve.Key{Name: "answer"}, Kind: resolve.Function, GoName: "Answer", Visibility: resolve.Public}}},
		packages:     map[string]bool{"fixture::parent::child": true},
	}
	files := parseFiles(t, map[string]string{"fixture/parent/parent.gox": source})
	result := resolve.Resolve(files, imports)
	noDiagnostics(t, result)
	file := result.Files["fixture/parent/parent.gox"]
	if target := result.Packages[0].Scope.Bindings[resolve.Key{Name: "child"}].Target; !reflect.DeepEqual(target, []string{"fixture", "parent", "child"}) {
		t.Fatalf("module target: %v", target)
	}
	read := named(t, files[file.Name], source, "_=read()", "read")
	resolved := file.References[read.ID()].Symbol
	if resolved == nil || resolved.AliasOf != nil || !reflect.DeepEqual(resolved.Target, []string{"fixture", "parent", "child", "answer"}) {
		t.Fatalf("child import did not retain external route: %#v", resolved)
	}
	delete(imports.packages, "fixture::parent::child")
	result = resolve.Resolve(files, imports)
	found := false
	for _, diagnostic := range result.Diagnostics {
		found = found || diagnostic.Code == "module-source" && diagnostic.File == file.Name
	}
	if !found {
		t.Fatalf("unselected source module accepted: %v", result.Diagnostics)
	}
}
