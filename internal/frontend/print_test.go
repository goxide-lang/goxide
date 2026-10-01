package frontend

import (
	"errors"
	"github.com/hxsf/goxide/internal/sourcemap"
	"github.com/hxsf/goxide/internal/typecheck"
	"strings"
	"testing"
)

func TestPrintFrontend(t *testing.T) {
	src := []byte(`package main
#[derive(Debug)]
type Item struct{N int}
macro_rules! say {($x:expr)=>{println!("value={:?}",$x)};}
func main(){ print!("{{{} }} {:?}",42,"hi");println!();item:=Item{7};say!(item) }
`)
	r, err := Compile("print.hgo", src)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(r.Go), "__hgo_print(") {
		t.Fatal("IR leaked")
	}
	t.Log(string(r.Go))
}
func TestPrintRejectsUnsupported(t *testing.T) {
	for _, body := range []string{`print!()`, `println!("{}",1,2)`, `println!("{name}",1)`, `println!("}")`, `s:="{}";println!(s,1)`, `type N int;println!("{}",N(1))`, `println!("{}",[]int{1})`, `defer println!("hi")`} {
		_, err := Compile("bad.hgo", []byte("package p\nfunc f(){"+body+"}\n"))
		if err == nil {
			t.Fatalf("accepted %s", body)
		}
		if !strings.Contains(err.Error(), "format:") {
			t.Fatalf("unrelated error for %s: %v", body, err)
		}
	}
}

func TestPrintNestedClosureAndQuestion(t *testing.T) {
	for _, src := range []string{
		`package p
func f(){println!("{}",func()int{println!("inner");return 1}())}
`,
		`package p
func load()Result[int,string]{return Result[int,string]::Ok(7)}
func f()Result[int,string]{println!("{}",load()?);return Result[int,string]::Ok(1)}
`,
		`package p
type Alias=int
func f(){println!("{} {:?}",Alias(7),rune(65))}
`,
	} {
		r, err := Compile("control.hgo", []byte(src))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(r.Go), "__hgo_println(") {
			t.Fatal("nested printing IR escaped")
		}
	}
}

func TestPrintPackageDebugAndOrigin(t *testing.T) {
	files := map[string][]byte{
		"model.hgo": []byte("package p\n#[derive(Debug)]\ntype Item struct{N int}\n"),
		"main.hgo":  []byte("package p\nfunc Run(){item:=Item{7};DebugItem:=false;_=DebugItem;println!(\"{:?}\",item)}\n"),
	}
	if _, err := CompilePackage("example/p", files, typecheck.NewImporter("main.hgo")); err != nil {
		t.Fatal(err)
	}
	_, err := Compile("source.hgo", []byte(`package p
macro_rules! bad {($x:expr)=>{println!("{}",$x)};}
func f(){bad!([]int{1})}
`))
	var mapped *sourcemap.Error
	if !errors.As(err, &mapped) || mapped.Origin == nil || mapped.Origin.File != "source.hgo" || mapped.Origin.Line != 3 || len(mapped.Stack) == 0 {
		t.Fatalf("missing original argument/call stack: %v", err)
	}
}
