package compile

import (
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/project"
)

func TestProceduralSyntaxDerivePreservesFieldTypes(t *testing.T) {
	sdk := compilerSDKForTest(t)
	module, err := project.ReadMod(filepath.Join(runtime.GOROOT(), "bin", "go"), filepath.Join("..", ".."))
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
	macros, app, protocol := filepath.Join(root, "macros"), filepath.Join(root, "app"), filepath.Join(root, "protocol")
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
func fragment(ctx pm::Context, text string) token::TokenStream {
 var result, err = token::ParseText(text, ctx.DefSite())
 if err != nil { panic(err) }
 var tokens = result.Tokens()
 for len(tokens) != 0 && tokens[len(tokens)-1].Kind() == token::SemicolonKind {
  tokens = tokens[:len(tokens)-1]
 }
 return token::New(tokens...)
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
#[proc_macro_derive(CopyFields)]
pub func copyFields(ctx pm::Context, input token::TokenStream) pm::Output {
 var item, err = syn::Parse(input)
 if err != nil { return pm::Failure(err.Error(), ctx.CallSpan()) }
 var fields, ok = item.Type().StructFields()
 if !ok || item.Alias() || !item.Generics().IsEmpty() {
  return pm::Failure("CopyFields requires a concrete struct", item.Span())
 }
 var root, rootErr = ctx.RuntimeRoot("example.org/copy-protocol")
 if rootErr != nil { return pm::Failure(rootErr.Error(), ctx.CallSpan()) }
 var protocol = root.Append(fragment(ctx, "::Duplicate").Tokens()...)
 var members token::TokenStream
 for _, field := range fields {
  if len(field.Names()) == 0 { return pm::Failure("CopyFields requires named fields", field.Span()) }
  for _, name := range field.Names() {
   if name.Text() == "_" { return pm::Failure("CopyFields requires named fields", name.Span()) }
   var argument = fragment(ctx, "&self.").Append(name.ToTokens().Tokens()...)
   var call = protocol.Append(fragment(ctx, "::duplicate").Tokens()...).Append(group(ctx, token::Parenthesis, argument))
   members = members.Append(name.ToTokens().Tokens()...).Append(fragment(ctx, ":").Tokens()...)
   members = members.Append(call.Tokens()...).Append(fragment(ctx, ",").Tokens()...)
  }
 }
 var body = fragment(ctx, "return Self").Append(group(ctx, token::Brace, members))
 var method = fragment(ctx, "func duplicate(self &Self) Self").Append(group(ctx, token::Brace, body))
 var output = fragment(ctx, "impl").Append(protocol.Tokens()...).Append(fragment(ctx, "for").Tokens()...)
 output = output.Append(item.Name().ToTokens().Tokens()...).Append(group(ctx, token::Brace, method))
 return pm::Success(output)
}
`)
	writeProjectFile(t, protocol, "goxide.toml", `[go_mod]
module="example.org/copy-protocol"
go="1.27"
[lib]
name="protocol"
path="."
`)
	writeProjectFile(t, protocol, "lib.gox", `package protocol
pub trait Duplicate { func duplicate(self &Self) Self }
`)
	writeProjectFile(t, app, "goxide.toml", `[go_mod]
module="example.org/syn-consumer"
go="1.27"
[[bin]]
name="app"
path="."
[dependencies.macros]
path="../macros"
[dependencies.api]
path="../protocol"
[dependencies.standard]
go="github.com/goxide-lang/std"
version="`+stdVersion+`"
`)
	consumer := `package main
use macros;
use fmt;
use api;
type localNumber = int32
#[derive(macros::Getters)]
type Record struct { Count localNumber; Text string }
var calls string
type Payload struct { Name string; Items []int }
impl api::Duplicate for Payload {
 func duplicate(self &Self) Self {
  calls += self.Name
  var items = make([]int, len(self.Items))
  for i := range self.Items { items[i] = self.Items[i] }
  return Self{Name: self.Name, Items: items}
 }
}
#[derive(macros::CopyFields)]
type Pair struct { Left Payload; Right Payload }
func main() {
 var record = Record{Count: 7, Text: "ok"}
 var count localNumber = ReadCount(record)
 fmt::Println(count, ReadText(record))
 var pair = Pair{Left: Payload{Name: "L", Items: []int{1}}, Right: Payload{Name: "R", Items: []int{2}}}
 var cloned = api::Duplicate::duplicate(&pair)
 if calls != "LR" { panic("field protocols did not run exactly once in declaration order") }
 cloned.Left.Items[0] = 8
 cloned.Right.Items[0] = 9
 if pair.Left.Items[0] != 1 || pair.Right.Items[0] != 2 { panic("field protocol storage was shared") }
 if cloned.Left.Name != "L" || cloned.Right.Name != "R" { panic("field protocol result was lost") }
 fmt::Println(calls, pair.Left.Items[0], cloned.Left.Items[0], pair.Right.Items[0], cloned.Right.Items[0])
}
`
	writeProjectFile(t, app, "main.gox", consumer)
	run := testWASI(t, nil)
	first, err := PrepareProject(app, Options{Offline: true, RunProcMacro: run})
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "run", "-mod=readonly", first.Entry)
	command.Dir, command.Env = first.Root, project.GoEnv(true)
	if output, err := command.CombinedOutput(); err != nil || string(output) != "7 ok\nLR 1 8 2 9\n" {
		t.Fatalf("generated getters and ordinary trait implementation: %v\n%s", err, output)
	}
	snapshot := projectSnapshot(t, first.Root)
	second, err := PrepareProject(app, Options{Offline: true, Locked: true, RunProcMacro: run})
	if err != nil {
		t.Fatal(err)
	}
	if first.Root != second.Root || !reflect.DeepEqual(snapshot, projectSnapshot(t, second.Root)) {
		t.Fatal("replay changed generated getters or ordinary trait implementation")
	}
	// A handwritten implementation must conflict with the generated ordinary
	// impl through the normal trait checker, not a builtin derive registry.
	writeProjectFile(t, app, "main.gox", consumer+`
impl api::Duplicate for Pair {
 func duplicate(self &Self) Self { return *self }
}
`)
	if _, err := PrepareProject(app, Options{Offline: true, Locked: true, RunProcMacro: run}); err == nil || !strings.Contains(err.Error(), "duplicate trait implementation") {
		t.Fatalf("generated and handwritten ordinary implementations did not conflict: %v", err)
	}
}
