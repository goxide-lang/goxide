package lower_test

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/compile"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/paths"
	testdeps "github.com/goxide-lang/goxide/tests/testutil"
	pm "github.com/goxide-lang/syntax/proc_macro"
	public "github.com/goxide-lang/syntax/token"
)

func runtimeRootOutput(t *testing.T, ctx pm.Context, logical, suffix string) pm.Output {
	t.Helper()
	root, err := ctx.RuntimeRoot(logical)
	if err != nil {
		return pm.Failure(err.Error(), ctx.CallSpan())
	}
	tail, err := public.ParseText(suffix, ctx.DefSite())
	if err != nil {
		t.Fatal(err)
	}
	return pm.Success(public.New(append(root.Tokens(), tail.Tokens()...)...))
}

func TestRuntimeRootBindingsAndMissingCapabilities(t *testing.T) {
	_, library, imports := compileProcFixture(t, procFixtureSource)
	root := analysis.RuntimeRoot{LogicalModule: "consumer", BindingID: "consumer-lib-content", Namespace: paths.Namespace{PackagePath: "consumer", ModulePath: "consumer"}}
	aliases := paths.Roots{
		"macros": {PackagePath: procFixturePackage, ModulePath: procFixturePackage},
		"first":  root.Namespace, "renamed": root.Namespace,
	}
	sources := map[string][]byte{"main.gox": []byte("package main\nuse macros;\npub func Value() int { return 42 }\nfunc private() int { return 99 }\nfunc main() { println(macros::identity!()) }\n")}
	for _, test := range []struct {
		name  string
		roots []analysis.RuntimeRoot
		query string
		want  string
	}{
		{name: "self", roots: []analysis.RuntimeRoot{root}, query: "consumer"},
		{name: "duplicate aliases", roots: []analysis.RuntimeRoot{root, root}, query: "consumer"},
		{name: "missing", query: "consumer", want: `runtime root "consumer" is not available`},
		{name: "logical identity only", roots: []analysis.RuntimeRoot{root}, query: "replacement.example/module", want: `runtime root "replacement.example/module" is not available`},
		{name: "ambiguous content", roots: []analysis.RuntimeRoot{root, {LogicalModule: root.LogicalModule, BindingID: "other-content", Namespace: root.Namespace}}, query: "consumer", want: `runtime root "consumer" is not available`},
		{name: "inactive", roots: []analysis.RuntimeRoot{{LogicalModule: root.LogicalModule, BindingID: root.BindingID, Namespace: paths.Namespace{PackagePath: "consumer", ModulePath: "consumer", Unavailable: "inactive"}}}, query: "consumer", want: `runtime root "consumer" is not available`},
	} {
		t.Run(test.name, func(t *testing.T) {
			library.Run = procFixtureHost(pm.Entry{Name: "identity", Function: func(ctx pm.Context, _ public.TokenStream) pm.Output {
				return runtimeRootOutput(t, ctx, test.query, "::Value()")
			}})
			options := lower.PackageOptions{Config: testConfig(t), ProcLibraries: []*analysis.ProcLibrary{library}, RuntimeRoots: test.roots}
			output, err := lower.CompilePackage(sources, "consumer", imports, aliases, options)
			if test.want != "" {
				if err == nil || output != nil || !strings.Contains(err.Error(), test.want) || !strings.Contains(err.Error(), "main.gox:5:") {
					t.Fatalf("missing capability: output=%v error=%v", output, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			again, err := lower.CompilePackage(sources, "consumer", imports, aliases, options)
			if err != nil || !reflect.DeepEqual(output, again) || !bytes.Contains(output["main.gox"], []byte("println(Value())")) {
				t.Fatalf("canonical root output is unstable: %v\n%s", err, output["main.gox"])
			}
			if test.name == "self" {
				shadowed := map[string][]byte{"main.gox": bytes.Replace(sources["main.gox"], []byte("println(macros::identity!())"), []byte("Value := 7; println(macros::identity!(), Value)"), 1)}
				generated, err := lower.CompilePackage(shadowed, "consumer", imports, aliases, options)
				if err != nil {
					t.Fatal(err)
				}
				directory := t.TempDir()
				writeDiagnosticModuleFile(t, directory, "go.mod", []byte("module consumer\ngo 1.27\n"))
				writeDiagnosticModuleFile(t, directory, "main.go", generated["main.gox"])
				if got := runGo(t, directory, "run", "."); got != "42 7\n" {
					t.Fatalf("canonical self root was captured by the caller's local binding: %q", got)
				}
			}
		})
	}
	for _, name := range []string{"private", "Value", "crate"} {
		t.Run("context does not grant "+name, func(t *testing.T) {
			library.Run = procFixtureHost(pm.Entry{Name: "identity", Function: func(ctx pm.Context, _ public.TokenStream) pm.Output {
				root, err := ctx.RuntimeRoot("consumer")
				if err != nil {
					t.Fatal(err)
				}
				identifier, err := public.NewIdent(name, false, root.Tokens()[0].Context())
				if err != nil {
					t.Fatal(err)
				}
				return pm.Success(public.New(identifier.WithSpan(ctx.DefSpan())))
			}})
			output, err := lower.CompilePackage(sources, "consumer", imports, aliases, lower.PackageOptions{Config: testConfig(t), ProcLibraries: []*analysis.ProcLibrary{library}, RuntimeRoots: []analysis.RuntimeRoot{root}})
			if err == nil || output != nil || !strings.Contains(err.Error(), "unresolved name") {
				t.Fatalf("root context exposed an unrelated binding: %v\n%s", err, output["main.gox"])
			}
		})
	}
}

func TestRuntimeRootReexportsAndOutputOwnership(t *testing.T) {
	_, library, _ := compileProcFixture(t, procFixtureSource)
	const consumerPath, facadePath, runtimePath = "example.org/consumer", "example.org/facade", "example.org/runtime"
	ordinary := func(module string) analysis.RuntimeRoot {
		return analysis.RuntimeRoot{LogicalModule: module, BindingID: module + "-selected-content", Namespace: paths.Namespace{PackagePath: module, ModulePath: module}}
	}
	for _, test := range []struct {
		name, reexport, suffix, want string
	}{
		{"full root", "pub use dependency as runtime;", "::Value()", ""},
		{"macro member", "pub use dependency as runtime;", "::answer!()", ""},
		{"member only", "pub use dependency::Value as forwarded;", "::Value()", "not available"},
		{"private root", "use dependency as runtime;", "::Value()", "not available"},
		{"glob members", "pub use dependency::*;", "::Value()", "not available"},
		{"submodule only", "pub use dependency::leaf as runtime;", "::Value()", "not available"},
		{"private member", "pub use dependency as runtime;", "::secret()", "unresolved name"},
		{"private macro", "pub use dependency as runtime;", "::hidden!()", "private"},
		{"internal package", "pub use dependency as runtime;", "::internal::hidden::Value()", "internal package"},
	} {
		t.Run(test.name, func(t *testing.T) {
			library.Run = procFixtureHost(pm.Entry{Name: "identity", Function: func(ctx pm.Context, input public.TokenStream) pm.Output {
				if !ctx.HasFeature("consumer") || ctx.HasFeature("facade") {
					t.Fatalf("nested invocation took configuration from its definition: %v", ctx.Features())
				}
				if strings.TrimSpace(input.Display()) == "nested" {
					return pm.FromParse(public.ParseText("identity!(finish)", ctx.DefSite()))
				}
				return runtimeRootOutput(t, ctx, runtimePath, test.suffix)
			}})
			generate := func() (string, map[string][]byte, error) {
				t.Helper()
				base := t.TempDir()
				consumer, facade, runtime := filepath.Join(base, "consumer"), filepath.Join(base, "facade"), filepath.Join(base, "runtime")
				writeDiagnosticModuleFile(t, consumer, "go.mod", []byte("module "+consumerPath+"\ngo 1.27\nrequire (\n"+facadePath+" v0.0.0\n"+runtimePath+" v0.0.0\n)\nreplace "+facadePath+" => ../facade\nreplace "+runtimePath+" => ../runtime\n"))
				writeDiagnosticModuleFile(t, consumer, "main.gox", []byte("package main\nuse facade;\nfunc main() { println(facade::forward!(nested)) }\n"))
				writeDiagnosticModuleFile(t, facade, "go.mod", []byte("module "+facadePath+"\ngo 1.27\nrequire "+runtimePath+" v0.0.0\nreplace "+runtimePath+" => ../runtime\n"))
				writeDiagnosticModuleFile(t, facade, "facade.gox", []byte("package facade\nuse macros;\npub use macros::identity as forward;\n"+test.reexport+"\n"))
				writeDiagnosticModuleFile(t, runtime, "go.mod", []byte("module "+runtimePath+"\ngo 1.27\n"))
				writeDiagnosticModuleFile(t, runtime, "runtime.gox", []byte("package runtime\npub mod leaf;\npub mod internal;\npub func Value() int { return 42 }\nfunc secret() int { return 99 }\npub macro_rules! answer { () => { Value() }; }\nmacro_rules! hidden { () => { 99 }; }\n"))
				writeDiagnosticModuleFile(t, runtime, "leaf/value.gox", []byte("package leaf\npub func Value() int { return 7 }\n"))
				writeDiagnosticModuleFile(t, runtime, "internal/module.gox", []byte("package internal\npub mod hidden;\n"))
				writeDiagnosticModuleFile(t, runtime, "internal/hidden/value.gox", []byte("package hidden\npub func Value() int { return 13 }\n"))
				consumerConfig, facadeConfig := testConfig(t), testConfig(t)
				consumerConfig.Features.Enabled = map[string]bool{"consumer": true}
				facadeConfig.Features.Enabled = map[string]bool{"facade": true}
				err := lower.CompileModules([]analysis.Module{
					{Path: consumerPath, Dir: consumer, Roots: paths.Roots{"facade": ordinary(facadePath).Namespace}, Config: consumerConfig, ProcLibraries: []*analysis.ProcLibrary{library}, RuntimeRoots: []analysis.RuntimeRoot{ordinary(facadePath)}},
					{Path: facadePath, Dir: facade, RootPackage: facadePath, Roots: paths.Roots{"dependency": ordinary(runtimePath).Namespace, "macros": {PackagePath: procFixturePackage, ModulePath: procFixturePackage}}, Config: facadeConfig, RuntimeRoots: []analysis.RuntimeRoot{ordinary(facadePath), ordinary(runtimePath)}},
					{Path: runtimePath, Dir: runtime, RootPackage: runtimePath, Config: testConfig(t), RuntimeRoots: []analysis.RuntimeRoot{ordinary(runtimePath)}},
				})
				if err != nil {
					return consumer, nil, err
				}
				files := map[string][]byte{}
				err = filepath.WalkDir(base, func(name string, entry os.DirEntry, err error) error {
					if err != nil || entry.IsDir() || filepath.Ext(name) != ".go" {
						return err
					}
					relative, err := filepath.Rel(base, name)
					if err != nil {
						return err
					}
					files[relative], err = os.ReadFile(name)
					return err
				})
				return consumer, files, err
			}
			consumer, generated, err := generate()
			if test.want != "" {
				if err == nil || !strings.Contains(err.Error(), test.want) {
					t.Fatalf("expected %q, got %v", test.want, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			_, repeated, err := generate()
			if err != nil || !reflect.DeepEqual(generated, repeated) {
				t.Fatalf("RuntimeRoot output changed across identical plans: %v", err)
			}
			if bytes.Contains(generated["consumer/main.go"], []byte(procFixturePackage)) || !bytes.Contains(generated["consumer/main.go"], []byte(`"`+runtimePath+`"`)) {
				t.Fatalf("macro identity or runtime import is wrong:\n%s", generated["consumer/main.go"])
			}
			runGo(t, consumer, "build", "-o", filepath.Join(t.TempDir(), "program"), ".")
			if got := runGo(t, consumer, "run", "."); got != "42\n" {
				t.Fatalf("generated runtime call = %q", got)
			}
			writeDiagnosticModuleFile(t, consumer, "main_test.go", []byte("package main\nimport \"testing\"\nfunc TestGeneratedProgram(t *testing.T) { main() }\n"))
			runGo(t, consumer, "test", "./...")
		})
	}
}

func TestRuntimeRootGoNamespaceWithoutRootPackage(t *testing.T) {
	_, library, _ := compileProcFixture(t, procFixtureSource)
	const consumerPath, facadePath, runtimePath = "example.org/consumer", "example.org/facade", "example.org/native"
	library.Run = procFixtureHost(pm.Entry{Name: "identity", Function: func(ctx pm.Context, _ public.TokenStream) pm.Output {
		return runtimeRootOutput(t, ctx, runtimePath, "::leaf::Value()")
	}})
	root := analysis.RuntimeRoot{LogicalModule: runtimePath, BindingID: "native-selected-content", Namespace: paths.Namespace{PackagePath: runtimePath, GoPrefix: runtimePath, ModulePath: runtimePath}}
	facadeRoot := analysis.RuntimeRoot{LogicalModule: facadePath, BindingID: "facade-selected-content", Namespace: paths.Namespace{PackagePath: facadePath, ModulePath: facadePath}}
	base := t.TempDir()
	consumer, facade, runtime := filepath.Join(base, "consumer"), filepath.Join(base, "facade"), filepath.Join(base, "runtime")
	writeDiagnosticModuleFile(t, consumer, "go.mod", []byte("module "+consumerPath+"\ngo 1.27\nrequire (\n"+facadePath+" v0.0.0\n"+runtimePath+" v0.0.0\n)\nreplace "+facadePath+" => ../facade\nreplace "+runtimePath+" => ../runtime\n"))
	writeDiagnosticModuleFile(t, consumer, "main.gox", []byte("package main\nuse macros;\nfunc main() { println(macros::identity!()) }\n"))
	writeDiagnosticModuleFile(t, facade, "go.mod", []byte("module "+facadePath+"\ngo 1.27\nrequire "+runtimePath+" v0.0.0\nreplace "+runtimePath+" => ../runtime\n"))
	writeDiagnosticModuleFile(t, facade, "facade.gox", []byte("package facade\npub use dependency as runtime;\n"))
	writeDiagnosticModuleFile(t, runtime, "go.mod", []byte("module "+runtimePath+"\ngo 1.27\n"))
	writeDiagnosticModuleFile(t, runtime, "leaf/value.go", []byte("package leaf\nfunc Value() int { return 42 }\n"))
	err := lower.CompileModules([]analysis.Module{
		{Path: consumerPath, Dir: consumer, Config: testConfig(t), Roots: paths.Roots{"facade": facadeRoot.Namespace, "macros": {PackagePath: procFixturePackage, ModulePath: procFixturePackage}}, ProcLibraries: []*analysis.ProcLibrary{library}, RuntimeRoots: []analysis.RuntimeRoot{facadeRoot}},
		{Path: facadePath, Dir: facade, RootPackage: facadePath, Config: testConfig(t), Roots: paths.Roots{"dependency": root.Namespace}, RuntimeRoots: []analysis.RuntimeRoot{facadeRoot, root}},
	})
	if err != nil {
		t.Fatal(err)
	}
	generated, err := os.ReadFile(filepath.Join(consumer, "main.go"))
	if err != nil || !bytes.Contains(generated, []byte(`"example.org/native/leaf"`)) {
		t.Fatalf("child-only Go namespace lost its canonical root: %v\n%s", err, generated)
	}
	runGo(t, consumer, "build", "-o", filepath.Join(t.TempDir(), "program"), ".")
	if got := runGo(t, consumer, "run", "."); got != "42\n" {
		t.Fatalf("generated child-package runtime call = %q", got)
	}
}

func TestRuntimeRootStaticMembersAndVariants(t *testing.T) {
	_, library, _ := compileProcFixture(t, procFixtureSource)
	const runtimePath = "example.org/item-runtime"
	root := analysis.RuntimeRoot{LogicalModule: runtimePath, BindingID: "selected-runtime", Namespace: paths.Namespace{PackagePath: runtimePath, ModulePath: runtimePath}}
	for _, test := range []struct{ name, suffix, expression, want string }{
		{"static method", "::Factory::Value()", "macros::identity!()", ""},
		{"enum variant", "::Choice::Item(42)", "runtime::Read(macros::identity!())", ""},
		{"private static method", "::Factory::secret()", "macros::identity!()", "private inherent member is not visible"},
	} {
		t.Run(test.name, func(t *testing.T) {
			library.Run = procFixtureHost(pm.Entry{Name: "identity", Function: func(ctx pm.Context, _ public.TokenStream) pm.Output {
				return runtimeRootOutput(t, ctx, runtimePath, test.suffix)
			}})
			base := t.TempDir()
			consumer, runtime := filepath.Join(base, "consumer"), filepath.Join(base, "runtime")
			writeDiagnosticModuleFile(t, consumer, "go.mod", []byte("module consumer\ngo 1.27\nrequire "+runtimePath+" v0.0.0\nreplace "+runtimePath+" => ../runtime\n"))
			writeDiagnosticModuleFile(t, consumer, "main.gox", []byte("package main\nuse macros; use runtime;\nfunc main() { println("+test.expression+") }\n"))
			writeDiagnosticModuleFile(t, runtime, "go.mod", []byte("module "+runtimePath+"\ngo 1.27\n"))
			writeDiagnosticModuleFile(t, runtime, "lib.gox", []byte(`package runtime
pub type Factory struct {}
impl Factory {
 pub func Value() int { return 42 }
 func secret() int { return 99 }
}
pub enum Choice { Item(int) }
pub func Read(value Choice) int { return match value { Choice::Item(n) => n } }
`))
			err := lower.CompileModules([]analysis.Module{
				{Path: "consumer", Dir: consumer, Config: testConfig(t), Roots: paths.Roots{"runtime": root.Namespace, "macros": {PackagePath: procFixturePackage, ModulePath: procFixturePackage}}, ProcLibraries: []*analysis.ProcLibrary{library}, RuntimeRoots: []analysis.RuntimeRoot{root}},
				{Path: runtimePath, Dir: runtime, RootPackage: runtimePath, Config: testConfig(t)},
			})
			if test.want != "" {
				if err == nil || !strings.Contains(err.Error(), test.want) {
					t.Fatalf("expected %q, got %v", test.want, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			runGo(t, consumer, "build", "./...")
			if got := runGo(t, consumer, "run", "."); got != "42\n" {
				t.Fatalf("generated item path output = %q", got)
			}
		})
	}
}

func TestRuntimeRootCanonicalProceduralDerive(t *testing.T) {
	base := t.TempDir()
	consumer, producer := filepath.Join(base, "consumer"), filepath.Join(base, "macros")
	manifest := testdeps.FormalManifest(t, "derivedroot", "app", "[dependencies.macros]\npath=\"../macros\"\n")
	manifest = strings.Replace(manifest, "[lib]", "[[bin]]", 1)
	manifest = strings.Replace(manifest, "[dependencies.serde]", "[dependencies.wire]", 1)
	writeDiagnosticModuleFile(t, consumer, "goxide.toml", []byte(manifest))
	sdkVersion := strings.TrimSpace(runGo(t, ".", "list", "-m", "-f", "{{.Version}}", "github.com/goxide-lang/syntax"))
	if sdkVersion == "" {
		t.Fatal("compiler SDK has no public version pin")
	}
	writeDiagnosticModuleFile(t, producer, "goxide.toml", []byte(`[go_mod]
module="example.org/runtime-root-macros"
go="1.27"
[lib]
name="macros"
path="."
proc_macro=true
[dependencies.syntax]
go="github.com/goxide-lang/syntax"
version="`+sdkVersion+`"
`))
	writeDiagnosticModuleFile(t, producer, "lib.gox", []byte(`package macros
use syntax::proc_macro as pm;use syntax::token;
#[proc_macro]
pub func identity(ctx pm::Context,input token::TokenStream)pm::Output{return expandIdentity(ctx,input)}
`))
	writeDiagnosticModuleFile(t, producer, "expand.go", []byte(`package macros
import (
 pm "github.com/goxide-lang/syntax/proc_macro"
 "github.com/goxide-lang/syntax/token"
)
func expandIdentity(ctx pm.Context,_ token.TokenStream)pm.Output{
 root,err:=ctx.RuntimeRoot("github.com/goxide-lang/serde")
 if err!=nil{return pm.Failure(err.Error(),ctx.CallSpan())}
 parse:=func(text string)[]token.Token{
  stream,err:=token.ParseText(text,ctx.DefSite());if err!=nil{panic(err)}
  values:=stream.Tokens()
  for len(values)>0&&values[len(values)-1].Kind()==token.SemicolonKind&&values[len(values)-1].Implicit(){values=values[:len(values)-1]}
  return values
 }
 arguments,err:=token.NewGroup(token.Parenthesis,token.New(append(root.Tokens(),parse("::derive::Serialize")...)...),ctx.DefSite())
 if err!=nil{return pm.Failure(err.Error(),ctx.CallSpan())}
 attribute,err:=token.NewGroup(token.Bracket,token.New(append(parse("derive"),arguments)...),ctx.DefSite())
 if err!=nil{return pm.Failure(err.Error(),ctx.CallSpan())}
 output:=append(parse("#"),attribute)
 output=append(output,parse("type Generated struct{Value int}")...)
 return pm.Success(token.New(output...))
}
`))
	writeDiagnosticModuleFile(t, consumer, "main.gox", []byte(`package main
use macros;use wire;use wire::json;use fmt;
macros::identity!()
func main(){value:=Generated{Value:42};data,err:=json::Marshal(&value,wire::Write[Generated]);if err!=nil{panic(err)};fmt::Println(string(data))}
`))
	first, calls, err := testdeps.PrepareFormalBinary(t, consumer)
	if err != nil {
		t.Fatal(err)
	}
	if calls < 2 {
		t.Fatalf("runtime root did not invoke both procedural stages: %d", calls)
	}
	generated, err := os.ReadFile(filepath.Join(first.Root, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(generated, []byte("Serialize")) {
		t.Fatalf("canonical procedural derive disappeared:\n%s", generated)
	}
	second, _, err := testdeps.PrepareFormalBinary(t, consumer, compile.Options{Locked: true})
	if err != nil {
		t.Fatal(err)
	}
	again, err := os.ReadFile(filepath.Join(second.Root, "main.go"))
	if err != nil || !bytes.Equal(generated, again) {
		t.Fatalf("runtime root generation changed: %v", err)
	}
	testdeps.RunFormalGo(t, first, "build", "-mod=readonly", "./...")
	if got := testdeps.RunFormalGo(t, first, "run", "-mod=readonly", "."); got != "{\"Value\":42}\n" {
		t.Fatalf("canonical runtime root output = %q", got)
	}
}
