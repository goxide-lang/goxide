// Package formatting supplies concrete formatter implementations and checked
// stdout writes for the finite print!/println! frontend protocol.
package formatting

import (
	"fmt"
	"io"
	"os"
	"strconv"
)

type WriteError struct {
	Written, Wanted int
	Cause           error
}

func (e *WriteError) Error() string {
	return fmt.Sprintf("goxide print: wrote %d of %d bytes: %v", e.Written, e.Wanted, e.Cause)
}
func (e *WriteError) Unwrap() error { return e.Cause }

// Write resolves stdout at execution time. Empty print performs no write.
func Write(text string) { WriteTo(os.Stdout, text) }

// WriteTo makes one write without retry or rollback, retaining the original
// error (or io.ErrShortWrite) and the observed partial byte count in its panic.
func WriteTo(w io.Writer, text string) {
	if text == "" {
		return
	}
	n, err := io.WriteString(w, text)
	if err == nil && n != len(text) {
		err = io.ErrShortWrite
	}
	if err != nil {
		panic(&WriteError{Written: n, Wanted: len(text), Cause: err})
	}
}
func DisplayString(v string) string  { return v }
func DebugString(v string) string    { return strconv.Quote(v) }
func Bool(v bool) string             { return strconv.FormatBool(v) }
func Int(v int) string               { return strconv.FormatInt(int64(v), 10) }
func Int8(v int8) string             { return strconv.FormatInt(int64(v), 10) }
func Int16(v int16) string           { return strconv.FormatInt(int64(v), 10) }
func Int32(v int32) string           { return strconv.FormatInt(int64(v), 10) }
func Int64(v int64) string           { return strconv.FormatInt(v, 10) }
func Uint(v uint) string             { return strconv.FormatUint(uint64(v), 10) }
func Uint8(v uint8) string           { return strconv.FormatUint(uint64(v), 10) }
func Uint16(v uint16) string         { return strconv.FormatUint(uint64(v), 10) }
func Uint32(v uint32) string         { return strconv.FormatUint(uint64(v), 10) }
func Uint64(v uint64) string         { return strconv.FormatUint(v, 10) }
func Uintptr(v uintptr) string       { return strconv.FormatUint(uint64(v), 10) }
func Float32(v float32) string       { return strconv.FormatFloat(float64(v), 'g', -1, 32) }
func Float64(v float64) string       { return strconv.FormatFloat(v, 'g', -1, 64) }
func Complex64(v complex64) string   { return strconv.FormatComplex(complex128(v), 'g', -1, 64) }
func Complex128(v complex128) string { return strconv.FormatComplex(v, 'g', -1, 128) }
