package serdejson

import (
	"bytes"
	"encoding/json"
	"github.com/goxide-lang/serde"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// failCause retains an actual backend error without changing the public kind or
// message. A nil cause is intentional for checks performed by this backend.
func failCause(kind, message string, cause error) error {
	return &serde.Error{Kind: kind, Message: message, Cause: cause}
}

// Strict Unicode validation precedes Decoder, which otherwise replaces invalid surrogates.
func validate(data []byte) error {
	if len(data) > MaxBytes {
		return fail("Limit", "JSON input exceeds size limit")
	}
	if !utf8.Valid(data) {
		return fail("Syntax", "invalid UTF-8")
	}
	inside := false
	for i := 0; i < len(data); i++ {
		if data[i] == '"' {
			inside = !inside
			continue
		}
		if !inside || data[i] != '\\' {
			continue
		}
		i++
		if i >= len(data) {
			return fail("Syntax", "truncated escape")
		}
		if data[i] != 'u' {
			continue
		}
		if i+4 >= len(data) {
			return fail("Syntax", "truncated Unicode escape")
		}
		n, e := strconv.ParseUint(string(data[i+1:i+5]), 16, 16)
		if e != nil {
			return failCause("Syntax", "invalid Unicode escape", e)
		}
		i += 4
		if n >= 0xD800 && n <= 0xDBFF {
			if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
				return fail("Syntax", "unpaired high surrogate")
			}
			low, e := strconv.ParseUint(string(data[i+3:i+7]), 16, 16)
			if e != nil || low < 0xDC00 || low > 0xDFFF {
				return failCause("Syntax", "unpaired high surrogate", e)
			}
			i += 6
		} else if n >= 0xDC00 && n <= 0xDFFF {
			return fail("Syntax", "unpaired low surrogate")
		}
	}
	return nil
}

type readState struct{ err error }
type reader struct {
	state      *readState
	d          *json.Decoder
	used, done bool
	depth      int
	cached     bool
	token      json.Token
}

