// Package serdejson implements JSON over the format-independent serde protocols.
package serdejson

import (
	"bytes"
	"encoding/json"
	"github.com/goxide-lang/serde"
	"math"
	"strconv"
	"unicode/utf8"
)

const MaxDepth = 128
const MaxBytes = 16 << 20

func fail(kind, msg string) error { return serde.NewError(kind, msg) }

type output struct {
	bytes.Buffer
	err error
}

func (o *output) put(s string) error {
	if o.Len()+len(s) > MaxBytes {
		return fail("Limit", "JSON output exceeds size limit")
	}
	o.WriteString(s)
	return nil
}

type writer struct {
	out        *output
	used, done bool
	depth      int
}

func (w *writer) start() (_serdeErr error) {
	defer func() {
		if _serdeErr != nil {
			w.out.err = _serdeErr
		}
	}()
	if w.used {
		return fail("State", "serializer already consumed")
	}
	w.used = true
	if w.depth > MaxDepth {
		return fail("Limit", "JSON nesting limit exceeded")
	}
	return nil
}
func (w *writer) scalar(s string) (_serdeErr error) {
	defer func() {
		if _serdeErr != nil {
			w.out.err = _serdeErr
		}
	}()
	if e := w.start(); e != nil {
		return e
	}
	e := w.out.put(s)
	w.done = e == nil
	return e
}
func (w *writer) child(f serde.WriteValue) (err error) {
	defer func() {
		if err != nil {
			w.out.err = err
		}
	}()
	if f == nil {
		return fail("State", "nil serialization callback")
	}
	c := &writer{out: w.out, depth: w.depth + 1}
	if e := f(c); e != nil {
		return e
	}
	if !c.done {
		return fail("State", "callback must serialize exactly one complete value")
	}
	return nil
}
func Encode(f serde.WriteValue) ([]byte, error) {
	o := new(output)
	w := &writer{out: o}
	if f == nil {
		return nil, fail("State", "nil serialization callback")
	}
	if e := f(w); e != nil {
		return nil, e
	}
	if o.err != nil {
		return nil, o.err
	}
	if !json.Valid(o.Bytes()) {
		return nil, fail("State", "serializer produced incomplete JSON")
	}
	if !w.done {
		return nil, fail("State", "incomplete serialized value")
	}
	return bytes.Clone(o.Bytes()), nil
}
func Marshal[T any](v T, f func(serde.Serializer, T) error) ([]byte, error) {
	return Encode(func(s serde.Serializer) error { return f(s, v) })
}
func (w *writer) Bool(v bool) error   { return w.scalar(strconv.FormatBool(v)) }
func (w *writer) Int64(v int64) error { return w.scalar(strconv.FormatInt(v, 10)) }
func (w *writer) Uint64(v uint64) (_serdeErr error) {
	defer func() {
		if _serdeErr != nil {
			w.out.err = _serdeErr
		}
	}()
	return w.scalar(strconv.FormatUint(v, 10))
}
func (w *writer) Float64(v float64) (_serdeErr error) {
	defer func() {
		if _serdeErr != nil {
			w.out.err = _serdeErr
		}
	}()
	if math.IsInf(v, 0) || math.IsNaN(v) {
		return fail("Range", "JSON requires finite floats")
	}
	return w.scalar(strconv.FormatFloat(v, 'g', -1, 64))
}
func quote(v string) (string, error) {
	if !utf8.ValidString(v) {
		return "", fail("Syntax", "invalid UTF-8 string")
	}
	b, e := json.Marshal(v)
	return string(b), e
}
func (w *writer) String(v string) (_serdeErr error) {
	defer func() {
		if _serdeErr != nil {
			w.out.err = _serdeErr
		}
	}()
	s, e := quote(v)
	if e != nil {
		return e
	}
	return w.scalar(s)
}
func (w *writer) None() (_serdeErr error) {
	defer func() {
		if _serdeErr != nil {
			w.out.err = _serdeErr
		}
	}()
	return w.scalar("null")
}
func (w *writer) Some(f serde.WriteValue) (_serdeErr error) {
	defer func() {
		if _serdeErr != nil {
			w.out.err = _serdeErr
		}
	}()
	if e := w.start(); e != nil {
		return e
	}
	e := w.child(f)
	w.done = e == nil
	return e
}
func (w *writer) Bytes(v []byte) (_serdeErr error) {
	defer func() {
		if _serdeErr != nil {
			w.out.err = _serdeErr
		}
	}()
	s, e := w.Seq(len(v))
	if e != nil {
		return e
	}
	for _, b := range v {
		if e = s.Element(func(s serde.Serializer) error { return s.Uint64(uint64(b)) }); e != nil {
			return e
		}
	}
	return s.End()
}

