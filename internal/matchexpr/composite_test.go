package matchexpr

import (
	"go/ast"
	"go/parser"
	"go/token"

	"github.com/hxsf/goxide/internal/enums"
	"github.com/hxsf/goxide/internal/propagate"
	"github.com/hxsf/goxide/internal/testdeps"
	"github.com/hxsf/goxide/internal/typecheck"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func compileCompositeMatch(source string) ([]byte, error) {
	prepared, err := propagate.Prepare("composite.gox", []byte(source))
	if err != nil {
		return nil, err
	}
	normalized, err := Lower("composite.gox", prepared)
	if err != nil {
		return nil, err
	}
	enum, err := enums.Lower("composite.gox", normalized)
	if err != nil {
		return nil, err
	}
	imp := typecheck.NewImporter("input.gox")
	out, err := propagate.Lower("composite.gox", enum.Source, propagate.Config{Importer: imp, Families: enum.Enums})
	if err == nil {
		err = typecheck.Check("composite.gox", out, imp)
	}
	return out, err
}

func TestUnaryCompositeGeneratedRace(t *testing.T) {
	source := `package main
import "fmt"
enum Mode {Off, Count(int),}
type Word uint64
type X__goxide_match_expr_pendingY int
type Flag bool
type Words []Word
type Index map[Word]Word
type Entry struct{N Word;Bits Word;Negative int;B Flag;P *int;Values Words;Mapping Index}
func contextual(m Mode,shift uint)Entry{
 type Word = int8;_ = Word(1)
 return Entry{
  N: +(match m {Mode::Off=>1<<shift,_=>0}),
  Bits: ^(match m {Mode::Off=>0,_=>1}),
  Negative: -match m {Mode::Off=>7,_=>3},
  B: !(match m {Mode::Off=>false,_=>true}),
  P: match m {Mode::Off=>nil,_=>nil},
  Values: Words{match m {Mode::Off=>1<<shift,_=>0}},
  Mapping: Index{(match m {Mode::Off=>1<<shift,_=>0}): +(match m {Mode::Off=>2,_=>3})},
 }
}
func elision(m Mode)[]*Entry{return []*Entry{{N:match m{Mode::Off=>9,_=>0}},{Values:Words{match m{Mode::Off=>4,_=>0}}}}}
func functions(m Mode)[]func()int{return []func()int{match m{Mode::Off=>func()int{return -match m{Mode::Off=>3,_=>4}},_=>func()int{return 0}}}}
func pendingName(m Mode)[]X__goxide_match_expr_pendingY{return []X__goxide_match_expr_pendingY{match m{Mode::Off=>4,_=>0}}}
func armLiteral(m Mode)[]int{return match m{Mode::Off=>[]int{match m{Mode::Off=>3,_=>4}},_=>[]int{0}}}
func nested(m Mode)[][]Word{return [][]Word{{match m{Mode::Off=>5,_=>0}}}}
func array(m Mode)[4]Word{return [...]Word{2:match m{Mode::Off=>7,_=>0},3:8}}
func positional(m Mode)struct{X Word;Y Flag}{return struct{X Word;Y Flag}{match m{Mode::Off=>6,_=>0},!(match m{Mode::Off=>false,_=>true})}}
func peer(m Mode,n uint)Word{return +(match m{Mode::Off=>1<<n,_=>0})+Word(1)}
var trace string
func mark(s string,n int)int{trace+=s;return n}
func get(ok bool)Result[int,string]{trace+="Q";if !ok{return Result[int,string]::Err("no")};return Result[int,string]::Ok(3)}
func collect(m Mode,ok bool)Result[[]int,string]{defer func(){trace+="D"}();value:=[]int{mark("A",1),match m{Mode::Off=>get(ok)?,_=>0},mark("B",2)};return Result[[]int,string]::Ok(value)}
func mapping(m Mode)map[int]int{return map[int]int{mark("K",1):match m{Mode::Off=>mark("V",2),_=>0},(match m{Mode::Off=>mark("L",3),_=>0}):mark("W",4)}}
func short(m Mode)bool{return false && !(match m{Mode::Off=>mark("X",1)>0,_=>false})}
type Closure struct{A int;Run func()Result[[]int,string];B int}
func closure(m Mode)int{x:=Closure{A:match m{Mode::Off=>mark("F",1),_=>0},Run:func()Result[[]int,string]{defer func(){trace+="C"}();v:=[]int{match m{Mode::Off=>get(false)?,_=>0},mark("Z",2)};return Result[[]int,string]::Ok(v)},B:match m{Mode::Off=>mark("L",2),_=>0}};_=x.Run();return 8}
func iface(m Mode)interface{String()string}{return match m{Mode::Off=>nil,_=>nil}}
func recoverDirect(m Mode)(out bool){defer func(){out=(match m{Mode::Off=>recover()!=nil,_=>false})}();panic("original")}
func main(){m:=Mode::Off;v:=contextual(m,63);if v.N!=Word(1)<<63||v.Bits!=^Word(0)||v.Negative!=-7||!v.B||v.P!=nil||v.Values[0]!=v.N||v.Mapping[v.N]!=2{panic(v)};if functions(m)[0]()!=-3||pendingName(m)[0]!=4||armLiteral(m)[0]!=3||nested(m)[0][0]!=5||array(m)[2]!=7||positional(m).X!=6||!positional(m).Y||peer(m,63)!=(Word(1)<<63)+1{panic("contexts")};p:=elision(m);if p[0].N!=9||p[0]==p[1]||p[1].Values[0]!=4{panic("elision")};collect(m,true);if trace!="AQBD"{panic(trace)};trace="";collect(m,false);if trace!="AQD"{panic(trace)};trace="";x:=mapping(m);if trace!="KVLW"||x[1]!=2||x[3]!=4{panic(trace)};trace="";if short(m)||trace!=""||closure(m)!=8||trace!="FLQC"||iface(m)!=nil||!recoverDirect(m){panic("control")};fmt.Print("ok")}
`
	out, err := compileCompositeMatch(source)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	f, err := parser.ParseFile(token.NewFileSet(), "generated.go", out, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if fn, ok := n.(*ast.FuncLit); ok && len(fn.Body.List) > 0 {
			if d, ok := fn.Body.List[0].(*ast.DeclStmt); ok {
				if g, ok := d.Decl.(*ast.GenDecl); ok && len(g.Specs) > 0 {
					if v, ok := g.Specs[0].(*ast.ValueSpec); ok && len(v.Names) > 0 && strings.HasPrefix(v.Names[0].Name, "__goxide_match_expr_atom_") {
						t.Fatal("compiler-only match function survived emission")
					}
				}
			}
		}
		return true
	})
	dir := t.TempDir()
	for path, data := range map[string][]byte{"main.go": out, "go.mod": []byte("module composite\ngo 1.27\n")} {
		if err := testdeps.WriteFile(filepath.Join(dir, path), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "run", "-race", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off")
	result, err := cmd.CombinedOutput()
	if err != nil || string(result) != "ok" {
		t.Fatalf("generated runtime: %v\n%s\n%s", err, result, out)
	}
}

func TestUnaryCompositeRejectsInvalidTypesAndContexts(t *testing.T) {
	for name, body := range map[string]string{
		"overflow":           `type S struct{N uint8};func f(m Mode)S{return S{N:match m{Mode::Off=>256,_=>0}}}`,
		"wrong bool":         `func f(m Mode)bool{return !(match m{Mode::Off=>1,_=>0})}`,
		"nil scalar":         `func f(m Mode)[]int{return []int{match m{Mode::Off=>nil,_=>nil}}}`,
		"array key":          `func f(m Mode)[2]int{return [2]int{(match m{Mode::Off=>0,_=>1}):3}}`,
		"slice key":          `func f(m Mode)[]int{return []int{(match m{Mode::Off=>0,_=>1}):3}}`,
		"struct key":         `type S struct{N int};func f(m Mode)S{return S{(match m{Mode::Off=>0,_=>1}):3}}`,
		"unknown field":      `type S struct{N int};func f(m Mode)S{return S{Bad:match m{Mode::Off=>0,_=>1}}}`,
		"duplicate field":    `type S struct{N int};func f(m Mode)S{return S{N:0,N:match m{Mode::Off=>0,_=>1}}}`,
		"map key":            `func f(m Mode)map[[]int]int{return map[[]int]int{(match m{Mode::Off=>[]int{},_=>[]int{}}):1}}`,
		"incompatible named": `type A int;type B int;func f(m Mode)[]A{return []A{match m{Mode::Off=>B(1),_=>B(2)}}}`,
		"address":            `func f(m Mode)*int{return &(match m{Mode::Off=>1,_=>2})}`,
		"dereference":        `func f(m Mode,p *int)int{return *(match m{Mode::Off=>p,_=>p})}`,
		"receive":            `func f(m Mode,c chan int)int{return <-(match m{Mode::Off=>c,_=>c})}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := compileCompositeMatch("package p;enum Mode{Off,Count(int),};" + body)
			if err == nil || !strings.Contains(err.Error(), "composite.gox") {
				t.Fatalf("expected located rejection, got %v", err)
			}
		})
	}
}
