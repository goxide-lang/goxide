package frontend

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/typecheck"
)

func TestExplicitStdStandalone(t *testing.T) {
	for name, body := range map[string]string{
		"option": `func Make()s.Option[int]{return s.Option[int]::Some(7)}
func Read(v s.Option[int])int{return match v{s.Option[int]::Some(n)=>n,s.Option[int]::None=>0}}`,
		"result":                  `func Next(v s.Result[int,error])s.Result[int,error]{n:=v?;return s.Result[int,error]::Ok(n+1)}`,
		"inferred-go-constructor": `func Read()int{return match s.NewSome(8){s.Option[int]::Some(n)=>n,s.Option[int]::None=>0}}`,
		"alias-shadow": `type Value=int;type Maybe=s.Option[Value]
func Read(v Maybe)int{type Value=string;s:=0;_=s;return match v{_=>9}}`,
	} {
		t.Run(name, func(t *testing.T) {
			result, err := Compile("explicit.gox", []byte("package sample\nimport s \"github.com/goxide-lang/std/sum\"\n"+body))
			if err != nil {
				t.Fatal(err)
			}
			if err = typecheck.CheckPackage("goxide/generated", map[string][]byte{"explicit.go": result.Go}, typecheck.NewImporter("explicit.gox")); err != nil {
				t.Fatal(err)
			}
			again, err := Compile("explicit.gox", []byte("package sample\nimport s \"github.com/goxide-lang/std/sum\"\n"+body))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(result.Go, again.Go) || !bytes.Equal(result.SourceMap, again.SourceMap) {
				t.Fatal("non-deterministic generated output or source map")
			}
			if len(result.Enums) == 0 {
				t.Fatal("missing local concrete instance metadata")
			}
		})
	}
}

