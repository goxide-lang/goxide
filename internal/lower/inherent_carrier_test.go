package lower_test

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/lower"
	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

func TestInherentStaticCarrierGoBoundary(t *testing.T) {
	const path = "carrierfixture/library/library.gox"
	sources := map[string]string{path: `package library
pub struct Point { pub X int }
impl Point {
    pub func new(x int) Self { return Self{X:x} }
    pub func origin() Self { return Self::new(Self::seed()) }
    func seed() int { return 7 }
    pub func catch(caught *bool) { *caught = recover() != nil }
    pub func shifted(self Self, dx int) Self { return Self{X:self.X+dx} }
}
pub enum Mode { Ready, Value(int) }
impl Mode {
    pub func new() Self { return Mode::Ready }
    pub func count() int { return 2 }
}
pub struct Pair[T any] { pub First T; pub Second T }
impl[T any] Pair[T] {
    pub func new(first T, second T) Self { return Self{First:first,Second:second} }
    pub func empty() Self { var zero T; return Self::new(zero,zero) }
    pub func catch(caught *bool) { *caught = recover() != nil }
}
pub func makePoint(x int) Point { return Point{X:x} }
pub func fromGoxide() int {
    make := Point::new
    pair := Pair[int]::new
    return make(3).X + pair(4,5).Second + Pair[int]::empty().First
}
`}
	generated := carrierSources(t, sources)
	file, err := parser.ParseFile(token.NewFileSet(), "library.go", generated[path], 0)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"PointOps": false, "ModeOps": false, "PairOps": false}
	for _, declaration := range file.Decls {
		switch declaration := declaration.(type) {
		case *ast.GenDecl:
			for _, spec := range declaration.Specs {
				if spec, ok := spec.(*ast.TypeSpec); ok {
					if _, tracked := want[spec.Name.Name]; tracked {
						structure, ok := spec.Type.(*ast.StructType)
						if !ok || len(structure.Fields.List) != 0 {
							t.Fatalf("carrier %s is not empty", spec.Name)
						}
						want[spec.Name.Name] = true
					}
				}
			}
		case *ast.FuncDecl:
			if declaration.Recv == nil && (strings.HasPrefix(declaration.Name.Name, "NewPoint") || strings.HasPrefix(declaration.Name.Name, "PointOrigin") || strings.HasPrefix(declaration.Name.Name, "NewPair") || strings.HasPrefix(declaration.Name.Name, "NewMode")) {
				t.Fatalf("inherent static body remains a package function: %s", declaration.Name)
			}
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("missing carrier %s:\n%s", name, generated[path])
		}
	}
	carrierGoConsumer(t, generated, `package main
import("fmt"; "carrierfixture/library")
func main() {
    point := library.PointOps{}.Origin()
    makePoint := library.PointOps{}.New
    makePair := library.PairOps[string]{}.New
    caught, genericCaught, valueCaught := false, false, false
    func(){ defer library.PointOps{}.Catch(&caught); panic("caught") }()
    func(){ defer library.PairOps[int]{}.Catch(&genericCaught); panic("caught") }()
    catch := library.PointOps{}.Catch
    func(){ defer catch(&valueCaught); panic("caught") }()
    fmt.Println(point.X, makePoint(9).Shifted(1).X, makePair("a","b").Second, library.PairOps[int]{}.Empty().First, library.ModeOps{}.Count(), library.ModeOps{}.New()!=nil, library.MakePoint(11).X, library.FromGoxide(), caught, genericCaught, valueCaught)
}
`, "7 10 b 0 2 true 11 8 true true true\n")
}

func TestInherentStaticCarrierConditionalInstances(t *testing.T) {
	const source = `package main
trait Copy { func copy(self &Self) Self }
impl Copy for int { func copy(self &Self) Self { return *self } }
struct Box[T any] { Value T }
impl[T Copy] Box[T] {
    pub func new(value T) Self { return Self{Value:Copy::copy(&value)} }
    pub func catch(caught *bool) { *caught = recover() != nil }
}
func main() {
    make := Box[int]::new
    caught, direct := false, false
    catch := Box[int]::catch
    func(){ defer catch(&caught); panic("caught") }()
    func(){ defer Box[int]::catch(&direct); panic("caught") }()
    println(make(6).Value, Box[int]::new(7).Value, caught, direct)
}
`
	const name = "macrobindingfixture/main.gox"
	generated := carrierSources(t, map[string]string{name: source})
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", generated[name], 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Recv == nil && strings.HasPrefix(function.Name.Name, "GoxideBoxInt") {
			t.Fatalf("conditional instance is a package function: %s", function.Name)
		}
	}
	runMacroBindingSource(t, source, "6 7 true true\n")
}

