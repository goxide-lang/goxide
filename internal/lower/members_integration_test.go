package lower_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/lower"
)

func TestMemberMappingThroughPreparedModule(t *testing.T) {
	tests := []struct {
		name, want string
		files      map[string]string
	}{
		{
			name: "generic_cross_package",
			want: "1 3 4\n",
			files: map[string]string{
				"lib/lib.gox": `package lib
pub struct box[T any] {
	pub value T
	Hidden int
	pub child *box[T]
}
pub func (b box[T]) next() *box[T] { return b.child }
pub func (b box[T]) readHidden() int { return b.Hidden }
pub func makeBox() *box[int] {
	return &box[int]{value: 1, Hidden: 2, child: &box[int]{value: 3, Hidden: 4}}
}
`,
				"main.gox": `package main
use fmt;
use members::lib;
func main() {
	box := lib::makeBox()
	fmt::Println(box.value, box.next().value, box.next().readHidden())
}
`,
			},
		},
		{
			name: "renamed_embedding_and_distinct_key",
			want: "7 9\n",
			files: map[string]string{
				"main.gox": `package main
use fmt;
struct Private { pub amount int }
type holder struct { Private }
struct local { value int }
pub var value = 9
func main() {
	h := holder{Private: Private{amount: 7}}
	v := local{value: value}
	fmt::Println(h.Private.amount, v.value)
}
`,
			},
		},
		{
			name: "promoted_alias_and_method_expression",
			want: "3 5 7\n",
			files: map[string]string{
				"main.gox": `package main
use fmt;
pub struct item[T any] { pub value T }
pub func (v item[T]) read() T { return v.value }
pub type alias = item[int]
type outer struct { alias }
struct local { value int }
func main() {
 o := outer{alias: alias{value: 3}}
 b := local{value: 5}
 read := item[int]::read
 fmt::Println(o.value, b.value, read(item[int]{value: 7}))
}
`,
			},
		},
		{
			name: "native_field_key_and_source_type",
			want: "1\n",
			files: map[string]string{
				"main.gox": `package main
use fmt;
use net::http;
struct Header {}
func main() {
 request := http::Request{Header: http::Header{"X-Test": []string{"value"}}}
 fmt::Println(len(request.Header))
}
`,
			},
		},
		{
			name: "inferred_range_and_builtin_members",
			want: "2\n7\n9\n13\n17 19\n23 29 31\n37 84\n",
			files: map[string]string{
				"main.gox": `package main
use fmt;
type number int
pub func (n number) read() int { return int(n) }
pub struct item { pub value int }
type maker func() item
func sequence(yield func(item) bool) { yield(item{value: 7}) }
func first[T interface{~[]item}](values T) int { return values[0].value }
func total[T interface{~[]item}](values T) int {
 result := 0
 for _, value := range values { result += value.value }
 return result
}
func indexed[T interface{~[]item | ~[2]item | ~*[3]item}](values T) int {
 return values[0].value
}
func receive[T interface{~chan item | ~<-chan item}](values T) int {
 return (<-values).value
}
func channelTotal[T interface{~chan item | ~<-chan item}](values T) int {
 result := 0
 for value := range values { result += value.value }
 return result
}
func main() {
 fmt::Println(min(number(2), number(3)).read())
 for value := range sequence { fmt::Println(value.value) }
 data := [1]item{{value: 9}}
 for _, value := range &data { fmt::Println(value.value) }
 makeItem := maker(func() item { return item{value: 13} })
 fmt::Println(makeItem().value)
 fmt::Println(first([]item{{value: 17}}), total([]item{{value: 19}}))
 fmt::Println(indexed([]item{{value: 23}}), indexed([2]item{{value: 29}}), indexed(&[3]item{{value: 31}}))
 channel := make(chan item, 3)
 channel <- item{value: 37}
 channel <- item{value: 41}
 channel <- item{value: 43}
 close(channel)
 var incoming <-chan item = channel
 fmt::Println(receive(channel), channelTotal(incoming))
}
`,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := t.TempDir()
			for name, contents := range test.files {
				writeDiagnosticModuleFile(t, source, name, []byte(contents))
			}
			module, err := lower.PrepareModule([]string{source}, "members", testConfig(t))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(module) })
			again, err := lower.PrepareModule([]string{source}, "members", testConfig(t))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(again) })
			for name := range test.files {
				generatedName := strings.TrimSuffix(name, ".gox") + ".go"
				first, err := os.ReadFile(filepath.Join(module, generatedName))
				if err != nil {
					t.Fatal(err)
				}
				second, err := os.ReadFile(filepath.Join(again, generatedName))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(first, second) {
					t.Fatalf("member generation changed for %s", name)
				}
			}
			if test.name == "generic_cross_package" {
				generated, err := os.ReadFile(filepath.Join(module, "lib", "lib.go"))
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(generated), "_Hidden") {
					t.Fatalf("private uppercase field did not receive its Go name:\n%s", generated)
				}
			}
			if got := runGo(t, module, "run", "."); got != test.want {
				t.Fatalf("runtime output = %q, want %q", got, test.want)
			}
		})
	}
}

func TestMemberMappingFromNativeGo(t *testing.T) {
	sources := map[string][]byte{
		"model.gox": []byte(`package main
pub struct item[T any] { pub value T }
pub func (v item[T]) read() T { return v.value }
`),
		"native.go": []byte(`package main
func main() {
 v := Item[int]{Value: 11}
 println(v.Value, v.Read())
}
`),
	}
	output, err := lower.CompilePackage(sources, "members", nil, nil, lower.PackageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	again, err := lower.CompilePackage(sources, "members", nil, nil, lower.PackageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeDiagnosticModuleFile(t, dir, "go.mod", []byte("module members\n\ngo 1.27\n"))
	writeDiagnosticModuleFile(t, dir, "native.go", sources["native.go"])
	for name, data := range output {
		if !bytes.Equal(data, again[name]) {
			t.Fatalf("member generation changed for %s", name)
		}
		writeDiagnosticModuleFile(t, dir, strings.TrimSuffix(name, filepath.Ext(name))+".go", data)
	}
	runGo(t, dir, "build", "./...")
	if got := runGo(t, dir, "run", "."); got != "11 11\n" {
		t.Fatalf("runtime output = %q", got)
	}
}
