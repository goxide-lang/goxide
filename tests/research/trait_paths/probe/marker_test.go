package traitpaths

import (
	"io"
	"reflect"
	"testing"
)

type Native struct{}

func (*Native) Read([]byte) (int, error) { return 0, io.EOF }

type Helper struct{}

func (Helper) Marker() {}

type Combined struct {
	*Native
	Helper
}
type Marked interface {
	Read([]byte) (int, error)
	Marker()
}

func TestMarkerPromotion(t *testing.T) {
	_, ok := any(Combined{&Native{}, Helper{}}).(Marked)
	if ok == enhanced {
		t.Fatal("unexpected promotion result")
	}
	t.Logf("enhanced=%v assertion=%v methods=%d", enhanced, ok, reflect.TypeOf(&Native{}).NumMethod())
}