func TestInherentStaticCarrierNameCollision(t *testing.T) {
	_, err := lowerDeriveSources(map[string]string{"carrierfixture/main.gox": `package main
pub struct Point {}
type PointOps struct{}
impl Point { pub func new() Self { return Self{} } }
`})
	if err == nil || !strings.Contains(err.Error(), "PointOps") || !strings.Contains(err.Error(), "冲突") {
		t.Fatalf("carrier collision was not diagnosed: %v", err)
	}
}

func TestInherentStaticCarrierMemberCollision(t *testing.T) {
	_, err := lowerDeriveSources(map[string]string{"carrierfixture/main.gox": `package main
pub struct Point {}
impl Point {
    pub func value() int { return 1 }
    pub func Value() int { return 2 }
}
`})
	if err == nil || !strings.Contains(err.Error(), "Value") {
		t.Fatalf("carrier member collision was not diagnosed: %v", err)
	}
}

func TestInherentStaticCarrierPrivateAssociatedOutput(t *testing.T) {
	sources := map[string]string{
		"protocol/lib.gox": `package protocol
pub trait Source { type Item; func get(self Self) Self::Item }
`,
		"ops/lib.gox": `package ops
use associatedfixture::protocol::Source;
pub struct Box[T any] {}
impl[T Source] Box[T] {
    pub func get(value T) T::Source::Item { return Source::get(value) }
    pub func zero() T::Source::Item { var value T::Source::Item; return value }
}
pub func Read[T Source](value T) T::Source::Item { return Box[T]::get(value) }
`,
		"model/lib.gox": `package model
use associatedfixture::protocol::Source;
struct secret { N int }
pub struct Cell { pub N int }
impl Source for Cell { type Item = secret; func get(self Self) Self::Item { return secret{N:self.N} } }
pub func Extract(value secret) int { return value.N }
`,
		"main.gox": `package main
use fmt;
use associatedfixture::model;
use associatedfixture::ops;
func main() {
    get := ops::Box[model::Cell]::get
    fmt::Println(model::Extract(get(model::Cell{N:20})), model::Extract(ops::Box[model::Cell]::get(model::Cell{N:22})), model::Extract(ops::Read(model::Cell{N:42})), model::Extract(ops::Box[model::Cell]::zero()))
}
`,
	}
	runPrivateAssociatedFixture(t, sources, "20 22 42 0\n")
}

func TestInherentStaticCarrierPrivateConditionalHelpers(t *testing.T) {
	sources := map[string]string{
		"protocol/lib.gox": `package protocol
pub trait Value { func value(self Self) int }
`,
		"ops/lib.gox": `package ops
use associatedfixture::protocol::Value;
pub struct Box[T any] { pub N int }
impl[T Value] Box[T] {
    pub func new(value T) Self { return Self{N:Value::value(value)} }
    pub func catch(caught *bool) { *caught = recover() != nil }
}
pub func Read[T Value](value T) int {
    make := Box[T]::new
    return make(value).N + Box[T]::new(value).N
}
pub func Catch[T Value](value T) bool {
    caught := false
    catch := Box[T]::catch
    func(){ defer catch(&caught); panic("caught") }()
    return caught
}
`,
		"model/lib.gox": `package model
use associatedfixture::protocol::Value;
struct secret { N int }
impl Value for secret { func value(self Self) int { return self.N } }
pub func Make() secret { return secret{N:21} }
`,
		"main.gox": `package main
use fmt;
use associatedfixture::model;
use associatedfixture::ops;
func main() { fmt::Println(ops::Read(model::Make()), ops::Catch(model::Make())) }
`,
	}
	runPrivateAssociatedFixture(t, sources, "42 true\n")
}

