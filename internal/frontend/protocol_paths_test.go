package frontend

import (
	"bytes"
	"go/format"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/paths"
	"github.com/hxsf/goxide/internal/protocols"
	"github.com/hxsf/goxide/internal/sourcemap"
)

func TestProtocolUseBindingEvidenceSurvivesAST(t *testing.T) {
	source := []byte("package p\nimport _ \"example/p\"\nvar f=Alias[int,Carrier]\n")
	m := sourcemap.Identity("p.gox", source)
	alias := strings.Index(string(source), "Alias")
	blank := strings.Index(string(source), "_ \"example/p\"")
	out, err := applyBoundUseEdits(source, m, []useEdit{{alias, alias + len("Alias"), "route.Function"}, {blank, blank + 1, "route"}}, map[int]bool{alias: true})
	if err != nil {
		t.Fatal(err)
	}
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, "p.gox", out, 0)
	if err != nil {
		t.Fatal(err)
	}
	evidence := m.CaptureAST(fs, f)
	var printed bytes.Buffer
	if err := format.Node(&printed, fs, f); err != nil {
		t.Fatal(err)
	}
	final := evidence.Print(f, printed.Bytes(), "test printing")
	for _, word := range []string{"route.Function", "Function"} {
		off := strings.LastIndex(printed.String(), word)
		seg := final.Lookup(off)
		if seg == nil || seg.Kind != "use-binding" || seg.Origin == nil || string(source[seg.Origin.Start:seg.Origin.End]) != "Alias" {
			t.Fatalf("canonical function lost binding cause: %s %#v", word, seg)
		}
	}
	seg := final.Lookup(strings.Index(printed.String(), "import route") + len("import "))
	if seg != nil && seg.Kind == "use-binding" {
		t.Fatal("import rewrite acquired source call evidence")
	}
	seg = final.Lookup(strings.Index(printed.String(), "Carrier"))
	if seg == nil || seg.Kind != "source" || seg.Origin == nil || string(source[seg.Origin.Start:seg.Origin.End]) != "Carrier" {
		t.Fatalf("explicit source type argument lost exact origin: %#v", seg)
	}
}

func TestProtocolAssociatedPathsRespectBinding(t *testing.T) {
	protocol := protocols.Definition{ID: protocols.ID{Owner: "example/p", Name: "Build"}, Members: []protocols.Member{{Name: "Abc", GoName: "Abc"}}}
	p := &protocols.Package{Owner: "example/p", Implementations: []protocols.Implementation{{Protocol: protocol.ID, Target: protocols.ID{Owner: "example/p", Name: "U"}, Members: protocol.Members}}, GenericBindings: map[string]map[string][]protocols.Bound{"p.gox": {"Generic": {{Function: "Generic", TypeParam: "T", Protocol: protocol}}}}}
	for _, tc := range []struct {
		name, source, want string
		rewrite            bool
	}{
		{"concrete", "type U struct{}; func f(){ _=U::Abc() }", ".Of[U]().Abc()", true},
		{"function value", "type U struct{}; var f=U::Abc", ".Of[U]().Abc", true},
		{"transparent alias", "type U struct{};type Alias=U;func f(){_=Alias::Abc()}", ".Of[Alias]().Abc()", true},
		{"alias chain", "type U struct{};type First=U;type Second=First;func f(){_=Second::Abc()}", ".Of[Second]().Abc()", true},
		{"generic", "func Generic[T any](){_=T::Abc()}", ".Of[T]().Abc()", true},
		{"generic alias", "func Generic[T any](){type First=T;type Second=First;_=Second::Abc()}", ".Of[Second]().Abc()", true},
		{"value shadow", "type U struct{};func f(){U:=1;_=U::Abc()}", "U::Abc()", false},
		{"parameter shadow", "type U struct{};func f(U int){_=U::Abc()}", "U::Abc()", false},
		{"local type shadow", "type U struct{};func f(){type U int;_=U::Abc()}", "U::Abc()", false},
		{"generic shadow", "func Generic[T any](){type T int;_=T::Abc()}", "T::Abc()", false},
		{"dot spelling", "type U struct{};func f(){_=U.Abc()}", "U.Abc()", false},
		{"native method", "type U struct{};func f(){_=U::Len}", "U::Len", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := []byte("package p\n" + tc.source + "\n")
			mapping := sourcemap.Identity("p.gox", source)
			out, err := lowerProtocolPaths("p.gox", source, mapping, p, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(out), tc.want) || strings.Contains(string(out), protocolTypeInfoAlias) != tc.rewrite {
				t.Fatalf("wrong bound dispatch: %s", out)
			}
		})
	}
}