func (r *reader) take() (_serdeResult0 json.Token, _serdeErr error) {
	defer func() {
		if _serdeErr != nil {
			r.state.err = _serdeErr
		}
	}()
	if r.used {
		return nil, fail("State", "deserializer already consumed")
	}
	r.used = true
	if r.depth > MaxDepth {
		return nil, fail("Limit", "JSON nesting limit exceeded")
	}
	if r.cached {
		return r.token, nil
	}
	t, e := r.d.Token()
	if e != nil {
		return nil, failCause("Syntax", e.Error(), e)
	}
	return t, nil
}
func (r *reader) child(f serde.ReadValue) (err error) {
	defer func() {
		if err != nil {
			r.state.err = err
		}
	}()
	if f == nil {
		return fail("State", "nil deserialization callback")
	}
	c := &reader{state: r.state, d: r.d, depth: r.depth + 1}
	if e := f(c); e != nil {
		return e
	}
	if !c.done {
		return fail("State", "callback must consume one complete value")
	}
	return nil
}
func Decode(data []byte, f serde.ReadValue) error {
	if e := validate(data); e != nil {
		return e
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	r := &reader{state: &readState{}, d: d}
	if f == nil {
		return fail("State", "nil deserialization callback")
	}
	if e := f(r); e != nil {
		return e
	}
	if r.state.err != nil {
		return r.state.err
	}
	if !r.done {
		return fail("State", "incomplete deserialized value")
	}
	if _, e := d.Token(); e != io.EOF {
		return failCause("Syntax", "expected single top-level JSON value", e)
	}
	return nil
}
func Unmarshal[T any](data []byte, f func(serde.Deserializer) (T, error)) (v T, e error) {
	e = Decode(data, func(d serde.Deserializer) error { var err error; v, err = f(d); return err })
	return
}
func (r *reader) scalar(v serde.Visitor, kind string) (_serdeErr error) {
	defer func() {
		if _serdeErr != nil {
			r.state.err = _serdeErr
		}
	}()
	t, e := r.take()
	if e != nil {
		return e
	}
	switch kind {
	case "bool":
		b, ok := t.(bool)
		if !ok {
			return fail("Type", "expected bool")
		}
		e = v.VisitBool(b)
	case "string":
		s, ok := t.(string)
		if !ok {
			return fail("Type", "expected string")
		}
		e = v.VisitString(s)
	case "int", "uint", "float":
		n, ok := t.(json.Number)
		if !ok {
			return fail("Type", "expected number")
		}
		switch kind {
		case "int":
			x, err := strconv.ParseInt(string(n), 10, 64)
			if err != nil {
				return failCause("Range", "expected exact int64: "+string(n), err)
			}
			e = v.VisitInt64(x)
		case "uint":
			x, err := strconv.ParseUint(string(n), 10, 64)
			if err != nil {
				return failCause("Range", "expected exact uint64: "+string(n), err)
			}
			e = v.VisitUint64(x)
		case "float":
			x, err := strconv.ParseFloat(string(n), 64)
			if err != nil || math.IsInf(x, 0) || (x == 0 && nonzero(string(n))) {
				return failCause("Range", "float64 overflow or underflow", err)
			}
			e = v.VisitFloat64(x)
		}
	}
	r.done = e == nil
	return e
}
func nonzero(s string) bool {
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		s = s[:i]
	}
	return strings.ContainsAny(s, "123456789")
}
func (r *reader) Bool(v serde.Visitor) error   { return r.scalar(v, "bool") }
func (r *reader) String(v serde.Visitor) error { return r.scalar(v, "string") }
func (r *reader) Int64(v serde.Visitor) error  { return r.scalar(v, "int") }
func (r *reader) Uint64(v serde.Visitor) error { return r.scalar(v, "uint") }
func (r *reader) Float64(v serde.Visitor) (_serdeErr error) {
	defer func() {
		if _serdeErr != nil {
			r.state.err = _serdeErr
		}
	}()
	return r.scalar(v, "float")
}
func (r *reader) Option(v serde.Visitor) (_serdeErr error) {
	defer func() {
		if _serdeErr != nil {
			r.state.err = _serdeErr
		}
	}()
	t, e := r.take()
	if e != nil {
		return e
	}
	if t == nil {
		e = v.VisitNone()
	} else {
		c := &reader{state: r.state, d: r.d, depth: r.depth + 1, cached: true, token: t}
		e = v.VisitSome(c)
		if e == nil && !c.done {
			e = fail("State", "option visitor did not consume value")
		}
	}
	r.done = e == nil
	return e
}

type access struct {
	r                         *reader
	obj, pending, ended, busy bool
	fields                    bool
	n                         int
	key                       string
	seen                      map[string]bool
}

