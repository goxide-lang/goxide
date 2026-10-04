#!/usr/bin/env python3
"""Isolated hand-written Go mechanisms. Not an hgo parser or compiler test."""
import argparse
import json
import os
import re
from pathlib import Path
import subprocess
import tempfile

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--go', required=True, help='real Go 1.27+ compiler path')
parser.add_argument('--report', type=Path)
args = parser.parse_args()
env = dict(os.environ, GOTOOLCHAIN='local', GOWORK='off', GOFLAGS='-buildvcs=false', GOPROXY='off', GOSUMDB='off')
results = []

with tempfile.TemporaryDirectory(prefix='hgo-mod-research-') as temporary:
    root = Path(temporary)
    env.setdefault('GOCACHE', str(root / 'cache'))
    env.setdefault('GOMODCACHE', str(root / 'modcache'))
    version = subprocess.check_output([args.go, 'version'], env=env, text=True).strip()
    match = re.match(r'go version go1\.(\d+)', version)
    if not match or int(match.group(1)) < 27:
        raise SystemExit('not a Go compiler: ' + version)

    def case(name, files, commands, failure=None):
        directory = root / name
        directory.mkdir()
        (directory / 'go.mod').write_text('module example.org/probe\n\ngo 1.27\n')
        for filename, content in files.items():
            path = directory / filename
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content)
        outputs = []
        for command in commands:
            proc = subprocess.run([args.go, *command], cwd=directory, env=env, text=True,
                                  stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=120)
            output = proc.stdout.replace(str(directory), '<fixture>')
            outputs.append({'command': command, 'exit': proc.returncode, 'output': output})
            if failure:
                assert proc.returncode != 0 and failure in output, (name, output)
            else:
                assert proc.returncode == 0, (name, output)
        results.append({'name': name, 'expected': 'reject' if failure else 'pass', 'runs': outputs})
        print('PASS', name, '(expected rejection)' if failure else '', flush=True)

    case('merged_recursive_identity', {
        'left.go': '''package probe
        type leftNode struct { Right *rightNode }
        func leftEven(n int) bool { if n == 0 { return true }; return rightOdd(n-1) }
        ''',
        'right.go': '''package probe
        type rightNode struct { Left *leftNode }
        func rightOdd(n int) bool { if n == 0 { return false }; return leftEven(n-1) }
        ''',
        'identity_test.go': '''package probe
        import "testing"
        func TestRecursive(t *testing.T) {
          a:=&leftNode{}; b:=&rightNode{Left:a}; a.Right=b
          if a.Right.Left!=a || !leftEven(20) || !rightOdd(21) { t.Fatal("identity/recursion") }
        }
        '''}, [['test', '-race', './...'], ['vet', './...']])

    case('split_import_cycle', {
        'a/a.go': '''package a
        import "example.org/probe/b"
        type A struct { B *b.B }
        func F(n int) int { if n==0{return 0};return b.G(n-1) }
        ''',
        'b/b.go': '''package b
        import "example.org/probe/a"
        type B struct { A *a.A }
        func G(n int) int { if n==0{return 0};return a.F(n-1) }
        '''}, [['test', './...']], 'import cycle not allowed')

    case('merged_infinite_value_type', {
        'bad.go': 'package probe\ntype A struct { B B }; type B struct { A A }\n'
        }, [['test', './...']], 'invalid recursive type')

    case('split_parent_private', {
        'parent/parent.go': 'package parent\nfunc hidden() int { return 7 }\n',
        'parent/child/child.go': '''package child
        import "example.org/probe/parent"
        func Read() int { return parent.hidden() }
        '''}, [['test', './...']], 'undefined: parent.hidden')

    case('merged_private_interface_collision', {
        'collision_test.go': '''package probe
        import "testing"
        // Incorrect lowering: unrelated logical modules share Go's private name.
        type naiveI interface { secret() }
        type naiveT struct{}
        func (naiveT) secret() {}
        // Candidate correction: private method keys include module identity.
        type aI interface { hgoA_secret() }
        type bT struct{}
        func (bT) hgoB_secret() {}
        func TestCollision(t *testing.T) {
          if _,ok:=any(naiveT{}).(naiveI);!ok { t.Fatal("counterexample missing") }
          if _,ok:=any(bT{}).(aI);ok { t.Fatal("private module identities conflated") }
        }
        '''}, [['test', '-race', './...'], ['vet', './...']])

    case('reexport_alias_generics_methods', {
        'origin/origin.go': '''package origin
        type Box[T any] struct { Value T }
        func (b Box[T]) Get() T { return b.Value }
        type Item struct { N int }
        func (i Item) Get() int { return i.N }
        ''',
        'facade/facade.go': '''package facade
        import "example.org/probe/origin"
        type Box[T any] = origin.Box[T]
        type Item = origin.Item
        ''',
        'consumer/alias_test.go': '''package consumer
        import ("testing"; "reflect"; "example.org/probe/origin"; "example.org/probe/facade")
        func TestIdentity(t *testing.T) {
          a:=origin.Box[int]{Value:42}; var b facade.Box[int]=a
          if b.Get()!=42 || reflect.TypeOf(a)!=reflect.TypeOf(b) {t.Fatal("generic identity")}
          x:=origin.Item{N:7}; var y facade.Item=x
          if y.Get()!=7 || reflect.TypeOf(x)!=reflect.TypeOf(y) {t.Fatal("method identity")}
        }
        '''}, [['test', '-race', './...'], ['vet', './...']])

    case('alias_cannot_add_external_method', {
        'origin/origin.go': 'package origin\ntype T struct{}\n',
        'facade/facade.go': '''package facade
        import "example.org/probe/origin"
        type Alias = origin.T
        func (Alias) Extra() {}
        '''}, [['test', './...']], 'cannot define new methods on non-local type')

    case('reexport_function_and_variable_limits', {
        'origin/origin.go': '''package origin
        var Counter=1
        func Value() int { return Counter }
        ''',
        'facade/facade.go': '''package facade
        import "example.org/probe/origin"
        var Snapshot = origin.Counter
        var FunctionVariable = origin.Value
        func Wrapper() int { return origin.Value() }
        ''',
        'facade/limits_test.go': '''package facade
        import ("testing"; "reflect"; "example.org/probe/origin")
        func TestLimits(t *testing.T) {
          origin.Counter=2
          if Snapshot!=1 || &Snapshot==&origin.Counter {t.Fatal("variable alias counterexample missing")}
          if reflect.ValueOf(Wrapper).Pointer()==reflect.ValueOf(origin.Value).Pointer(){t.Fatal("wrapper unexpectedly identical")}
          FunctionVariable=func()int{return 99}
          if FunctionVariable()!=99 || origin.Value()!=2 {t.Fatal("function variable counterexample missing")}
        }
        '''}, [['test', '-race', './...'], ['vet', './...']])

    case('merged_native_same_package_bypass', {
        'generated.go': 'package probe\nfunc hgoChild_private() int { return 9 }\n',
        'native_test.go': '''package probe
        import "testing"
        // Native Go in the package bypasses logical hgo privacy.
        func TestNative(t *testing.T){if hgoChild_private()!=9{t.Fatal("unreachable")}}
        '''}, [['test', '-race', './...']])

    case('external_go_private_rejection', {
        'lib/lib.go': 'package lib\nfunc hgoChild_private() int { return 9 }\n',
        'consumer/consumer.go': '''package consumer
        import "example.org/probe/lib"
        var Value = lib.hgoChild_private()
        '''}, [['test', './...']], 'undefined: lib.hgoChild_private')

    case('internal_is_not_crate_privacy', {
        'lib/internal/bridge/bridge.go': 'package bridge\nfunc Scoped() int{return 1}\n',
        'lib/consumer/consumer_test.go': '''package consumer
        import ("testing"; "example.org/probe/lib/internal/bridge")
        // A different hgo target under the allowed subtree can still call it.
        func TestBridge(t *testing.T){if bridge.Scoped()!=1{t.Fatal("bridge")}}
        '''}, [['test', './...']])

    case('initialization_and_shared_lib', {
        'lib/00_parent.go': '''package lib
        var Trace []string
        func mark(s string) int {Trace=append(Trace,s);return 0}
        var parent=mark("parent-var")
        func init(){mark("parent-init")}
        ''',
        'lib/10_child.go': '''package lib
        var child=mark("child-var")
        func init(){mark("child-init")}
        ''',
        'left/left.go': '''package left
        import "example.org/probe/lib"
        func Trace() []string{return lib.Trace}
        ''',
        'right/right.go': '''package right
        import "example.org/probe/lib"
        func Trace() []string{return lib.Trace}
        ''',
        'cmd/web/main.go': 'package main\nimport _ "example.org/probe/left"\nfunc main(){}\n',
        'cmd/cli/main.go': 'package main\nimport _ "example.org/probe/right"\nfunc main(){}\n',
        'examples/demo/main.go': 'package main\nimport _ "example.org/probe/left"\nfunc main(){}\n',
        'init_test.go': '''package probe
        import ("testing"; "reflect"; "example.org/probe/left"; "example.org/probe/right")
        func TestOrder(t *testing.T) {
          want:=[]string{"parent-var","child-var","parent-init","child-init"}
          a,b:=left.Trace(),right.Trace()
          if !reflect.DeepEqual(a,want)||len(b)!=4||&a[0]!=&b[0]{t.Fatalf("init: %v / %v",a,b)}
        }
        '''}, [['test', '-race', './...'], ['build', './cmd/web', './cmd/cli', './examples/demo'], ['vet', './...']])

    case('initialization_cycle_still_invalid', {
        'bad.go': 'package probe\nvar a=b+1\nvar b=a+1\n'
        }, [['test', './...']], 'initialization cycle')

    case('definition_site_binding_vs_shadow', {
        'lib/lib.go': 'package lib\nfunc Helper() int {return 7}\n',
        'consumer/consumer_test.go': '''package consumer
        import ("testing"; definitionSite "example.org/probe/lib")
        // Hand-written result of resolving a macro helper to its defining unit.
        var macroHelper = definitionSite.Helper
        func TestDefinitionBinding(t *testing.T) {
          Helper:=func()int{return 99}; _=Helper
          if macroHelper()!=7 {t.Fatal("call-site shadow captured")}
        }
        '''}, [['test', '-race', './...']])

    case('facade_variadic_multireturn_constants', {
        'origin/origin.go': 'package origin\n        const Small = 7\n        const Huge = 1 << 100\n        func Join(prefix string, values ...int) (string, int) {\n          total:=0; for _,v:=range values {total+=v}; return prefix,total\n        }\n        func Generic[T any](x T) T {return x}\n        ',
        'facade/facade.go': 'package facade\n        import "example.org/probe/origin"\n        const Small=origin.Small\n        const Huge=origin.Huge\n        func Join(prefix string, values ...int)(string,int){return origin.Join(prefix,values...)}\n        func Generic[T any](x T) T {return origin.Generic[T](x)}\n        ',
        'consumer/facade_test.go': 'package consumer\n        import ("testing"; "example.org/probe/facade")\n        var narrow int8=facade.Small\n        const cancel=facade.Huge-(1<<100)\n        func TestCalls(t *testing.T){\n          label,n:=facade.Join("ok",1,2,3)\n          if label!="ok"||n!=6||narrow!=7||cancel!=0||facade.Generic(9)!=9{t.Fatal("facade")}\n        }\n        '}, [['test', '-race', './...'], ['vet', './...']])

    case('wrapper_changes_direct_recover', {
        'origin/origin.go': 'package origin\nfunc Recover(){ _=recover() }\n',
        'facade/facade.go': 'package facade\n        import "example.org/probe/origin"\n        func Recover(){origin.Recover()}\n        ',
        'consumer/recover_test.go': 'package consumer\n        import ("testing"; "example.org/probe/origin"; "example.org/probe/facade")\n        func escaped(f func())(escaped bool){\n          defer func(){escaped=recover()!=nil}()\n          defer f()\n          panic("probe")\n        }\n        func TestRecover(t *testing.T){\n          if escaped(origin.Recover){t.Fatal("direct defer did not recover")}\n          if !escaped(facade.Recover){t.Fatal("wrapper counterexample missing")}\n        }\n        '}, [['test', '-race', './...']])

    case('generic_function_not_variable_alias', {
        'origin/origin.go': 'package origin\nfunc F[T any](x T) T{return x}\n',
        'facade/facade.go': 'package facade\n        import "example.org/probe/origin"\n        var F=origin.F\n        '}, [['test', './...']], 'cannot use generic function')

    case('canonical_internal_path_is_not_accessible_route', {
        'origin/internal/detail/detail.go': 'package detail\ntype T struct{ Value int }\n',
        'origin/api/api.go': 'package api\n        import "example.org/probe/origin/internal/detail"\n        type T = detail.T\n        ',
        'consumer/direct.go': 'package consumer\n        import "example.org/probe/origin/internal/detail"\n        var V detail.T\n        '}, [['test', './...']], 'use of internal package')

    case('internal_alias_and_function_value_bridge', {
        'origin/internal/detail/detail.go': 'package detail\n        type T struct {Value int}\n        func (t T) Get()int{return t.Value}\n        func Identity[T any](x T)T{return x}\n        func Catch(){_=recover()}\n        ',
        'origin/api/api.go': 'package api\n        import ("reflect"; "example.org/probe/origin/internal/detail")\n        type T = detail.T\n        func CatchFunction()func(){return detail.Catch}\n        func IdentityFunction[T any]()func(T)T{return detail.Identity[T]}\n        func CanonicalType()reflect.Type{return reflect.TypeOf(detail.T{})}\n        func CanonicalPC()uintptr{return reflect.ValueOf(detail.Catch).Pointer()}\n        ',
        'consumer/bridge_test.go': 'package consumer\n        import ("testing"; "reflect"; "example.org/probe/origin/api")\n        func recovered()(escaped bool){\n          defer func(){escaped=recover()!=nil}()\n          defer api.CatchFunction()()\n          panic("probe")\n        }\n        func TestBridge(t *testing.T){\n          v:=api.T{Value:8}\n          if v.Get()!=8||reflect.TypeOf(v)!=api.CanonicalType(){t.Fatal("alias identity")}\n          if recovered(){t.Fatal("getter bridge lost direct recover")}\n          if reflect.ValueOf(api.CatchFunction()).Pointer()!=api.CanonicalPC(){t.Fatal("function identity")}\n          if api.IdentityFunction[int]()(9)!=9{t.Fatal("generic getter")}\n        }\n        '}, [['test', '-race', './...'], ['vet', './...']])

    case('facade_initialization_retained', {
        'trace/trace.go': 'package trace\nvar Events []string\n',
        'origin/origin.go': 'package origin\n        import "example.org/probe/trace"\n        func init(){trace.Events=append(trace.Events,"origin")}\n        func F()int{return 1}\n        ',
        'facade/facade.go': 'package facade\n        import (_ "example.org/probe/origin"; "example.org/probe/trace")\n        func init(){trace.Events=append(trace.Events,"facade")}\n        ',
        'consumer/init_test.go': 'package consumer\n        import ("testing"; "reflect"; "example.org/probe/origin"; _ "example.org/probe/facade"; "example.org/probe/trace")\n        func TestInit(t *testing.T){\n          if origin.F()!=1||!reflect.DeepEqual(trace.Events,[]string{"origin","facade"}){t.Fatalf("%v",trace.Events)}\n        }\n        '}, [['test', '-race', './...']])

    case('facade_initialization_lost_when_import_dropped', {
        'trace/trace.go': 'package trace\nvar Events []string\n',
        'origin/origin.go': 'package origin\n        import "example.org/probe/trace"\n        func init(){trace.Events=append(trace.Events,"origin")}\n        func F()int{return 1}\n        ',
        'facade/facade.go': 'package facade\n        import (_ "example.org/probe/origin"; "example.org/probe/trace")\n        func init(){trace.Events=append(trace.Events,"facade")}\n        ',
        'consumer/init_test.go': 'package consumer\n        import ("testing"; "reflect"; "example.org/probe/origin"; "example.org/probe/trace")\n        func TestLost(t *testing.T){\n          if origin.F()!=1||!reflect.DeepEqual(trace.Events,[]string{"origin"}){t.Fatalf("%v",trace.Events)}\n        }\n        '}, [['test', '-race', './consumer']])

    case('generic_alias_go123_observed_acceptance', {
        'go.mod': 'module example.org/probe\n\ngo 1.23\n',
        'alias.go': 'package probe\ntype Box[T any] struct{ V T }\ntype Alias[T any] = Box[T]\n'
        }, [['test', './...']])

    case('generic_alias_go122_language_rejection', {
        'go.mod': 'module example.org/probe\n\ngo 1.22\n',
        'alias.go': 'package probe\ntype Box[T any] struct{ V T }\ntype Alias[T any] = Box[T]\n'
        }, [['test', './...']], 'generic type alias requires go1.23 or later')

    case('hidden_signature_direct_call_inference', {
        'origin/origin.go': 'package origin\n        type hidden struct{Value int}\n        func New()hidden{return hidden{7}}\n        func F(x hidden)int{return x.Value}\n        ',
        'consumer/direct_test.go': 'package consumer\n        import ("testing"; "example.org/probe/origin")\n        func TestDirect(t *testing.T){if origin.F(origin.New())!=7{t.Fatal("inferred hidden type")}}\n        '}, [['test', '-race', './...']])

    case('hidden_signature_getter_rejected', {
        'origin/origin.go': 'package origin\n        type hidden struct{Value int}\n        func New()hidden{return hidden{7}}\n        func F(x hidden)int{return x.Value}\n        ',
        'facade/facade.go': 'package facade\n        import "example.org/probe/origin"\n        func GetF()func(origin.hidden)int{return origin.F}\n        '}, [['test', './...']], 'name hidden not exported')

    case('private_constraint_cannot_be_textually_copied', {
        'origin/origin.go': 'package origin\n        type T struct{}\n        func(T) hidden(){}\n        func F[V interface{hidden()}](v V)V{return v}\n        ',
        'facade/facade.go': 'package facade\n        import "example.org/probe/origin"\n        func GetF[V interface{hidden()}]()func(V)V{return origin.F[V]}\n        '}, [['test', './...']], 'does not satisfy')

    case('source_line_diagnostic', {
        'lowered.go': '''package probe
//line src/net/client.hgo:41
func Broken() int {return "wrong"}
        '''}, [['test', './...']], 'src/net/client.hgo:41')

report = {'scope': 'hand-written Go mechanism probes; no hgo syntax implemented',
          'toolchain': version, 'cases': results}
if args.report:
    args.report.parent.mkdir(parents=True, exist_ok=True)
    args.report.write_text(json.dumps(report, indent=2) + '\n')
print(f'{len(results)} mechanism probes passed; {version}')