func TestProtocolImportedPathCanonicalOwnerAndFileAliases(t *testing.T) {
	metadata := protocolTestMetadata()
	pkg := enumTestPackage(t, metadata.Path, protocolTestSource(metadata), nil)
	other := protocolTestMetadata()
	other.Path = "protocol.example/other"
	other.Definitions[0].Owner = other.Path
	other.Implementations[0].Target.Owner = other.Path
	other.Implementations[0].Protocol.Owner = other.Path
	other.Implementations[0].Members[0].GoName = "OtherAbc"
	otherPkg := enumTestPackage(t, other.Path, "package producer\ntype User struct{}\n", nil)
	sources := map[string][]byte{
		"first.gox":   []byte("package p\nimport m \"" + metadata.Path + "\"\ntype Alias=m.User\ntype Again=Alias\nfunc F(){_=Again::Abc()}\n"),
		"second.gox":  []byte("package p\nimport m \"" + other.Path + "\"\nfunc G(){_=m.User::Abc()}\n"),
		"third.gox":   []byte("package p\nfunc H(){_=Again::Abc()}\n"),
		"grouped.gox": []byte("package p\ntype (GroupFirst=Alias;GroupSecond=GroupFirst)\n"),
		"fourth.gox":  []byte("package p\nfunc I(){_=GroupSecond::Abc()}\n"),
	}
	u := &usePackage{metadata: useMetadata{Path: "example/p"}, original: sources, imported: map[string]useMetadata{}, imp: enumTestImporter{metadata.Path: pkg, other.Path: otherPkg}, protocols: map[string]protocols.Metadata{metadata.Path: metadata, other.Path: other}, namespaces: map[string]map[string]paths.Namespace{
		"first.gox": {"m": {PackagePath: metadata.Path}}, "second.gox": {"m": {PackagePath: other.Path}},
	}}
	p := &protocols.Package{Owner: "example/p"}
	for file, source := range sources {
		if file == "grouped.gox" {
			continue
		}
		out, err := lowerProtocolPaths(file, source, sourcemap.Identity(file, source), p, u)
		if err != nil {
			t.Fatal(err)
		}
		want := ".Of[Again]().Abc()"
		if file == "second.gox" {
			want = ".Of[m.User]().OtherAbc()"
		}
		if file == "fourth.gox" {
			want = ".Of[GroupSecond]().Abc()"
		}
		if !strings.Contains(string(out), want) {
			t.Fatalf("%s lost canonical file binding: %s", file, out)
		}
	}
}