func (r *reader) collection(v serde.Visitor, obj, fields bool) (_serdeErr error) {
	defer func() {
		if _serdeErr != nil {
			r.state.err = _serdeErr
		}
	}()
	t, e := r.take()
	if e != nil {
		return e
	}
	want := json.Delim('[')
	if obj {
		want = '{'
	}
	if t != want {
		return fail("Type", "expected JSON collection")
	}
	a := &access{r: r, obj: obj, fields: fields, seen: map[string]bool{}}
	if obj {
		e = v.VisitMap(a)
	} else {
		e = v.VisitSeq(a)
	}
	if e != nil {
		return e
	}
	if a.pending {
		return fail("State", "map value not consumed")
	}
	if !a.ended {
		if r.d.More() {
			return fail("State", "visitor left collection entries unread")
		}
		if e = a.finish(); e != nil {
			return e
		}
	}
	r.done = true
	return nil
}
func (a *access) finish() (_serdeErr error) {
	defer func() {
		if _serdeErr != nil {
			a.r.state.err = _serdeErr
		}
	}()
	t, e := a.r.d.Token()
	if e != nil {
		return failCause("Syntax", e.Error(), e)
	}
	want := json.Delim(']')
	if a.obj {
		want = '}'
	}
	if t != want {
		return fail("Syntax", "invalid collection end")
	}
	a.ended = true
	return nil
}
func (a *access) NextElement(f serde.ReadValue) (_serdeResult0 bool, _serdeErr error) {
	defer func() {
		if _serdeErr != nil {
			a.r.state.err = _serdeErr
		}
	}()
	if a.busy {
		return false, fail("State", "reentrant collection access")
	}
	if a.ended {
		return false, nil
	}
	if !a.r.d.More() {
		return false, a.finish()
	}
	a.busy = true
	defer func() { a.busy = false }()
	e := serde.AtIndex(a.r.child(f), a.n)
	a.n++
	return true, e
}
func (a *access) NextKey(f serde.ReadValue) (okResult bool, errResult error) {
	defer func() {
		if errResult != nil {
			a.r.state.err = errResult
		}
	}()
	if a.pending {
		return false, fail("State", "must consume map value before next key")
	}
	if a.busy {
		return false, fail("State", "reentrant collection access")
	}
	if a.ended {
		return false, nil
	}
	if !a.r.d.More() {
		return false, a.finish()
	}
	t, e := a.r.d.Token()
	if e != nil {
		return false, failCause("Syntax", e.Error(), e)
	}
	k, ok := t.(string)
	if !ok {
		return false, fail("Type", "expected string key")
	}
	if a.seen[k] {
		return false, a.atKey(fail("Duplicate", "duplicate object key"), k)
	}
	a.seen[k] = true
	a.key = k
	a.pending = true
	c := &reader{state: a.r.state, d: a.r.d, depth: a.r.depth + 1, cached: true, token: k}
	a.busy = true
	defer func() { a.busy = false }()
	if f == nil {
		return false, fail("State", "nil key callback")
	}
	if e = f(c); e != nil {
		return false, a.atKey(e, k)
	}
	if !c.done {
		return false, fail("State", "key callback did not consume key")
	}
	return true, nil
}
func (a *access) NextValue(f serde.ReadValue) (_serdeErr error) {
	defer func() {
		if _serdeErr != nil {
			a.r.state.err = _serdeErr
		}
	}()
	if a.busy {
		return fail("State", "reentrant collection access")
	}
	a.busy = true
	defer func() { a.busy = false }()
	if !a.pending {
		return fail("State", "map value requested without key")
	}
	a.pending = false
	return a.atKey(a.r.child(f), a.key)
}
func (r *reader) Seq(v serde.Visitor) error { return r.collection(v, false, false) }
func (r *reader) Map(v serde.Visitor) error { return r.collection(v, true, false) }
func (r *reader) Struct(_ string, _ []string, v serde.Visitor) (_serdeErr error) {
	defer func() {
		if _serdeErr != nil {
			r.state.err = _serdeErr
		}
	}()
	return r.collection(v, true, true)
}
func (r *reader) Bytes(v serde.Visitor) (_serdeErr error) {
	defer func() {
		if _serdeErr != nil {
			r.state.err = _serdeErr
		}
	}()
	var b []byte
	e := r.Seq(serde.VisitorFuncs{OnSeq: func(a serde.SeqAccess) error {
		for {
			ok, e := a.NextElement(func(d serde.Deserializer) error {
				x, e := serde.ReadUint64(d)
				if e != nil {
					return e
				}
				if x > 255 {
					return fail("Range", "byte out of range")
				}
				b = append(b, byte(x))
				return nil
			})
			if e != nil {
				return e
			}
			if !ok {
				return nil
			}
		}
	}})
	if e != nil {
		return e
	}
	return v.VisitBytes(b)
}
func (r *reader) Any(v serde.Visitor) (_serdeErr error) {
	defer func() {
		if _serdeErr != nil {
			r.state.err = _serdeErr
		}
	}()
	t, e := r.take()
	if e != nil {
		return e
	}
	r.used = false
	r.cached = true
	r.token = t
	switch x := t.(type) {
	case nil:
		return r.Option(v)
	case bool:
		return r.Bool(v)
	case string:
		return r.String(v)
	case json.Number:
		if !strings.ContainsAny(string(x), ".eE") {
			if strings.HasPrefix(string(x), "-") {
				return r.Int64(v)
			}
			return r.Uint64(v)
		}
		return r.Float64(v)
	case json.Delim:
		if x == '[' {
			return r.Seq(v)
		}
		if x == '{' {
			return r.Map(v)
		}
	}
	return fail("Type", "unsupported token")
}
func (r *reader) Skip() (_serdeErr error) {
	defer func() {
		if _serdeErr != nil {
			r.state.err = _serdeErr
		}
	}()
	t, e := r.take()
	if e != nil {
		return e
	}
	switch t {
	case json.Delim('['), json.Delim('{'):
		r.used = false
		r.cached = true
		r.token = t
		v := serde.VisitorFuncs{OnSeq: func(a serde.SeqAccess) error {
			for {
				ok, e := a.NextElement(func(d serde.Deserializer) error { return d.Skip() })
				if e != nil || !ok {
					return e
				}
			}
		}, OnMap: func(a serde.MapAccess) error {
			for {
				ok, e := a.NextKey(func(d serde.Deserializer) error { return d.Skip() })
				if e != nil || !ok {
					return e
				}
				if e = a.NextValue(func(d serde.Deserializer) error { return d.Skip() }); e != nil {
					return e
				}
			}
		}}
		if t == json.Delim('[') {
			return r.Seq(v)
		}
		return r.Map(v)
	}
	r.done = true
	return nil
}

