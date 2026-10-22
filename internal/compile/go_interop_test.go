package compile

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/project"
)

func TestOrdinaryGoInteropAndExplicitTraitBodies(t *testing.T) {
	module, err := project.ReadMod(filepath.Join(runtime.GOROOT(), "bin", "go"), filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	var standard string
	for _, dependency := range module.Require {
		if dependency.Path == "github.com/goxide-lang/std" {
			standard = fmt.Sprintf("[dependencies.standard]\ngo=\"github.com/goxide-lang/std\"\nversion=%q\n", dependency.Version)
		}
	}
	if standard == "" {
		t.Fatal("compiler go.mod must pin std")
	}
	root := featureProject(t, map[string]string{
		"native/go.mod": "module example.org/interop-native\n\ngo 1.27\n",
		"native/native.go": `package native
import "io"
type Counter struct { N int; Caught any }
func (c *Counter) Read(p []byte) (int,error) {
 if c == nil { return 0, io.EOF }
 if len(p) == 0 { return 0, nil }
 c.N++
 p[0] = byte(c.N)
 return 1, nil
}
func (c *Counter) Catch() { c.Caught = recover() }
type Failure struct{}
func (*Failure) Error() string { return "typed nil" }
func NilError() error { return (*Failure)(nil) }
`,
		"api/goxide.toml": `[go_mod]
module="example.org/interop-api"
go="1.27"
[lib]
name="api"
path="."
[dependencies.native]
path="../native"
` + standard,
		"api/lib.gox": `package api
use native;
pub trait Reader { func read(self &mut Self, p []byte) (int,error) }
impl Reader for native::Counter {
 func read(self &mut Self, p []byte) (int,error) {
  if len(p)==0 { return 0,nil }
  self.N++
  p[0]=byte(self.N)
  return 1,nil
 }
}
pub func OwnerView(value &mut native::Counter) Reader { return value }
`,
		// Duplicate impls must fail even without optional Go method collisions.
		"adapter/goxide.toml": `[go_mod]
module="example.org/interop-adapter"
go="1.27"
[lib]
name="adapter"
path="."
[codegen]
go_sugar=false
[dependencies.api]
path="../api"
[dependencies.native]
path="../native"
` + standard,
		"adapter/lib.gox": `package adapter
use api;
use native;
use io;
pub struct BufferReader { raw *native::Counter }
impl api::Reader for BufferReader {
 func read(self &mut Self, p []byte) (int,error) { return self.raw.Read(p) }
}
pub func Wrap(raw *native::Counter) BufferReader { return BufferReader{raw:raw} }
pub func Raw(value BufferReader) *native::Counter { return value.raw }
pub func View(value &mut BufferReader) api::Reader { return value }
pub func Direct(raw *native::Counter, p []byte) (int,error) { return raw.Read(p) }
pub func Interface(raw io::Reader, p []byte) (int,error) { return raw.Read(p) }
pub func MethodValue(raw *native::Counter) func([]byte)(int,error) { return raw.Read }
pub func Catcher(raw *native::Counter) func() { return raw.Catch }
pub func Pointer(raw *native::Counter) *native::Counter { return raw }
pub func Error(err error) error { return err }
pub func NativeError() error { return native::NilError() }
`,
		"consumer/goxide.toml": `[go_mod]
module="example.org/interop-consumer"
go="1.27"
[[bin]]
name="consumer"
path="."
[dependencies.adapter]
path="../adapter"
[dependencies.api]
path="../api"
[dependencies.native]
path="../native"
`,
		"consumer/main.go": `package main
import (
 "fmt"
 "io"
 "reflect"
 adapter "example.org/interop-adapter"
 api "example.org/interop-api"
 native "example.org/interop-native"
)
func verify() {
 raw := &native.Counter{}
 wrapped := adapter.Wrap(raw)
 if adapter.Raw(wrapped) != raw || adapter.Pointer(raw) != raw || adapter.Pointer(nil) != nil { panic("pointer identity") }
 calls := []func([]byte)(int,error){raw.Read, adapter.MethodValue(raw), func(p []byte)(int,error){return adapter.Direct(raw,p)}, func(p []byte)(int,error){return adapter.Interface(raw,p)}, api.OwnerView(raw).Read, adapter.View(&wrapped).Read, wrapped.AsReader().Read}
 for index, call := range calls {
  p := make([]byte,1)
  n, err := call(p)
  if n != 1 || err != nil || int(p[0]) != index+1 || raw.N != index+1 { panic("call or state changed") }
 }
 if _, ok := any(raw).(api.Reader); ok { panic("native type acquired a Goxide marker") }
 if field, ok := reflect.TypeOf(wrapped).FieldByName("raw"); !ok || field.IsExported() { panic("private field visibility") }
 var nilRaw *native.Counter
 if n, err := adapter.Direct(nilRaw,nil); n != 0 || err != io.EOF { panic("nil receiver") }
 if n, err := adapter.Interface(nilRaw,nil); n != 0 || err != io.EOF { panic("typed nil native interface") }
 nilWrapped := adapter.Wrap(nilRaw)
 if n, err := adapter.View(&nilWrapped).Read(nil); n != 0 || err != io.EOF { panic("wrapped nil receiver") }
 typedNil := native.NilError()
 if adapter.Error(nil) != nil || adapter.Error(typedNil) != typedNil || adapter.NativeError() != typedNil { panic("nil error identity") }
 if pointer, ok := adapter.NativeError().(*native.Failure); !ok || pointer != nil { panic("typed nil normalized") }
 func() {
  defer adapter.Catcher(raw)()
  panic("method value frame")
 }()
 if raw.Caught != "method value frame" { panic("native method value gained a call frame") }
}
func main() { verify();fmt.Println("ordinary Go interop and explicit trait bodies") }
`,
		"consumer/main_test.go": `package main
import "testing"
func TestOrdinaryConsumer(t *testing.T) { verify() }
`,
	})
	consumer := filepath.Join(root, "consumer")
	first, err := PrepareProject(consumer, Options{Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	configuredGo(t, first, "build", "-mod=readonly", "-o", filepath.Join(root, "consumer-bin"), first.Entry)
	if output := configuredGo(t, first, "run", "-mod=readonly", first.Entry); output != "ordinary Go interop and explicit trait bodies\n" {
		t.Fatalf("Go consumer output = %q", output)
	}
	configuredGo(t, first, "test", "-mod=readonly", "-count=1", first.Entry)
	before := projectSnapshot(t, filepath.Dir(first.Root))
	lock := unchangedConfigLock(t, consumer, nil)
	second, err := PrepareProject(consumer, Options{Offline: true, Locked: true})
	if err != nil {
		t.Fatal(err)
	}
	if first.Root != second.Root || !reflect.DeepEqual(before, projectSnapshot(t, filepath.Dir(second.Root))) {
		t.Fatal("repeated generation changed ordinary Go interop artifacts")
	}
	unchangedConfigLock(t, consumer, lock)
	for _, test := range []struct{ name, source, want string }{
		{
			name:   "foreign-trait-and-target",
			source: "impl api::Reader for native::Counter { func read(self &mut Self,p []byte)(int,error){return self.Read(p)} }",
			want:   "body impl requires the trait or outer named target to belong to this package",
		},
		{
			name:   "foreign-alias-does-not-own-target",
			source: "type NativeAlias = native::Counter\nimpl api::Reader for NativeAlias { func read(self &mut Self,p []byte)(int,error){return self.Read(p)} }",
			want:   "body impl requires the trait or outer named target to belong to this package",
		},
		{
			name:   "identical-business-bodies-do-not-merge",
			source: "impl api::Reader for BufferReader { func read(self &mut Self,p []byte)(int,error){return self.raw.Read(p)} }",
			want:   "duplicate trait implementation",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			writeProjectFile(t, root, "adapter/rejected.gox", "package adapter\nuse api;\nuse native;\n"+test.source+"\n")
			result, err := PrepareProject(consumer, Options{Offline: true, Locked: true})
			var diagnostic check.Diagnostic
			if result != nil || !errors.As(err, &diagnostic) || diagnostic.Code != check.TraitInvalid || !strings.Contains(diagnostic.Message, test.want) || !strings.HasSuffix(diagnostic.File, "/rejected.gox") {
				t.Fatalf("invalid implementation boundary = %v, %v", result, err)
			}
		})
	}
}
