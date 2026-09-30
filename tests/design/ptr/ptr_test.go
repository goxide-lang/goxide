package rawptrcheck

import "testing"

type Ptr[T any] *T

func asPtr[T any](p *T) Ptr[T] { return Ptr[T](p) }

type AsPtr[T any] interface{ AsPtr() Ptr[T] }
type MutView[T any] struct{ target *T }

func (v MutView[T]) AsPtr() Ptr[T] { return Ptr[T](v.target) }
func nativeFill(p *int)            { *p += 1 }
func nativeIdentity(p *int) *int   { return p }
func TestRawPointerBoundary(t *testing.T) {
	n := 41
	p := asPtr(&n)
	nativeFill((*int)(p))
	if n != 42 || (*int)(p) != &n {
		t.Fatal("lost identity")
	}
	back := Ptr[int](nativeIdentity((*int)(p)))
	if back != p {
		t.Fatal("changed pointer on roundtrip")
	}
	var z Ptr[int]
	if z != nil {
		t.Fatal("raw pointer must preserve nil")
	}
	if Ptr[int]((*int)(z)) != nil {
		t.Fatal("nil conversion")
	}
}
func TestTraitWrapper(t *testing.T) {
	n := 1
	var v AsPtr[int] = MutView[int]{target: &n}
	nativeFill((*int)(v.AsPtr()))
	if n != 2 {
		t.Fatal("trait view copied target")
	}
}