func TestInherentStaticCarrierSharedAcrossFiles(t *testing.T) {
	sources := map[string]string{
		"carrierfixture/library/types.gox": `package library
pub struct Pair[T any] { pub Value T }
impl[T any] Pair[T] { pub func new(value T) Self { return Self{Value:value} } }
`,
		"carrierfixture/library/other.gox": `package library
impl[U any] Pair[U] { pub func empty() Self { var value U; return Self::new(value) } }
`,
	}
	generated := carrierSources(t, sources)
	count := 0
	for _, data := range generated {
		count += strings.Count(string(data), "type PairOps[")
	}
	if count != 1 {
		t.Fatalf("shared generic carrier emitted %d times", count)
	}
	carrierGoConsumer(t, generated, `package main
import("fmt"; "carrierfixture/library")
func main(){fmt.Println(library.PairOps[int]{}.New(7).Value, library.PairOps[int]{}.Empty().Value)}
`, "7 0\n")
}

func TestInherentStaticCarrierConstInstances(t *testing.T) {
	const source = `package main
struct Box[T any] { Value T }
impl[T any, const N int] Box[[N]T] {
    pub func new(value [N]T) Self { return Self{Value:value} }
}
func main() {
    make := Box[[2]int]::new
    println(make([2]int{4,5}).Value[1], Box[[3]string]::new([3]string{"a","b","c"}).Value[2])
}
`
	runMacroBindingSource(t, source, "5 c\n")
}

func TestInherentStaticCarrierSourceFrame(t *testing.T) {
	const source = `package main
use fmt;
use path::filepath;
use runtime;
pub struct Point {}
impl Point {
    pub func locate() (string, int) {
        // The business frame stays on the carrier.
        _, file, line, _ := runtime::Caller(0)
        return filepath::Base(file), line
    }
}
func main() {
    direct, directLine := Point::locate()
    locate := Point::locate
    saved, savedLine := locate()
    fmt::Println(direct, directLine, saved, savedLine)
}
`
	compile := func() []byte {
		result, err := lower.CompilePackage(map[string][]byte{"carrier.gox": []byte(source)}, "carrierfixture", nil, nil, lower.PackageOptions{LineDirectives: true})
		if err != nil {
			t.Fatal(err)
		}
		return result["carrier.gox"]
	}
	generated := compile()
	if !bytes.Equal(generated, compile()) {
		t.Fatal("source frame output is not deterministic")
	}
	if !bytes.Contains(generated, []byte("// The business frame stays on the carrier.")) {
		t.Fatal("carrier business comment was lost")
	}
	module := t.TempDir()
	if err := testdeps.WriteFile(filepath.Join(module, "go.mod"), []byte("module carrierfixture\n\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(module, "main.go"), generated, 0600); err != nil {
		t.Fatal(err)
	}
	runGo(t, module, "build", "./...")
	if got := runGo(t, module, "run", "."); got != "carrier.gox 9 carrier.gox 9\n" {
		t.Fatalf("carrier source frame = %q", got)
	}
}

func carrierSources(t *testing.T, sources map[string]string) map[string][]byte {
	t.Helper()
	first, err := lowerDeriveSources(sources)
	if err != nil {
		t.Fatal(err)
	}
	second, err := lowerDeriveSources(sources)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range first {
		if !bytes.Equal(data, second[name]) {
			t.Fatalf("carrier generation is not deterministic: %s", name)
		}
	}
	return first
}

func carrierGoConsumer(t *testing.T, generated map[string][]byte, consumer, want string) {
	t.Helper()
	module := t.TempDir()
	if err := testdeps.WriteFile(filepath.Join(module, "go.mod"), []byte("module carrierfixture\n\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for source, data := range generated {
		path := filepath.Join(module, strings.TrimSuffix(strings.TrimPrefix(source, "carrierfixture/"), ".gox")+".go")
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(module, "main.go"), []byte(consumer), 0600); err != nil {
		t.Fatal(err)
	}
	runGo(t, module, "build", "./...")
	if got := runGo(t, module, "run", "."); got != want {
		t.Fatalf("Go consumer output = %q, want %q", got, want)
	}
}
