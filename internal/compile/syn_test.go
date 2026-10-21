package compile

import (
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/goxide-lang/goxide/internal/project"
)

func TestProceduralSyntaxDerivePreservesFieldTypes(t *testing.T) {
	sdk := compilerSDKForTest(t)
	root := t.TempDir()
	macros, app := filepath.Join(root, "macros"), filepath.Join(root, "app")
	writeProcBuildManifest(t, macros, sdk, "")
	writeProjectFile(t, macros, "lib.gox", `package macros
use syntax::token;
use syntax::proc_macro as pm;
use syntax::syn;

func ident(ctx pm::Context, name string) token::Token {
 var result, err = token::NewIdent(name, false, ctx.DefSite())
 if err != nil { panic(err) }
 return result
}
func keyword(ctx pm::Context, name string) token::Token {
 var result, err = token::NewKeyword(name, ctx.DefSite())
 if err != nil { panic(err) }
 return result
}
func group(ctx pm::Context, kind token::Delimiter, body token::TokenStream) token::Token {
 var result, err = token::NewGroup(kind, body, ctx.DefSite())
 if err != nil { panic(err) }
 return result
}
#[proc_macro_derive(Getters)]
pub func getters(ctx pm::Context, input token::TokenStream) pm::Output {
 var item, err = syn::Parse(input)
 if err != nil { return pm::Failure(err.Error(), ctx.CallSpan()) }
 var fields, ok = item.Type().StructFields()
 if !ok { return pm::Failure("Getters requires a struct", item.Span()) }
 var output token::TokenStream
 var dot, dotErr = token::NewPunct(".", ctx.DefSite())
 if dotErr != nil { return pm::Failure(dotErr.Error(), ctx.CallSpan()) }
 for _, field := range fields {
  for _, name := range field.Names() {
   var params = token::New(ident(ctx, "value")).Append(item.Name().ToTokens().Tokens()...)
   var body = token::New(keyword(ctx, "return"), ident(ctx, "value"), dot).Append(name.ToTokens().Tokens()...)
   output = output.Append(keyword(ctx, "func"), ident(ctx, "Read" + name.Text()), group(ctx, token::Parenthesis, params))
   output = output.Append(field.Type().ToTokens().Tokens()...)
   output = output.Append(group(ctx, token::Brace, body), token::NewSemicolon(ctx.DefSite()))
  }
 }
 return pm::Success(output)
}
`)
	writeProjectFile(t, app, "goxide.toml", `[go_mod]
module="example.org/syn-consumer"
go="1.27"
[[bin]]
name="app"
path="."
[dependencies.macros]
path="../macros"
`)
	writeProjectFile(t, app, "main.gox", `package main
use macros;
use fmt;
type localNumber = int32
#[derive(macros::Getters)]
type Record struct { Count localNumber; Text string }
func main() {
 var record = Record{Count: 7, Text: "ok"}
 var count localNumber = ReadCount(record)
 fmt::Println(count, ReadText(record))
}
`)
	run := testWASI(t, nil)
	first, err := PrepareProject(app, Options{Offline: true, RunProcMacro: run})
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "run", "-mod=readonly", first.Entry)
	command.Dir, command.Env = first.Root, project.GoEnv(true)
	if output, err := command.CombinedOutput(); err != nil || string(output) != "7 ok\n" {
		t.Fatalf("generated getters: %v\n%s", err, output)
	}
	snapshot := projectSnapshot(t, first.Root)
	second, err := PrepareProject(app, Options{Offline: true, Locked: true, RunProcMacro: run})
	if err != nil {
		t.Fatal(err)
	}
	if first.Root != second.Root || !reflect.DeepEqual(snapshot, projectSnapshot(t, second.Root)) {
		t.Fatal("replay changed generated getters")
	}
}