func TestExplicitStdModulePayloadValidation(t *testing.T) {
	dir := sharedStdModule(t, "example.org/explicit")
	sharedStdWrite(t, dir, "dep/enum.gox", `package dep
pub enum Event { Ready, Count(int), }
type Scalar int
`)
	sharedStdWrite(t, dir, "box/native.go", `package box
import d "example.org/explicit/dep"
type Box struct{Events map[string][]d.Event}
`)
	sharedStdWrite(t, dir, "a_types.gox", `package explicit
import s "example.org/explicit/dep"
pub enum Mode { Idle, Workers(int), }
type Modes []Mode
type Lookup map[string]Modes
type Foreign struct { Event s.Event }
`)
	sharedStdWrite(t, dir, "native.go", `package explicit
import(s "github.com/goxide-lang/std/sum";d "example.org/explicit/dep")
type NativeForeign struct{Events map[string][]d.Event}
type Other d.Event
type Reordered[E,T any] = s.Result[T,E]
`)
	sharedStdWrite(t, dir, "b_use.gox", `package explicit
import s "github.com/goxide-lang/std/sum"
import b "example.org/explicit/box"
use s::Option as Maybe;
type Envelope struct { Nested s.Option[Lookup]; Direct [1]Mode; Foreign Foreign; Native NativeForeign; Transitive b.Box }
func OtherValue(v s.Option[Other]){match v{_=>{}}}
type Public = s.Option[Envelope]
func Some()s.Option[int]{return s.Option[int]::Some(7)}
func Read(v s.Option[int])int{return match v{s.Option[int]::Some(n)=>n,s.Option[int]::None=>0}}
func Inferred()int{return match s.NewSome(8){s.Option[int]::Some(n)=>n,s.Option[int]::None=>0}}
func ViaUse(v Maybe[int])int{return match v{s.Option[int]::Some(n)=>n,s.Option[int]::None=>0}}
func Next(v Reordered[error,int])s.Result[int,error]{n:=v?;return s.Result[int,error]::Ok(n+1)}
func Inspect(v s.Result[Envelope,Modes],seen *int){match v{s.Result[Envelope,Modes]::Ok(_) if guard(seen)=>{},s.Result[Envelope,Modes]::Ok(_)=>{},s.Result[Envelope,Modes]::Err(_)=>{}}}
func Propagate(v s.Result[Envelope,Modes],seen *int)s.Result[int,Modes]{_=v?;*seen+=1;return s.Result[int,Modes]::Ok(1)}
func InspectPublic(v Public){match v{_=>{}}}
func guard(seen *int)bool{*seen+=1;return true}
func Shadow(v s.Option[int])int{type int=string;s:=0;_=s;return match v{_=>9}}
`)
	output := filepath.Join(t.TempDir(), "generated")
	if _, err := CompileModuleWithOptions(dir, output, ModuleOptions{}); err != nil {
		t.Fatal(err)
	}
	sharedStdWrite(t, output, "explicit_test.go", `package explicit
import("testing";s "github.com/goxide-lang/std/sum";"example.org/explicit/dep";"example.org/explicit/box")
type forgedOption struct{s.Option[int]}
func rejects(t *testing.T, f func()){t.Helper();defer func(){if recover()==nil{t.Error("accepted invalid sum")}}();f()}
func valid()Envelope{return Envelope{Nested:s.NewSome(Lookup{"ok":Modes{NewModeIdle()}}),Direct:[1]Mode{NewModeIdle()},Foreign:Foreign{Event:dep.NewEventReady()},Native:NativeForeign{Events:map[string][]dep.Event{"ok":{dep.NewEventReady()}}},Transitive:box.Box{Events:map[string][]dep.Event{"ok":{dep.NewEventReady()}}}}}
func TestValues(t *testing.T){if Read(Some())!=7||Inferred()!=8||ViaUse(Some())!=7||Shadow(Some())!=9{t.Fatal("option")};if Next(s.NewOk[int,error](4)).(s.Ok[int,error]).Value!=5{t.Fatal("result")};OtherValue(s.NewSome[Other](nil));v:=valid();n:=0;Inspect(s.NewOk[Envelope,Modes](v),&n);_=Propagate(s.NewOk[Envelope,Modes](v),&n);InspectPublic(s.NewSome(v));if n!=2{t.Fatal(n)}}
func TestShape(t *testing.T){for _,v:=range []s.Option[int]{(*s.Some[int])(nil),forgedOption{Some()}}{rejects(t,func(){Read(v)})};for _,v:=range []s.Result[int,error]{nil,(*s.Ok[int,error])(nil)}{rejects(t,func(){Next(v)})}}
func TestPayloadBeforeEffects(t *testing.T){for _,kind:=range []string{"slice","map","nested","array","foreign","native","transitive","err"}{t.Run(kind,func(t *testing.T){v:=valid();r:=s.NewOk[Envelope,Modes](v);switch kind{case "slice":v.Nested=s.NewSome(Lookup{"bad":Modes{nil}});case "map":v.Nested=s.NewSome(Lookup{"bad":Modes{(*GoxideCase4_Mode_Idle)(nil)}});case "nested":v.Nested=(*s.Some[Lookup])(nil);case "array":v.Direct[0]=nil;case "foreign":v.Foreign.Event=nil;case "native":v.Native.Events["bad"]=[]dep.Event{nil};case "transitive":v.Transitive.Events["bad"]=[]dep.Event{nil};case "err":r=s.NewErr[Envelope,Modes](Modes{nil})};if kind!="err"{r=s.NewOk[Envelope,Modes](v)};checks:=[]func(){func(){n:=0;defer func(){if n!=0{t.Error("guard before validation")}}();Inspect(r,&n)},func(){n:=0;defer func(){if n!=0{t.Error("effect before validation")}}();_=Propagate(r,&n)}};if kind!="err"{checks=append(checks,func(){InspectPublic(s.NewSome(v))})};for _,f:=range checks{rejects(t,f)}})}}
`)
	sharedStdGo(t, output, "test", "-race", "./...")
	sharedStdGo(t, output, "vet", "./...")
	// A concrete public transparent alias carries the local validator's owner
	// through an independently generated consumer module.
	consumer := sharedStdModule(t, "example.org/consumer")
	sharedStdWrite(t, consumer, "go.mod", "module example.org/consumer\ngo 1.27\nrequire example.org/explicit v0.0.0\nreplace example.org/explicit => "+filepath.ToSlash(output)+"\n")
	sharedStdWrite(t, consumer, "read.gox", `package consumer
import(p "example.org/explicit";b "example.org/explicit/box";s "github.com/goxide-lang/std/sum")
func Transitive(v s.Option[b.Box]){match v{_=>{}}}
func Check(v p.Public){match v{p.Public::Some(_)=>{},p.Public::None=>{}}}
`)
	target := filepath.Join(t.TempDir(), "consumer")
	if _, err := CompileModuleWithOptions(consumer, target, ModuleOptions{}); err != nil {
		t.Fatal(err)
	}
	sharedStdWrite(t, target, "read_test.go", `package consumer
import("testing";p "example.org/explicit";b "example.org/explicit/box";d "example.org/explicit/dep";s "github.com/goxide-lang/std/sum")
func TestTransitiveArtifact(t *testing.T){defer func(){if recover()==nil{t.Fatal("lost transitive payload check")}}();Transitive(s.NewSome(b.Box{Events:map[string][]d.Event{"bad":{nil}}}))}
func TestOwnerValidation(t *testing.T){defer func(){if recover()==nil{t.Fatal("lost producer payload check")}}();Check(s.NewSome(p.Envelope{}))}
`)
	sharedStdGo(t, target, "test", "-race", "./...")
	sharedStdGo(t, target, "vet", "./...")
}

