package serde

import (
	"errors"
	"strconv"
	"strings"
)

// Error kinds are format independent. Formats may add further kinds.
const (
	Type           = "Type"
	MissingField   = "MissingField"
	DuplicateField = "DuplicateField"
	UnknownField   = "UnknownField"
	UnknownVariant = "UnknownVariant"
	Range          = "Range"
	Unsupported    = "Unsupported"
	Custom         = "Custom"
	Syntax         = "Syntax"
	IO             = "IO"
	State          = "State"
)

// PathSegment is structured so field names containing punctuation stay unambiguous.
// Kind is Field, Index, MapKey, or Variant; Index applies only to Index segments.
type PathSegment struct {
	Kind  string
	Name  string
	Index int
}

// Error retains both a machine-readable category/path and the original cause.
// Constructors and path wrappers do not mutate an existing error.
type Error struct {
	Kind    string
	Path    []PathSegment
	Message string
	Cause   error
}

func NewError(kind, message string) *Error { return &Error{Kind: kind, Message: message} }
func (e *Error) Unwrap() error             { return e.Cause }
func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString(e.Kind)
	if len(e.Path) > 0 {
		b.WriteString(" at $")
		for _, p := range e.Path {
			switch p.Kind {
			case "Index":
				b.WriteString("[")
				b.WriteString(strconv.Itoa(p.Index))
				b.WriteString("]")
			case "Field":
				b.WriteString("[")
				b.WriteString(strconv.Quote(p.Name))
				b.WriteString("]")
			default:
				b.WriteString("[")
				b.WriteString(p.Kind)
				b.WriteString(":")
				b.WriteString(strconv.Quote(p.Name))
				b.WriteString("]")
			}
		}
	}
	if e.Message != "" {
		b.WriteString(": ")
		b.WriteString(e.Message)
	}
	// A cause's text is deliberately not duplicated: wrappers preserve its message.
	if e.Message == "" && e.Cause != nil {
		b.WriteString(": ")
		b.WriteString(e.Cause.Error())
	}
	return b.String()
}

func at(err error, segment PathSegment) error {
	if err == nil {
		return nil
	}
	wrapped := &Error{Kind: Custom, Message: err.Error(), Cause: err}
	var inner *Error
	if errors.As(err, &inner) {
		wrapped.Kind = inner.Kind
		// Retain contextual text from opaque wrappers around a typed error.
		if direct, ok := err.(*Error); ok {
			wrapped.Message = direct.Message
		}
		wrapped.Path = make([]PathSegment, 1, len(inner.Path)+1)
		wrapped.Path[0] = segment
		wrapped.Path = append(wrapped.Path, inner.Path...)
	} else {
		wrapped.Path = []PathSegment{segment}
	}
	return wrapped
}

// AtField prepends a wire field name, preserving errors.Is/errors.As causes.
func AtField(err error, name string) error { return at(err, PathSegment{Kind: "Field", Name: name}) }
func AtIndex(err error, index int) error   { return at(err, PathSegment{Kind: "Index", Index: index}) }
func AtMapKey(err error, key string) error { return at(err, PathSegment{Kind: "MapKey", Name: key}) }
func AtVariant(err error, name string) error {
	return at(err, PathSegment{Kind: "Variant", Name: name})
}
