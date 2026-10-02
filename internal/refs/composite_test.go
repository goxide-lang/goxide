package refs

import "testing"

func TestCompositeConcreteReferenceElements(t *testing.T) {
	for _, src := range []string{
		`func f(r &[ 2 ] int)int {v:=*r;v[0]++;return v[0]};func g()int{v:=[2]int{1};return f(&v)}`,
		`func f(r &[]int)int{return (*r)[0]};func g()int{v:=[]int{1};return f(&v)}`,
		`func f(r &map[string]int)int{return (*r)["x"]}`,
		`type S struct{P *int};func f(r &S,m map[*int]int)int{return m[r.P]}`,
		`type S struct{N int};func read(r &int)int{return *r};func f(r &S)int{return read(&r.N)}`,
		`type S struct{N int};func f(r &S)&int{return &r.N}`,
		`type S struct{N int};func f(r &S)int{var p &int=&r.N;return *p}`,

		`func f(r &*int)int{return **r}`,
		`type S struct{Items []int};func f(r &S)int{var items []int=r.Items;return items[0]}`,
		`type S struct{Items []int};func f(r &S)int{g:=func()int{var items=r.Items;return len(items)};return g()}`,
		`type S struct{N int;Items []int};func f(r &S)int{dst:=make([]int,len(r.Items));copy(dst,r.Items);dst[0]++;return dst[0]}`,
		`type S struct{N int};func f(r &S)int{{r:=S{};r.N++;};v:=*r;w:=mut&v;w.N++;return w.N}`,
	} {
		if _, err := Lower("composite.hgo", []byte("package p\n"+src)); err != nil {
			t.Fatalf("%s: %v", src, err)
		}
	}
}

func TestCompositeFixedPointAndSharedCopies(t *testing.T) {
	for _, src := range []string{
		`func f(r &S){a:=[]int{1};g:=func(){a[0]++};a=r.Items;g()}`,
		`func f(r &S){var items []int=r.Items;items[0]++}`,
		`func f(r &S){var items any=r.Items;items.([]int)[0]++}`,
		`func f(r &S,b bool){a:=[]int{1};for b{a[0]++;a=r.Items}}`,
		`func f(r &S){v:=*r;w:=mut&v;w.Items[0]++}`,
		`func f(r &S){m:=map[*int]int{};m[r.Pointers[0]]=1;for p:=range m{*p=2}}`,
		`func f(r &S){dst:=make([]*int,len(r.Pointers));copy(dst,r.Pointers);*dst[0]++}`,
	} {
		if _, err := Lower("composite.hgo", []byte("package p\ntype S struct{Items []int;Pointers []*int}\n"+src)); err == nil {
			t.Fatalf("accepted %s", src)
		}
	}
}

func TestCompositeRecursiveQualificationConverges(t *testing.T) {
	for _, src := range []string{
		`type P *P;func f(r &P){p:=*r;p=&p;_=p}`,
		`type Node struct{Next *Node;Items []int};func f(r &Node){p:=r.Next;for p!=nil{p=p.Next};_=p}`,
	} {
		if _, err := Lower("recursive.hgo", []byte("package p\n"+src)); err != nil {
			t.Fatal(err)
		}
	}
}
func TestCompositeHiddenBoundaryRejections(t *testing.T) {
	for _, src := range []string{
		`type S struct{P *int};func f(r &S)(p *int){p=r.P;return}`,
		`type S struct{P *int};func f(r &S){var p *int;(p)=r.P;*p=42}`,
		`type S struct{Ps []*int};func f(r &S){var p *int;for _,(p)=range r.Ps{*p=42}}`,
		`type S struct{P *int};func f(r &S){_=func()(p *int){p=r.P;return}}`,
		`type S struct{Ps []*int};var G *int;func f(r &S){for _,G=range r.Ps{}}`,
		`type S struct{Ps []*int};func f(r &S){var x any;for _,x=range r.Ps{};_=x}`,
	} {
		if _, err := Lower("escape.hgo", []byte("package p\n"+src)); err == nil {
			t.Fatal("accepted " + src)
		}
	}
}