type container struct {
	w                   *writer
	size, n             int
	object, ended, busy bool
	mapKeys             bool
	variant             bool
	seen                map[string]bool
}

func (w *writer) begin(n int, obj bool) (_serdeResult0 *container, _serdeErr error) {
	defer func() {
		if _serdeErr != nil {
			w.out.err = _serdeErr
		}
	}()
	if n < -1 {
		return nil, fail("State", "invalid length hint")
	}
	if e := w.start(); e != nil {
		return nil, e
	}
	s := "["
	if obj {
		s = "{"
	}
	if e := w.out.put(s); e != nil {
		return nil, e
	}
	return &container{w: w, size: n, object: obj, seen: map[string]bool{}}, nil
}
func (w *writer) Seq(n int) (serde.SeqWriter, error) { return w.begin(n, false) }
func (w *writer) Map(n int) (serde.MapWriter, error) {
	c, e := w.begin(n, true)
	if e == nil {
		c.mapKeys = true
	}
	return c, e
}
func (w *writer) Struct(_ string, n int) (_serdeResult0 serde.StructWriter, _serdeErr error) {
	defer func() {
		if _serdeErr != nil {
			w.out.err = _serdeErr
		}
	}()
	return w.begin(n, true)
}
func (c *container) next() (_serdeErr error) {
	defer func() {
		if _serdeErr != nil {
			c.w.out.err = _serdeErr
		}
	}()
	if c.busy {
		return fail("State", "reentrant container access")
	}
	if c.ended {
		return fail("State", "container already ended")
	}
	if c.size >= 0 && c.n >= c.size {
		return fail("State", "too many container entries")
	}
	if c.n > 0 {
		if e := c.w.out.put(","); e != nil {
			return e
		}
	}
	c.n++
	return nil
}
func (c *container) Element(f serde.WriteValue) (_serdeErr error) {
	defer func() {
		if _serdeErr != nil {
			c.w.out.err = _serdeErr
		}
	}()
	if e := c.next(); e != nil {
		return e
	}
	c.busy = true
	defer func() { c.busy = false }()
	return serde.AtIndex(c.w.child(f), c.n-1)
}
func (c *container) Field(k string, f serde.WriteValue) (_serdeErr error) {
	defer func() {
		if _serdeErr != nil {
			c.w.out.err = _serdeErr
		}
	}()
	if c.seen[k] {
		return fail("Duplicate", "duplicate object key: "+k)
	}
	s, e := quote(k)
	if e != nil {
		return e
	}
	if e = c.next(); e != nil {
		return e
	}
	c.seen[k] = true
	if e = c.w.out.put(s + ":"); e != nil {
		return e
	}
	c.busy = true
	defer func() { c.busy = false }()
	err := c.w.child(f)
	if c.variant {
		return serde.AtVariant(err, k)
	}
	if c.mapKeys {
		return serde.AtMapKey(err, k)
	}
	return serde.AtField(err, k)
}
func (c *container) Entry(k, v serde.WriteValue) (_serdeErr error) {
	defer func() {
		if _serdeErr != nil {
			c.w.out.err = _serdeErr
		}
	}()
	b, e := Encode(k)
	if e != nil {
		return e
	}
	var s string
	if len(b) == 0 || b[0] != '"' {
		return fail("Type", "JSON map keys must be strings")
	}
	if e = json.Unmarshal(b, &s); e != nil {
		return e
	}
	return c.Field(s, v)
}
func (c *container) End() (_serdeErr error) {
	defer func() {
		if _serdeErr != nil {
			c.w.out.err = _serdeErr
		}
	}()
	if c.busy {
		return fail("State", "reentrant container access")
	}
	if c.ended {
		return fail("State", "container already ended")
	}
	c.ended = true
	if c.size >= 0 && c.n != c.size {
		return fail("State", "container length does not match hint")
	}
	s := "]"
	if c.object {
		s = "}"
	}
	e := c.w.out.put(s)
	c.w.done = e == nil
	return e
}
func (w *writer) UnitVariant(_, v string) (_serdeErr error) {
	defer func() {
		if _serdeErr != nil {
			w.out.err = _serdeErr
		}
	}()
	return w.String(v)
}
func (w *writer) NewtypeVariant(_, v string, f serde.WriteValue) (_serdeErr error) {
	defer func() {
		if _serdeErr != nil {
			w.out.err = _serdeErr
		}
	}()
	c, e := w.begin(1, true)
	if e == nil {
		c.variant = true
	}
	if e != nil {
		return e
	}
	if e = c.Field(v, f); e != nil {
		return e
	}
	return c.End()
}
