package acceptance_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/frontend"
	"github.com/hxsf/goxide/internal/paths"
	"github.com/hxsf/goxide/internal/rootconfig"
)

// These fixtures use the CLI and independently build its output. In particular,
// the plain Go consumer has neither Goxide source nor a compiler dependency.
func TestStaticProtocolModules(t *testing.T) {
	h := newStaticProtocolHarness(t)
	contracts := h.transpile(t, "contracts", map[string]string{
		"build.gox": "package contracts\npub trait Build { func Abc() Self }\n",
	}, nil)
	deps := map[string]string{"protocols.example/contracts": contracts}
	model := h.transpile(t, "model", map[string]string{
		"user.gox": `package model
use contracts;
type User struct { Name string }
var Calls int
impl contracts::Build for User {
    func Abc() Self { Calls++; return Self{Name: "Ada"} }
}
`,
		"state.gox": `package model
use contracts;
pub enum State { Empty, Ready(int) }
impl contracts::Build for State {
    func Abc() Self { return Self::Ready(37) }
}
func Number(v State) int { return match v { State::Empty => 0, State::Ready(n) => n } }
`,
		"generic.gox": `package model
use contracts;
func Make[T contracts::Build]() T { return T::Abc() }
`,
		"native.go": `package model
func (u User) Len() int { return len(u.Name) }
`,
		"other/other.gox": `package other
use contracts;
type User struct { Number int }
impl contracts::Build for User { func Abc() Self { return Self{Number: 73} } }
`,
		"facade/facade.gox": `package facade
use library as origin;
use contracts;
pub use origin::User as Renamed;
pub use origin::Make as BuildOne;
pub use contracts::Build as Factory;
`,
	}, deps)
	deps["protocols.example/model"] = model
	appSources := map[string]string{
		"app.gox": `package app
use library as m;
use contracts;
use library::facade as f;
type Alias = m::User
func Concrete() m::User { return m::User::Abc() }
func ViaAlias() Alias { return Alias::Abc() }
func Generic[T contracts::Build]() T { return T::Abc() }
func ViaFactory() m::User { return m::Make[m::User]() }
func ViaFacade() f::Renamed { return f::BuildOne[f::Renamed]() }
func FacadeGeneric[T f::Factory]() T { return T::Abc() }
func EnumValue() int { return m::Number(m::State::Abc()) }
func OrdinaryMethod() int { return m::User::Len(Concrete()) }
`,
		"other.gox": `package app
use library::other as m;
func OtherFile() int { return m::User::Abc().Number }
`,
	}
	appSource := h.source(t, "app", appSources, deps)
	app := filepath.Join(t.TempDir(), "app")
	packagesCommand(t, h.root, h.cli, true, "transpile", "-o", app, appSource)
	repeated := filepath.Join(t.TempDir(), "repeat")
	packagesCommand(t, h.root, h.cli, true, "transpile", "-o", repeated, appSource)
	staticProtocolEqualTrees(t, app, repeated)
	// Recompiling generated ordinary Go must retain the protocol and use
	// artifacts, including the source arity of public generic functions. It
	// already carries its imports/configuration; do not reload source roots.
	reloaded := filepath.Join(t.TempDir(), "reloaded")
	if _, err := frontend.CompileModuleWithOptions(model, reloaded, frontend.ModuleOptions{Roots: paths.Roots{}}); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{".goxide.protocols.json", "facade/.goxide.uses.json"} {
		if _, err := os.Stat(filepath.Join(reloaded, relative)); err != nil {
			t.Fatalf("generated module reload lost %s: %v", relative, err)
		}
	}
	deps["protocols.example/model"] = reloaded
	appReloaded := h.transpile(t, "app", appSources, deps)
	deps["protocols.example/app"] = appReloaded
	t.Run("independent_modules", func(t *testing.T) {
		plain := h.source(t, "plain", map[string]string{
			"consumer_test.go": `package plain
import (
 "reflect"
 "testing"
 "github.com/goxide-lang/std/typeinfo"
 a "protocols.example/app"
 m "protocols.example/model"
 f "protocols.example/model/facade"
)
func Native[T typeinfo.CarrierFor[T,S], S interface{~struct{}; Abc() T}]() T {
 return typeinfo.Of[T]().Abc()
}
func TestSourceAndNativeCalls(t *testing.T) {
 m.Calls=0
 values := []m.User{
  a.Concrete(), a.ViaAlias(), a.Generic[m.User](), a.ViaFactory(), a.ViaFacade(),
  a.FacadeGeneric[f.Renamed](), m.Make[m.User](), m.Make[f.Renamed](),
  typeinfo.Of[m.User]().Abc(), Native[m.User](),
 }
 for i,v := range values {
  if v.Name != "Ada" || reflect.TypeOf(v) != reflect.TypeOf(m.User{}) { t.Fatalf("call %d: %#v",i,v) }
 }
 if m.Calls!=len(values) { t.Fatalf("implementation called %d times for %d calls",m.Calls,len(values)) }
 if a.EnumValue()!=37 || m.Number(a.Generic[m.State]())!=37 || m.Number(Native[m.State]())!=37 {
  t.Fatal("enum associated implementation")
 }
 if a.OtherFile()!=73 || a.OrdinaryMethod()!=3 { t.Fatal("file owner or ordinary method resolution") }
 var ordinary interface{Len() int} = a.Concrete()
 if ordinary.Len()!=3 { t.Fatal("ordinary Go interface interoperability") }
}
// Ordinary Go callers may supply their own conforming carrier. This does not
// register a source-language impl, and Of must never execute the anchor.
type Handwritten struct{ Value int }
type HandwrittenOps struct{}
func (Handwritten) GoxideInternalTypeCarrierForSelf(Handwritten) HandwrittenOps { panic("anchor executed") }
func (HandwrittenOps) Abc() Handwritten { return Handwritten{91} }
func TestOrdinaryLibraryCarrier(t *testing.T) {
 if Native[Handwritten]().Value!=91 { t.Fatal("ordinary generic library") }
}
`,
		}, deps)
		for _, dir := range []string{contracts, model, app, reloaded, appReloaded, plain} {
			packagesCommand(t, dir, h.goTool, true, "test", "-race", "-count=1", "-v", "./...")
			packagesCommand(t, dir, h.goTool, true, "vet", "./...")
		}
		graph := packagesCommand(t, plain, h.goTool, true, "list", "-buildvcs=false", "-deps", "-test", "./...")
		if strings.Contains(graph, "github.com/hxsf/goxide") || strings.Contains(graph, "github.com/goxide-lang/goxide") {
			t.Fatalf("generated modules depend on compiler: %s", graph)
		}
	})

	// Mutate actual on-disk dependencies, rather than feeding invented importer
	// records to the compiler. Each subtest restores the known-good producer.
	artifact := filepath.Join(reloaded, ".goxide.protocols.json")
	saved, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []string{"missing", "malformed", "stale_source"} {
		t.Run(mutation, func(t *testing.T) {
			switch mutation {
			case "missing":
				if err := os.Remove(artifact); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { packagesWrite(t, reloaded, ".goxide.protocols.json", string(saved)) })
			case "malformed":
				packagesWrite(t, reloaded, ".goxide.protocols.json", string(saved)+"\n{}\n")
				t.Cleanup(func() { packagesWrite(t, reloaded, ".goxide.protocols.json", string(saved)) })
			case "stale_source":
				name := filepath.Join(reloaded, "user.go")
				data, err := os.ReadFile(name)
				if err != nil {
					t.Fatal(err)
				}
				packagesWrite(t, reloaded, "user.go", string(data)+"\n// changed after protocol export\n")
				t.Cleanup(func() { packagesWrite(t, reloaded, "user.go", string(data)) })
			}
			source := h.source(t, "broken", map[string]string{"bad.gox": "package broken\nuse library as m;\nfunc Value() m::User { return m::User::Abc() }\n"}, deps)
			out := filepath.Join(t.TempDir(), "failed")
			log := packagesCommand(t, h.root, h.cli, false, "transpile", "-o", out, source)
			if !strings.Contains(log, "bad.gox:3:17:") || !strings.Contains(log, "source span [49,53)") || !strings.Contains(log, "metadata") {
				t.Fatalf("lost original source or artifact diagnostic:\n%s", log)
			}
			staticProtocolNoOutput(t, out)
		})
	}
}

