package frontend

import (
	"strings"
	"testing"
)

func TestReferencePropagationAndDerive(t *testing.T) {
	for name, body := range map[string]string{
		"read":    `func f(r &int)Result[int,string]{ n:=load()?;return Result[int,string]::Ok(*r+n) }`,
		"write":   `func f(w &mut  int)Result[int,string]{ n:=load()?;*w+=n;return Result[int,string]::Ok(*w) };func local()Result[int,string]{v:=1;w:=&mut  v;return f(w)}`,
		"derive":  `func f(r &int)Result[int,string]{ n:=load()?;_=DebugItem(&Item{N:*r});return Result[int,string]::Ok(n) }`,
		"defer":   `func consume(w &mut  int,n int){*w+=n};func f(w &mut  int)Result[int,string]{defer consume(w,load()?);return Result[int,string]::Ok(1)}`,
		"closure": `func f(w &mut  int)Result[int,string]{g:=func()Result[int,string]{n:=load()?;*w+=n;return Result[int,string]::Ok(*w)};return g()}`,
	} {
		t.Run(name, func(t *testing.T) {
			src := `package p
#[derive(Debug)]
type Item struct {N int}
func load()Result[int,string]{return Result[int,string]::Ok(1)}
` + body
			r, err := Compile("refs-combination.gox", []byte(src))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(r.Go), "__goxide_ref_") {
				t.Fatalf("reference IR escaped: %s", r.Go)
			}
			if len(r.References) == 0 {
				t.Fatal("missing permission checking records")
			}
		})
	}
}

func TestReferenceCompositionRejections(t *testing.T) {
	for name, body := range map[string]string{
		"readonly":        `func f(r &int)Result[int,string]{n:=load()?;*r=n;return Result[int,string]::Ok(n)}`,
		"raw":             `func raw(*int){};func f(r &int)Result[int,string]{n:=load()?;raw(r);return Result[int,string]::Ok(n)}`,
		"any":             `func f(r &int)Result[int,string]{n:=load()?;var a any=r;_=a;return Result[int,string]::Ok(n)}`,
		"promote":         `func f(r &int)Result[int,string]{n:=load()?;w:=&mut  *r;*w=n;return Result[int,string]::Ok(n)}`,
		"unrelated":       `func f(r &int)Result[int,string]{n:=load()?;return Result[int,string]::Ok(n)};func bad()int{return "bad"}`,
		"generic-phantom": `type Phantom[T any] struct{};func f(r &int)Result[int,string]{n:=load()?;var p Phantom[&int];_=p;return Result[int,string]::Ok(n)}`,
		"payload":         `func f(r &int)Option[&int]{return Option[&int]::Some(r)}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Compile("bad-refs.gox", []byte("package p\nfunc load()Result[int,string]{return Result[int,string]::Ok(1)}\n"+body))
			if err == nil {
				t.Fatal("accepted unsupported/invalid permission flow")
			}
		})
	}
}
