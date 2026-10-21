package compile

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/goxide-lang/goxide/internal/project"
	pm "github.com/goxide-lang/syntax/proc_macro"
)

func TestProceduralSyntaxGenericConditionalImplGoConsumer(t *testing.T) {
	sdk := compilerSDKForTest(t)
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	module, err := project.ReadMod(tool, filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	var stdVersion string
	for _, dependency := range module.Require {
		if dependency.Path == "github.com/goxide-lang/std" {
			stdVersion = dependency.Version
		}
	}
	if stdVersion == "" {
		t.Fatal("compiler go.mod must pin std")
	}
	root := t.TempDir()
	macros, protocol := filepath.Join(root, "macros"), filepath.Join(root, "protocol")
	model, app := filepath.Join(root, "model"), filepath.Join(root, "app")
	writeProcBuildManifest(t, macros, sdk, "")
	writeProjectFile(t, macros, "lib.gox", `package macros
use syntax::token;
use syntax::proc_macro as pm;
#[proc_macro_derive(GenericCopy)]
pub func genericCopy(ctx pm::Context, input token::TokenStream) pm::Output {
    return deriveGenericCopy(ctx, input)
}
`)
	// The macro implementation is ordinary Go. Only its source registration
	// uses Goxide syntax; the producer is built and executed as a WASI command.
	writeProjectFile(t, macros, "derive.go", `package macros
import (
    pm "github.com/goxide-lang/syntax/proc_macro"
    "github.com/goxide-lang/syntax/syn"
    "github.com/goxide-lang/syntax/token"
)
func genericFragment(ctx pm.Context, text string) token.TokenStream {
    stream, err := token.ParseText(text, ctx.DefSite())
    if err != nil { panic(err) }
    tokens := stream.Tokens()
    for len(tokens) != 0 && tokens[len(tokens)-1].Kind() == token.SemicolonKind {
        tokens = tokens[:len(tokens)-1]
    }
    return token.New(tokens...)
}
func genericGroup(ctx pm.Context, delimiter token.Delimiter, body token.TokenStream) token.Token {
    group, err := token.NewGroup(delimiter, body, ctx.DefSite())
    if err != nil { panic(err) }
    return group
}
func deriveGenericCopy(ctx pm.Context, input token.TokenStream) pm.Output {
    item, err := syn.Parse(input)
    if err != nil { return pm.Failure(err.Error(), ctx.CallSpan()) }
    fields, ok := item.Type().StructFields()
    generics := item.Generics().Tokens()
    if !ok || item.Alias() || len(generics) != 1 || generics[0].Delimiter() != token.Bracket || !item.Where().IsEmpty() {
        return pm.Failure("GenericCopy requires a struct with one type parameter", item.Span())
    }
    parameters := generics[0].Children().Tokens()
    if len(parameters) != 2 || parameters[0].Kind() != token.IdentKind || parameters[1].Text() != "any" {
        return pm.Failure("GenericCopy requires one unconstrained type parameter", item.Span())
    }
    parameter := token.New(parameters[0])
    root, err := ctx.RuntimeRoot("example.org/generic-copy-protocol")
    if err != nil { return pm.Failure(err.Error(), ctx.CallSpan()) }
    protocol := root.Append(genericFragment(ctx, "::Duplicate").Tokens()...)
    var values token.TokenStream
    for _, field := range fields {
        if len(field.Names()) == 0 { return pm.Failure("GenericCopy requires named fields", field.Span()) }
        for _, name := range field.Names() {
            if name.Text() == "_" { return pm.Failure("GenericCopy requires named fields", field.Span()) }
            argument := genericFragment(ctx, "&self.").Append(name.ToTokens().Tokens()...)
            call := protocol.Append(genericFragment(ctx, "::duplicate").Tokens()...).Append(genericGroup(ctx, token.Parenthesis, argument))
            values = values.Append(name.ToTokens().Tokens()...).Append(genericFragment(ctx, ":").Tokens()...)
            values = values.Append(call.Tokens()...).Append(genericFragment(ctx, ",").Tokens()...)
        }
    }
    body := genericFragment(ctx, "return Self").Append(genericGroup(ctx, token.Brace, values))
    method := genericFragment(ctx, "func duplicate(self &Self) Self").Append(genericGroup(ctx, token.Brace, body))
    output := genericFragment(ctx, "impl").Append(item.Generics().Tokens()...)
    output = output.Append(protocol.Tokens()...).Append(genericFragment(ctx, "for").Tokens()...)
    output = output.Append(item.Name().ToTokens().Tokens()...).Append(genericGroup(ctx, token.Bracket, parameter))
    output = output.Append(genericFragment(ctx, "where").Tokens()...).Append(parameter.Tokens()...)
    output = output.Append(genericFragment(ctx, ":").Tokens()...).Append(protocol.Tokens()...)
    output = output.Append(genericGroup(ctx, token.Brace, method))
    return pm.Success(output)
}
`)
	writeProjectFile(t, protocol, project.ManifestName, `[go_mod]
module="example.org/generic-copy-protocol"
go="1.27"
[lib]
name="protocol"
path="."
`)
	writeProjectFile(t, protocol, "lib.gox", `package protocol
pub trait Duplicate { func duplicate(self &Self) Self }
impl Duplicate for int { func duplicate(self &Self) Self { return *self } }
impl[T Duplicate] Duplicate for []T {
    func duplicate(self &Self) Self {
        if *self == nil { return nil }
        result := make([]T, len(*self))
        for index := range *self { result[index] = Duplicate::duplicate(&(*self)[index]) }
        return result
    }
}
`)
	writeProjectFile(t, model, project.ManifestName, `[go_mod]
module="example.org/generic-copy-model"
go="1.27"
[lib]
name="model"
path="."
[dependencies.macros]
path="../macros"
[dependencies.protocol]
path="../protocol"
[dependencies.standard]
go="github.com/goxide-lang/std"
version="`+stdVersion+`"
`)
	writeProjectFile(t, model, "lib.gox", `package model
use macros;
use protocol;
#[derive(macros::GenericCopy)]
pub struct Parcel[T any] { pub Value T }
pub func MakeInts() Parcel[[]int] { return Parcel[[]int]{Value: []int{1, 2}} }
pub func CloneInts(value &Parcel[[]int]) Parcel[[]int] { return protocol::Duplicate::duplicate(value) }
`)
	writeProjectFile(t, app, project.ManifestName, `[go_mod]
module="example.org/generic-copy-consumer"
go="1.27"
[[bin]]
name="app"
path="."
[dependencies.model]
path="../model"
`)
	writeProjectFile(t, app, "main.go", `package main
import (
    "fmt"
    model "example.org/generic-copy-model"
)
func main() {
    original := model.MakeInts()
    copied := model.CloneInts(&original)
    original.Value[0] = 9
    unconstrained := model.Parcel[func()]{Value: func(){}}
    unconstrained.Value()
    fmt.Println(original.Value, copied.Value)
}
`)
	deriveCalls := 0
	run := testWASI(t, func(request pm.Request) {
		if request.Kind() == pm.Derive {
			deriveCalls++
		}
	})
	first, err := PrepareProject(app, Options{Offline: true, RunProcMacro: run})
	if err != nil {
		t.Fatal(err)
	}
	if deriveCalls == 0 {
		t.Fatal("GenericCopy was not executed by the WASI host")
	}
	for _, operation := range []string{"build", "run"} {
		arguments := []string{operation, "-mod=readonly"}
		if operation == "build" {
			arguments = append(arguments, "-o", filepath.Join(root, "consumer"))
		}
		command := exec.Command(tool, append(arguments, first.Entry)...)
		command.Dir, command.Env = first.Root, project.GoEnv(true)
		output, err := command.CombinedOutput()
		if err != nil || operation == "run" && string(output) != "[9 2] [1 2]\n" {
			t.Fatalf("ordinary Go consumer %s: %v\n%s", operation, err, output)
		}
	}
	snapshot := projectSnapshot(t, filepath.Dir(first.Root))
	lock, err := os.ReadFile(filepath.Join(app, "goxide.lock"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := PrepareProject(app, Options{Offline: true, Locked: true, RunProcMacro: run})
	if err != nil {
		t.Fatal(err)
	}
	if first.Root != second.Root || !reflect.DeepEqual(snapshot, projectSnapshot(t, filepath.Dir(second.Root))) {
		t.Fatal("locked replay changed generated generic impls or procedural artifacts")
	}
	after, err := os.ReadFile(filepath.Join(app, "goxide.lock"))
	if err != nil || !bytes.Equal(lock, after) {
		t.Fatalf("locked replay changed dependency lock: %v", err)
	}
}