func TestProtocolNamespaceSurfaceHidesGeneratedGoABI(t *testing.T) {
	m := protocolTestMetadata()
	pkg := enumTestPackage(t, m.Path, protocolTestSource(m), nil)
	u := &usePackage{metadata: useMetadata{Path: "example/consumer"}, imported: map[string]useMetadata{}, imp: enumTestImporter{m.Path: pkg}, protocols: map[string]protocols.Metadata{m.Path: m}}
	surface, err := u.surface(paths.Namespace{PackagePath: m.Path})
	if err != nil {
		t.Fatal(err)
	}
	if surface["Produce"].Kind != "protocol" || surface["User"].Kind != "type" {
		t.Fatalf("missing source namespace: %#v", surface)
	}
	for _, name := range []string{m.Definitions[0].OpsName, m.Implementations[0].CarrierName} {
		if _, ok := surface[name]; ok {
			t.Fatalf("glob exposed reserved generated helper %s", name)
		}
		if pkg.Scope().Lookup(name) == nil {
			t.Fatalf("native Go API was removed: %s", name)
		}
	}
	prepared, err := prepareUses("example/facade", map[string][]byte{"p.gox": []byte("package facade\npub use producer::*;\n")}, map[string]PackageResult{m.Path: {protocolMetadata: m}}, enumTestImporter{m.Path: pkg}, paths.Roots{"producer": {PackagePath: m.Path}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(prepared.prepared[prepared.generatedFile]), "type "+m.Definitions[0].OpsName) != 1 {
		t.Fatalf("glob duplicated or lost protocol Ops alias: %s", prepared.prepared[prepared.generatedFile])
	}
}

func TestProtocolUnboundGenericDiagnosticAndNativeConstraint(t *testing.T) {
	p := &protocols.Package{}
	source := []byte("package p\nfunc F[T any](){_=T::Abc()}\n")
	_, err := lowerProtocolPaths("p.gox", source, sourcemap.Identity("p.gox", source), p, nil)
	if err == nil || !strings.Contains(err.Error(), "p.gox:2:22:") || !strings.Contains(err.Error(), "protocol bound") {
		t.Fatalf("missing exact unbound member diagnostic: %v", err)
	}
	source = []byte("package p\nfunc F[T interface{M()}](){_=T::M}\n")
	out, err := lowerProtocolPaths("p.gox", source, sourcemap.Identity("p.gox", source), p, nil)
	if err != nil || string(out) != string(source) {
		t.Fatalf("ordinary native method expression claimed: %s %v", out, err)
	}
}

func TestProtocolAssociatedPathSourceEnvelope(t *testing.T) {
	p := &protocols.Package{Implementations: []protocols.Implementation{{Target: protocols.ID{Name: "U"}, Members: []protocols.Member{{Name: "Abc", GoName: "Abc"}}}}}
	for _, nl := range []string{"\n", "\r\n"} {
		source := []byte("package p" + nl + "type U struct{}" + nl + "func f(){_=U::Abc(untouched)}" + nl)
		m := sourcemap.Identity("p.gox", source)
		out, err := lowerProtocolPaths("p.gox", source, m, p, nil)
		if err != nil {
			t.Fatal(err)
		}
		idx := strings.Index(string(out), ".Of[U]")
		seg := m.Lookup(idx)
		if seg == nil || seg.Kind != "protocol-associated" || seg.Origin == nil || string(source[seg.Origin.Start:seg.Origin.End]) != "U::Abc" {
			t.Fatalf("dispatch not causal envelope: %#v", seg)
		}
		idx = strings.Index(string(out), "untouched")
		seg = m.Lookup(idx)
		if seg == nil || seg.Kind != "source" || seg.Origin == nil || string(source[seg.Origin.Start:seg.Origin.End]) != "untouched" {
			t.Fatalf("argument lost exact token: %#v", seg)
		}
		fs := token.NewFileSet()
		f, err := parser.ParseFile(fs, "p.gox", out, 0)
		if err != nil {
			t.Fatal(err)
		}
		evidence := m.CaptureAST(fs, f)
		var printed bytes.Buffer
		if err := format.Node(&printed, fs, f); err != nil {
			t.Fatal(err)
		}
		final := evidence.Print(f, printed.Bytes(), "protocol path test")
		seg = final.Lookup(strings.Index(printed.String(), "Abc"))
		if seg == nil || seg.Kind != "protocol-associated" || seg.Origin == nil || string(source[seg.Origin.Start:seg.Origin.End]) != "U::Abc" {
			t.Fatalf("AST printing lost causal associated path: %#v", seg)
		}
	}
}