func TestStaticProtocolDiagnostics(t *testing.T) {
	h := newStaticProtocolHarness(t)
	const prelude = "package bad\ntrait Build { func Abc() Self }\ntype U struct { N int }\n"
	for _, tc := range []struct {
		name, source, peer, want string
	}{
		{"missing_impl", prelude + "func Value() U { return U::Abc() }\n", "", "Abc"},
		{"missing_method", prelude + "impl Build for U {}\n", "", "Abc"},
		{"wrong_result", prelude + "type Other struct { N int }\nimpl Build for U { func Abc() Other { return Other{} } }\n", "", "Abc"},
		{"wrong_parameter", prelude + "impl Build for U { func Abc(n int) Self { return Self{N:n} } }\n", "", "Abc"},
		{"unconstrained_generic", prelude + "func Value[T any]() T { return T::Abc() }\n", "", "Abc"},
		{"distinct_defined_type", prelude + "impl Build for U { func Abc() Self { return Self{N:1} } }\ntype V U\nfunc Value() V { return V::Abc() }\n", "", "Abc"},
		{"alias_impl_target", prelude + "type Alias = U\nimpl Build for Alias { func Abc() Self { return Self{N:1} } }\n", "", "Alias"},
		{"reserved_source_anchor", prelude + "func (U) GoxideInternalTypeCarrierForSelf(U) struct{} { return struct{}{} }\nimpl Build for U { func Abc() Self { return Self{} } }\n", "", "GoxideInternalTypeCarrierForSelf"},
		{"reserved_peer_anchor", prelude + "impl Build for U { func Abc() Self { return Self{} } }\n", "package bad\nfunc (U) GoxideInternalTypeCarrierForSelf(U) struct{} { return struct{}{} }\n", "generated protocol anchor"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{"bad.gox": tc.source}
			if tc.peer != "" {
				files["peer.go"] = tc.peer
			}
			source := h.source(t, "bad", files, nil)
			out := filepath.Join(t.TempDir(), "failed")
			log := packagesCommand(t, h.root, h.cli, false, "transpile", "-o", out, source)
			file := "bad.gox:"
			if tc.peer != "" {
				file = "peer.go:"
			}
			if !strings.Contains(log, file) || !strings.Contains(log, tc.want) {
				t.Fatalf("expected source diagnostic for %s:\n%s", tc.want, log)
			}
			if tc.peer != "" && (!strings.Contains(log, "peer.go:2:10:") || !strings.Contains(log, "source span [21,53)")) {
				t.Fatalf("anchor collision did not identify the original native method name:\n%s", log)
			}
			if tc.name == "unconstrained_generic" && (!strings.Contains(log, "bad.gox:4:35:") || !strings.Contains(log, "source span [102,105)")) {
				t.Fatalf("missing constraint did not identify the original associated member:\n%s", log)
			}
			staticProtocolNoOutput(t, out)
		})
	}
}

