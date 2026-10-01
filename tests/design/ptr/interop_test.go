package rawptrcheck

import (
	"bytes"
	"flag"
	"io"
	"sort"
	"sync/atomic"
	"testing"
)

func TestNativePointerInputsAndReturns(t *testing.T) {
	var n int64
	p := Ptr[int64](&n)
	atomic.StoreInt64((*int64)(p), 7)
	if atomic.LoadInt64((*int64)(p)) != 7 {
		t.Fatal(n)
	}
	fs := flag.NewFlagSet("probe", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	port := fs.Int("port", 8080, "")
	saved := Ptr[int](port)
	if err := fs.Parse([]string{"-port=9090"}); err != nil {
		t.Fatal(err)
	}
	if (*int)(saved) != port || *port != 9090 {
		t.Fatal("slot identity lost")
	}
}

func TestNativeContainersAndInterface(t *testing.T) {
	data := []byte("ab")
	buffer := bytes.NewBuffer(data)
	raw := Ptr[bytes.Buffer](buffer)
	// Observe the documented alias through Buffer.Bytes, not the surrendered input.
	buffer.Bytes()[0] = 'z'
	var reader io.Reader = (*bytes.Buffer)(raw)
	out := make([]byte, 2)
	if n, err := reader.Read(out); n != 2 || err != nil || string(out) != "zb" {
		t.Fatal(n, err, out)
	}
	one, two := 1, 2
	items := []*int{&two, &one}
	byName := map[string]*int{"one": &one}
	sort.Slice(items, func(i, j int) bool { return *items[i] < *items[j] })
	if items[0] != byName["one"] {
		t.Fatal("native pointer identity changed")
	}
}

func bridgeCallback(f func(Ptr[bytes.Buffer])) func(*bytes.Buffer) {
	if f == nil {
		return nil
	}
	return func(p *bytes.Buffer) { f(Ptr[bytes.Buffer](p)) }
}

func TestNativeCallbackBridge(t *testing.T) {
	if bridgeCallback(nil) != nil {
		t.Fatal("nil function changed")
	}
	var seen Ptr[bytes.Buffer]
	original := func(p Ptr[bytes.Buffer]) { seen = p }
	callback := bridgeCallback(original)
	original = func(Ptr[bytes.Buffer]) { t.Fatal("bridge captured a rebound variable") }
	_ = original
	callback(nil)
	if seen != nil {
		t.Fatal("nil argument changed")
	}
	buffer := bytes.NewBuffer(nil)
	callback(buffer)
	if (*bytes.Buffer)(seen) != buffer {
		t.Fatal("callback changed identity")
	}
	var nilBuffer *bytes.Buffer
	var reader io.Reader = nilBuffer
	if reader == nil {
		t.Fatal("typed nil interface collapsed")
	}
}

func TestHolderSlotAndInternalObject(t *testing.T) {
	type large struct{ data [1024]int }
	first := &large{}
	holder := first
	alias := holder
	slot := &holder
	replacement := &large{}
	*slot = replacement
	alias.data[0] = 7
	if holder != replacement || alias != first || holder.data[0] != 0 || first.data[0] != 7 {
		t.Fatal("holder slot confused with internal object")
	}
}