func TestExplicitStdProjectionBoundaries(t *testing.T) {
	for name, declaration := range map[string]string{
		"pointer":   `type Holder struct{Bad *Mode}`,
		"embedded":  `type Holder struct{Mode}`,
		"blank":     `type Holder struct{_ Mode}`,
		"recursive": `type Holder struct{Next s.Option[Holder]}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Compile("bad.gox", []byte("package bad\nimport s \"github.com/goxide-lang/std/sum\"\nenum Mode{Idle,}\n"+declaration+"\nfunc F(v s.Option[Holder]){match v{_=>{}}}"))
			if err == nil || !strings.Contains(err.Error(), "projection") {
				t.Fatalf("wanted projection rejection: %v", err)
			}
		})
	}
	_, err := Compile("generic.gox", []byte("package bad\nimport s \"github.com/goxide-lang/std/sum\"\ntype Maybe[T any] = s.Option[T]"))
	if err == nil || !strings.Contains(err.Error(), "user generic") {
		t.Fatalf("handwritten generic alias accepted: %v", err)
	}
}

// Neither consumer has a .gox import of the enum's package: the only route is
// native Go type information, and each consumer uses a fresh module importer.
func TestExplicitStdNativePayloadRoutes(t *testing.T) {
	producer := sharedStdModule(t, "example.org/wrappers")
	sharedStdWrite(t, producer, "event/event.gox", `package event
pub enum Event { Ready, }
type Scalar int
`)
	sharedStdWrite(t, producer, "box/box.go", `package box
import e "example.org/wrappers/event"
type Box struct{Events map[string][]e.Event}
`)
	sharedStdWrite(t, producer, "privatebox/box.go", `package privatebox
import e "example.org/wrappers/event"
type Box struct{events []e.Event}
`)
	sharedStdWrite(t, producer, "internal/model/event.gox", `package model
pub enum Event { Ready, }
`)
	sharedStdWrite(t, producer, "publicbox/box.go", `package publicbox
import e "example.org/wrappers/internal/model"
type Box struct{Events []e.Event}
`)
	output := filepath.Join(t.TempDir(), "producer")
	if _, err := CompileModuleWithOptions(producer, output, ModuleOptions{}); err != nil {
		t.Fatal(err)
	}
	consumer := func(t *testing.T) string {
		t.Helper()
		dir := sharedStdModule(t, "example.org/route")
		sharedStdWrite(t, dir, "go.mod", "module example.org/route\ngo 1.27\nrequire example.org/wrappers v0.0.0\nreplace example.org/wrappers => "+filepath.ToSlash(output)+"\n")
		return dir
	}
	for _, kind := range []string{"native-companion", "transitive-artifact"} {
		t.Run(kind, func(t *testing.T) {
			dir := consumer(t)
			prefix := ""
			if kind == "native-companion" {
				sharedStdWrite(t, dir, "native.go", `package route
import e "example.org/wrappers/event"
type Box struct{Events map[string][]e.Event}
`)
			} else {
				prefix = "import b \"example.org/wrappers/box\"\ntype Box = b.Box\n"
			}
			sharedStdWrite(t, dir, "read.gox", "package route\nimport s \"github.com/goxide-lang/std/sum\"\n"+prefix+`func Read(v s.Option[Box])int{return match v{s.Option[Box]::Some(b)=>len(b.Events),s.Option[Box]::None=>0}}
func Next(v s.Option[Box])s.Option[int]{b:=v?;return s.Option[int]::Some(len(b.Events))}
`)
			target := filepath.Join(t.TempDir(), "consumer")
			if _, err := CompileModuleWithOptions(dir, target, ModuleOptions{}); err != nil {
				t.Fatal(err)
			}
			sharedStdWrite(t, target, "read_test.go", `package route
import("testing";s "github.com/goxide-lang/std/sum";e "example.org/wrappers/event")
func TestNativePayload(t *testing.T){valid:=s.NewSome(Box{Events:map[string][]e.Event{"ok":{e.NewEventReady()}}});if Read(valid)!=1{t.Fatal("valid payload")};for _,check:=range []func(s.Option[Box]){func(v s.Option[Box]){_=Read(v)},func(v s.Option[Box]){_=Next(v)}}{for _,bad:=range []e.Event{nil,(*e.GoxideCase5_Event_Ready)(nil)}{func(){defer func(){if recover()==nil{t.Error("lost native enum validation")}}();check(s.NewSome(Box{Events:map[string][]e.Event{"bad":{bad}}}))}()}}}
`)
			sharedStdGo(t, target, "test", "-race", "./...")
			sharedStdGo(t, target, "vet", "./...")
		})
	}
	for kind, want := range map[string]string{"privatebox": "inaccessible enum-bearing field", "publicbox": "inaccessible internal origin"} {
		t.Run(kind, func(t *testing.T) {
			dir := consumer(t)
			sharedStdWrite(t, dir, "read.gox", "package route\nimport(s \"github.com/goxide-lang/std/sum\";b \"example.org/wrappers/"+kind+"\")\nfunc Read(v s.Option[b.Box]){match v{_=>{}}}")
			_, err := CompileModuleWithOptions(dir, filepath.Join(t.TempDir(), "blocked"), ModuleOptions{})
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("wanted %q: %v", want, err)
			}
		})
	}
	t.Run("standalone-scalar-retains-identity", func(t *testing.T) {
		dir := consumer(t)
		file := filepath.Join(dir, "input.gox")
		source := []byte("package route\nimport(s \"github.com/goxide-lang/std/sum\";e \"example.org/wrappers/event\")\nfunc Read(v s.Option[e.Scalar])int{return match v{s.Option[e.Scalar]::Some(n)=>int(n),s.Option[e.Scalar]::None=>0}}")
		if _, err := Compile(file, source); err != nil {
			t.Fatal(err)
		}
	})
}

func TestExplicitStdPayloadDoesNotImportUnrelatedFacade(t *testing.T) {
	dir := sharedStdModule(t, "example.org/payloadroute")
	sharedStdWrite(t, dir, "event/event.gox", `package event
pub enum Event { Ready, }
`)
	sharedStdWrite(t, dir, "a_alias/alias.gox", `package alias
import e "example.org/payloadroute/event"
type Alias = e.Event
func init(){panic("unrelated facade was initialized")}
`)
	sharedStdWrite(t, dir, "box/box.go", `package box
import e "example.org/payloadroute/event"
type Box struct{Event e.Event}
`)
	sharedStdWrite(t, dir, "zzzconsumer/read.gox", `package consumer
import(s "github.com/goxide-lang/std/sum";b "example.org/payloadroute/box")
func Read(v s.Option[b.Box]){match v{_=>{}}}
`)
	output := filepath.Join(t.TempDir(), "out")
	if _, err := CompileModuleWithOptions(dir, output, ModuleOptions{}); err != nil {
		t.Fatal(err)
	}
	sharedStdWrite(t, output, "zzzconsumer/read_test.go", `package consumer
import("testing";s "github.com/goxide-lang/std/sum";b "example.org/payloadroute/box";e "example.org/payloadroute/event")
func TestCanonicalPayloadRoute(t *testing.T){Read(s.NewSome(b.Box{Event:e.NewEventReady()}));defer func(){if recover()==nil{t.Fatal("missing payload validation")}}();Read(s.NewSome(b.Box{}))}
`)
	sharedStdGo(t, output, "test", "-race", "./zzzconsumer")
	sharedStdGo(t, output, "vet", "./zzzconsumer")
}