func TestStaticProtocolReplacedDependencies(t *testing.T) {
	h := newStaticProtocolHarness(t)
	// Both choices have the same module/package/name. Freshly produce each
	// version so this checks the selected signature, not only stale hashes.
	contracts := h.transpile(t, "contracts", map[string]string{
		"build.gox": "package contracts\npub trait Build { func Abc() Self }\n",
	}, nil)
	changed := h.transpile(t, "contracts", map[string]string{
		"build.gox": "package contracts\npub trait Build { func Abc(n int) Self }\n",
	}, nil)
	implementation := map[string]string{"user.gox": `package model
use contracts;
type User struct { N int }
impl contracts::Build for User { func Abc() Self { return Self{N:37} } }
func Value() User { return User::Abc() }
`}
	for _, tc := range []struct {
		name, dependency string
		success          bool
	}{
		{"original_shape", contracts, true}, {"replaced_shape", changed, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := h.source(t, "model", implementation, map[string]string{"protocols.example/contracts": tc.dependency})
			out := filepath.Join(t.TempDir(), "model")
			log := packagesCommand(t, h.root, h.cli, tc.success, "transpile", "-o", out, source)
			if tc.success {
				packagesCommand(t, out, h.goTool, true, "test", "-race", "-count=1", "./...")
			} else {
				if !strings.Contains(log, "user.gox:") || !strings.Contains(log, "Abc") {
					t.Fatal(log)
				}
				staticProtocolNoOutput(t, out)
			}
		})
	}

	t.Run("wrong_typeinfo_shape", func(t *testing.T) {
		wrong := t.TempDir()
		selected := strings.TrimSpace(packagesCommand(t, h.root, h.goTool, true, "list", "-m", "-f", "{{.Dir}}", "github.com/goxide-lang/std"))
		// Keep every other package from the actual selected std module. A
		// missing sum/runtime package must not make this negative pass.
		if err := os.CopyFS(wrong, os.DirFS(selected)); err != nil {
			t.Fatal(err)
		}
		packagesWrite(t, wrong, "typeinfo/typeinfo.go", `package typeinfo
const GoxideTypeInfoABI = 1
type CarrierFor[T any,S ~struct{}] interface { GoxideInternalTypeCarrierForSelf(T) S }
// Same owner, marker and name; the actual function has the wrong result.
func Of[T CarrierFor[T,S],S ~struct{}]() int { return 0 }
`)
		source := h.source(t, "bad", map[string]string{"bad.gox": `package bad
trait Build { func Abc() Self }
type User struct { N int }
impl Build for User { func Abc() Self { return Self{N:37} } }
func Value() User { return User::Abc() }
`}, nil)
		mod, err := os.OpenFile(filepath.Join(source, "go.mod"), os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		_, writeErr := fmt.Fprintf(mod, "\nreplace github.com/goxide-lang/std => %s\n", filepath.ToSlash(wrong))
		closeErr := mod.Close()
		if writeErr != nil {
			t.Fatal(writeErr)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
		out := filepath.Join(t.TempDir(), "bad")
		log := packagesCommand(t, h.root, h.cli, false, "transpile", "-o", out, source)
		if !strings.Contains(log, "bad.gox:") || (!strings.Contains(log, "typeinfo") && !strings.Contains(log, "Of")) {
			t.Fatal(log)
		}
		staticProtocolNoOutput(t, out)
	})
}

// Item imports and pub-use routes must retain the same source arity and
// explicit-implementation requirements as a directly qualified call.
func TestStaticProtocolFunctionAliases(t *testing.T) {
	h := newStaticProtocolHarness(t)
	provider := h.transpile(t, "model", map[string]string{
		"model.gox": `package model
pub trait Build { func Abc() Self }
type U struct { N int }
impl Build for U { func Abc() Self { return Self{N:29} } }
func F[T Build]() T { return T::Abc() }
`,
		"facade/facade.gox": "package facade\nuse library as p;\npub use p::F as ExportedF;\n",
	}, nil)
	// Read the public ABI actually emitted by this producer. The regression
	// must not guess hashed helper names or add compatibility helper aliases.
	var artifact struct {
		Metadata struct {
			Definitions     []struct{ Name, MarkerName string }
			Implementations []struct {
				Target      struct{ Name string }
				CarrierName string
			}
		}
	}
	data, err := os.ReadFile(filepath.Join(provider, ".goxide.protocols.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &artifact); err != nil {
		t.Fatal(err)
	}
	marker, carrier := "", ""
	for _, d := range artifact.Metadata.Definitions {
		if d.Name == "Build" {
			marker = d.MarkerName
		}
	}
	for _, i := range artifact.Metadata.Implementations {
		if i.Target.Name == "U" {
			carrier = i.CarrierName
		}
	}
	if marker == "" || carrier == "" {
		t.Fatal("producer did not export Build/U evidence")
	}
	native := h.source(t, "native", map[string]string{
		"native.go": fmt.Sprintf(`package native
type U struct { N int }
type Ops struct{}
func (U) GoxideInternalTypeCarrierForSelf(U) Ops { panic("anchor executed") }
func (Ops) Abc() U { return U{N:77} }
func (Ops) %s() {}
`, marker),
	}, nil)
	deps := map[string]string{"protocols.example/model": provider, "protocols.example/native": native}
	roots := map[string]rootconfig.Mapping{
		"library": {Module: "protocols.example/model", Package: "protocols.example/model", Namespaces: map[string]string{"facade": "protocols.example/model/facade"}},
		"native":  {Module: "protocols.example/native", Package: "protocols.example/native"},
	}
	const prelude = "package app\nuse library as p;\nuse library::facade as f;\nuse native;\n"
	routes := []struct{ name, binding, callee, token string }{
		{"direct", "", "p::F", "F"},
		{"item_alias", "use p::F as AliasF;\n", "AliasF", "AliasF"},
		{"public_route", "", "f::ExportedF", "ExportedF"},
		{"public_item_alias", "use f::ExportedF as AliasF;\n", "AliasF", "AliasF"},
	}
	t.Run("only_source_parameters", func(t *testing.T) {
		files := map[string]string{}
		for i, route := range routes {
			files[fmt.Sprintf("route%d.gox", i)] = prelude + route.binding + fmt.Sprintf("func Route%d() int { return %s[p::U]().N }\n", i, route.callee)
		}
		source := h.source(t, "app", files, deps)
		writeAcceptanceRoots(t, source, roots)
		out := filepath.Join(t.TempDir(), "app")
		packagesCommand(t, h.root, h.cli, true, "transpile", "-o", out, source)
		packagesWrite(t, out, "route_test.go", `package app
import("testing";p "protocols.example/model";n "protocols.example/native")
func TestAliasesPreserveTheImplementation(t *testing.T) {
 for i,v := range []int{Route0(),Route1(),Route2(),Route3()} { if v!=29 {t.Fatalf("route %d: %d",i,v)} }
}
func TestNativeStructuralControl(t *testing.T) {
 // All Go constraints, including the actual protocol marker, are met. The
 // source compiler must still reject n.U because there is no explicit impl.
 if p.F[n.U]().N!=77 {t.Fatal("native structural control")}
}
`)
		packagesCommand(t, out, h.goTool, true, "test", "-race", "-count=1", "-v", "./...")
		packagesCommand(t, out, h.goTool, true, "vet", "./...")
	})
	for _, route := range routes {
		for _, kind := range []string{"hidden_parameter", "missing_impl"} {
			t.Run(route.name+"/"+kind, func(t *testing.T) {
				args, want, token := "p::U,OpsAlias", "source type parameters", "OpsAlias"
				files := map[string]string{}
				if kind == "missing_impl" {
					args, want, token = "native::U", "no explicit implementation", route.token
				} else {
					files["native.go"] = fmt.Sprintf("package app\nimport p %q\ntype OpsAlias = p.%s\n", "protocols.example/model", carrier)
				}
				body := prelude + route.binding + fmt.Sprintf("func Wrong() int { return %s[%s]().N }\n", route.callee, args)
				files["case.gox"] = body
				source := h.source(t, "app", files, deps)
				writeAcceptanceRoots(t, source, roots)
				out := filepath.Join(t.TempDir(), "failed")
				log := packagesCommand(t, h.root, h.cli, false, "transpile", "-o", out, source)
				if !strings.Contains(log, want) {
					t.Fatalf("route %s lost %s check:\n%s", route.name, kind, log)
				}
				staticProtocolDiagnosticToken(t, log, "case.gox", body, token)
				staticProtocolNoOutput(t, out)
			})
		}
	}
}

func staticProtocolDiagnosticToken(t *testing.T, log, file, source, want string) {
	t.Helper()
	m := regexp.MustCompile(`source span \[(\d+),(\d+)\)`).FindStringSubmatch(log)
	if len(m) != 3 {
		t.Fatalf("missing original source span:\n%s", log)
	}
	start, e1 := strconv.Atoi(m[1])
	end, e2 := strconv.Atoi(m[2])
	if e1 != nil || e2 != nil || start < 0 || end > len(source) || start >= end || source[start:end] != want {
		t.Fatalf("diagnostic does not point to original %q token:\n%s", want, log)
	}
	line := strings.Count(source[:start], "\n") + 1
	column := start - strings.LastIndex(source[:start], "\n")
	if !strings.Contains(log, fmt.Sprintf("%s:%d:%d:", file, line, column)) {
		t.Fatalf("source coordinates disagree with token span:\n%s", log)
	}
}

type staticProtocolHarness struct{ root, goTool, cli string }

func newStaticProtocolHarness(t *testing.T) staticProtocolHarness {
	t.Helper()
	_, here, _, _ := runtime.Caller(0)
	h := staticProtocolHarness{root: filepath.Clean(filepath.Join(filepath.Dir(here), "../..")), goTool: filepath.Join(runtime.GOROOT(), "bin/go"), cli: filepath.Join(t.TempDir(), "goxide")}
	packagesCommand(t, h.root, h.goTool, true, "build", "-o", h.cli, "./cmd/goxide")
	return h
}

func (h staticProtocolHarness) source(t *testing.T, name string, files, deps map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	mod := "module protocols.example/" + name + "\n\ngo 1.27\n"
	var names []string
	for path := range deps {
		names = append(names, path)
	}
	sort.Strings(names)
	for _, path := range names {
		mod += fmt.Sprintf("\nrequire %s v0.0.0\nreplace %s => %s\n", path, path, filepath.ToSlash(deps[path]))
	}
	packagesWrite(t, dir, "go.mod", mod)
	writeAcceptanceRoots(t, dir, map[string]rootconfig.Mapping{
		"contracts": {Module: "protocols.example/contracts", Package: "protocols.example/contracts"},
		"library":   {Module: "protocols.example/model", Package: "protocols.example/model", Namespaces: map[string]string{"other": "protocols.example/model/other", "facade": "protocols.example/model/facade"}},
	})
	for name, data := range files {
		packagesWrite(t, dir, name, data)
	}
	return dir
}

func (h staticProtocolHarness) transpile(t *testing.T, name string, files, deps map[string]string) string {
	t.Helper()
	source := h.source(t, name, files, deps)
	out := filepath.Join(t.TempDir(), name)
	packagesCommand(t, h.root, h.cli, true, "transpile", "-o", out, source)
	return out
}

func staticProtocolNoOutput(t *testing.T, out string) {
	t.Helper()
	entries, err := os.ReadDir(out)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed protocol compile emitted artifacts: %v", entries)
	}
}

func staticProtocolEqualTrees(t *testing.T, a, b string) {
	t.Helper()
	read := func(root string) map[string]string {
		files := map[string]string{}
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			name, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			files[name] = string(data)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return files
	}
	left, right := read(a), read(b)
	if len(left) != len(right) {
		t.Fatalf("nondeterministic output count: %d vs %d", len(left), len(right))
	}
	for name, contents := range left {
		if right[name] != contents {
			t.Errorf("nondeterministic generated artifact %s", name)
		}
	}
}
