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

func TestProceduralSyntaxConstArrayTemplatesGoConsumer(t *testing.T) {
	sdk := compilerSDKForTest(t)
	root := t.TempDir()
	macros, model, app := filepath.Join(root, "macros"), filepath.Join(root, "model"), filepath.Join(root, "app")
	writeProcBuildManifest(t, macros, sdk, "")
	writeProjectFile(t, macros, "lib.gox", `package macros
use syntax::token;
use syntax::proc_macro as pm;
#[proc_macro_derive(FromArray)]
pub func fromArray(ctx pm::Context, input token::TokenStream) pm::Output {
 return deriveFromArray(ctx, input)
}
`)
	writeProjectFile(t, macros, "derive.go", `package macros
import (
 "strconv"
 pm "github.com/goxide-lang/syntax/proc_macro"
 "github.com/goxide-lang/syntax/syn"
 "github.com/goxide-lang/syntax/token"
)
func fragment(ctx pm.Context, source string) token.TokenStream {
 stream, err := token.ParseText(source, ctx.DefSite())
 if err != nil { panic(err) }
 nodes := stream.Tokens()
 for len(nodes) != 0 && nodes[len(nodes)-1].Kind() == token.SemicolonKind { nodes = nodes[:len(nodes)-1] }
 return token.New(nodes...)
}
func group(ctx pm.Context, delimiter token.Delimiter, contents token.TokenStream) token.Token {
 result, err := token.NewGroup(delimiter, contents, ctx.DefSite())
 if err != nil { panic(err) }
 return result
}
func deriveFromArray(ctx pm.Context, input token.TokenStream) pm.Output {
 item, err := syn.Parse(input)
 if err != nil { return pm.Failure(err.Error(), ctx.CallSpan()) }
 fields, ok := item.Type().StructFields()
 generics := item.Generics().Tokens()
 if !ok || item.Alias() || len(generics) != 1 || generics[0].Delimiter() != token.Bracket || !item.Where().IsEmpty() {
  return pm.Failure("FromArray requires a generic struct", item.Span())
 }
 parameters := generics[0].Children().Tokens()
 if len(parameters) != 2 || parameters[0].Kind() != token.IdentKind || parameters[1].Text() != "any" {
  return pm.Failure("FromArray requires one unconstrained type parameter", item.Span())
 }
 element := parameters[0]
 lengthName := "N"
 if element.Text() == lengthName { lengthName = "ArrayN" }
 length := fragment(ctx, lengthName)
 mixed := generics[0].Children().Append(fragment(ctx, ", const " + lengthName + " int").Tokens()...)
 mixedGroup := group(ctx, token.Bracket, mixed)
 array := group(ctx, token.Bracket, length)
 instance := item.Name().ToTokens().Append(group(ctx, token.Bracket, token.New(element)))
 var initializers token.TokenStream
 index := 0
 for _, field := range fields {
  fieldType := field.Type().ToTokens().Tokens()
  if len(field.Names()) == 0 || len(fieldType) != 1 || fieldType[0].Text() != element.Text() {
   return pm.Failure("FromArray requires named fields of the type parameter", field.Span())
  }
  for _, name := range field.Names() {
   if name.Text() == "_" { return pm.Failure("FromArray requires nonblank fields", name.Span()) }
   initializers = initializers.Append(name.ToTokens().Tokens()...).Append(fragment(ctx, ": values").Tokens()...)
   initializers = initializers.Append(group(ctx, token.Bracket, fragment(ctx, strconv.Itoa(index))))
   initializers = initializers.Append(fragment(ctx, ",").Tokens()...)
   index++
  }
 }
 constructorName := "From" + item.Name().Text() + "Array"
 params := group(ctx, token.Parenthesis, fragment(ctx, "values").Append(array, element))
 constructorBody := fragment(ctx, "return").Append(instance.Tokens()...).Append(group(ctx, token.Brace, initializers))
 output := fragment(ctx, "pub func " + constructorName).Append(mixedGroup, params)
 output = output.Append(instance.Tokens()...).Append(group(ctx, token.Brace, constructorBody), token.NewSemicolon(ctx.DefSite()))
 target := token.New(array).Append(instance.Tokens()...)
 traitName := "First" + item.Name().Text()
 trait := fragment(ctx, "pub trait " + traitName + " { type Item; func first(self Self) Self::Item }")
 output = output.Append(trait.Tokens()...).Append(token.NewSemicolon(ctx.DefSite()))
 association := fragment(ctx, "type Item =").Append(instance.Tokens()...).Append(token.NewSemicolon(ctx.DefSite()))
 method := fragment(ctx, "func first(self Self) Self::Item { return self[0] }")
 impl := fragment(ctx, "impl").Append(mixedGroup).Append(fragment(ctx, traitName + " for").Tokens()...).Append(target.Tokens()...)
 impl = impl.Append(group(ctx, token.Brace, association.Append(method.Tokens()...)))
 return pm.Success(output.Append(impl.Tokens()...))
}
`)
	writeProjectFile(t, model, project.ManifestName, `[go_mod]
module="example.org/const-array-model"
go="1.27"
[lib]
name="model"
path="."
[dependencies.macros]
path="../macros"
`)
	writeProjectFile(t, model, "lib.gox", `package model
use macros;
#[derive(macros::FromArray)]
pub struct Pair[T any] { pub Left, Right T }
#[derive(macros::FromArray)]
pub struct Triple[N any] { pub First N; pub Middle N; pub Last N }
pub func ForwardPair[T any,const N int](values [N]T) Pair[T] { return FromPairArray[T,N](values) }
pub func Evaluate() (int, string) {
 pair := ForwardPair([2]int{4,7})
 triple := FromTripleArray[string,3]([3]string{"a","b","c"})
 pairs := [2]Pair[int]{pair,pair}
 triples := [1]Triple[string]{triple}
 firstPair := FirstPair::first(pairs)
 firstTriple := FirstTriple::first(triples)
 return firstPair.Left + firstPair.Right, firstTriple.First + firstTriple.Middle + firstTriple.Last
}
`)
	writeProjectFile(t, app, project.ManifestName, `[go_mod]
module="example.org/const-array-consumer"
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
 model "example.org/const-array-model"
)
func main() { fmt.Println(model.Evaluate()) }
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
	if deriveCalls != 2 {
		t.Fatalf("FromArray executions = %d, want 2", deriveCalls)
	}
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	for _, operation := range []string{"build", "run"} {
		arguments := []string{operation, "-mod=readonly"}
		if operation == "build" {
			arguments = append(arguments, "-o", filepath.Join(root, "consumer"))
		}
		command := exec.Command(tool, append(arguments, first.Entry)...)
		command.Dir, command.Env = first.Root, project.GoEnv(true)
		output, err := command.CombinedOutput()
		if err != nil || operation == "run" && string(output) != "11 abc\n" {
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
		t.Fatal("locked replay changed generated const templates or procedural artifacts")
	}
	after, err := os.ReadFile(filepath.Join(app, "goxide.lock"))
	if err != nil || !bytes.Equal(lock, after) {
		t.Fatalf("locked replay changed dependency lock: %v", err)
	}
}
