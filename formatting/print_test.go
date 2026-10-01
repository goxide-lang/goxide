package formatting

import (
	"errors"
	"io"
	"testing"
)

type writer struct {
	calls, n int
	err      error
}

func (w *writer) Write(p []byte) (int, error) { w.calls++; return w.n, w.err }
func TestWriteFailureContract(t *testing.T) {
	cause := errors.New("closed sink")
	for _, tc := range []struct {
		name      string
		n         int
		err, want error
	}{
		{"error", 2, cause, cause}, {"short", 2, nil, io.ErrShortWrite}, {"negative", -1, nil, io.ErrShortWrite},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := &writer{n: tc.n, err: tc.err}
			defer func() {
				v := recover()
				e, ok := v.(*WriteError)
				if !ok || !errors.Is(e, tc.want) || e.Written != tc.n || e.Wanted != 4 || w.calls != 1 {
					t.Fatalf("panic=%#v calls=%d", v, w.calls)
				}
			}()
			WriteTo(w, "text")
			t.Fatal("expected panic")
		})
	}
	w := &writer{}
	WriteTo(w, "")
	if w.calls != 0 {
		t.Fatal("empty print touched writer")
	}
	w = &writer{n: 4}
	WriteTo(w, "text")
	if w.calls != 1 {
		t.Fatal(w.calls)
	}
}