type enumAccess struct {
	r                *reader
	name             string
	unit, used, done bool
}

func (r *reader) Enum(_ string, variants []string, v serde.Visitor) (_serdeErr error) {
	defer func() {
		if _serdeErr != nil {
			r.state.err = _serdeErr
		}
	}()
	t, e := r.take()
	if e != nil {
		return e
	}
	a := &enumAccess{r: r}
	switch x := t.(type) {
	case string:
		a.name = x
		a.unit = true
	case json.Delim:
		if x != '{' || !r.d.More() {
			return fail("Type", "expected externally tagged enum")
		}
		key, e := r.d.Token()
		if e != nil {
			return failCause("Syntax", e.Error(), e)
		}
		var ok bool
		a.name, ok = key.(string)
		if !ok {
			return fail("Type", "invalid variant name")
		}
	default:
		return fail("Type", "expected enum string or object")
	}
	known := false
	for _, n := range variants {
		if n == a.name {
			known = true
		}
	}
	if !known {
		return fail("UnknownVariant", "unknown enum variant: "+a.name)
	}
	if e = v.VisitEnum(a); e != nil {
		return e
	}
	if !a.done {
		return fail("State", "enum payload not consumed")
	}
	if !a.unit {
		if r.d.More() {
			return fail("Type", "enum object must have exactly one key")
		}
		t, e = r.d.Token()
		if e != nil || t != json.Delim('}') {
			return failCause("Syntax", "invalid enum end", e)
		}
	}
	r.done = true
	return nil
}
func (a *enumAccess) Variant() (_serdeResult0 string, _serdeResult1 serde.VariantAccess, _serdeErr error) {
	defer func() {
		if _serdeErr != nil {
			a.r.state.err = _serdeErr
		}
	}()
	if a.used {
		return "", nil, fail("State", "variant already requested")
	}
	a.used = true
	return a.name, a, nil
}
func (a *enumAccess) Unit() (_serdeErr error) {
	defer func() {
		if _serdeErr != nil {
			a.r.state.err = _serdeErr
		}
	}()
	if a.done || !a.unit {
		return fail("Type", "expected unit variant string")
	}
	a.done = true
	return nil
}
func (a *enumAccess) Newtype(f serde.ReadValue) (_serdeErr error) {
	defer func() {
		if _serdeErr != nil {
			a.r.state.err = _serdeErr
		}
	}()
	if a.done || a.unit {
		return fail("Type", "expected newtype variant object")
	}
	if e := a.r.child(f); e != nil {
		return serde.AtVariant(e, a.name)
	}
	a.done = true
	return nil
}

func (a *access) atKey(err error, key string) error {
	if a.fields {
		return serde.AtField(err, key)
	}
	return serde.AtMapKey(err, key)
}
