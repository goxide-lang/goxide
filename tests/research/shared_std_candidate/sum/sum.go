// Package sum provides shared value representations for hgo Option and Result.
// Shape validation does not replace compiler-generated payload validation.
package sum

// HgoSumABI identifies the representation contract, not a module release.
const HgoSumABI = 1

// Option contains a Some value or nil (None).
// The private marker carries T even when the value is None.
type Option[T any] interface{ isOption(T) }

// Some holds a value. Copying Some copies Value using ordinary Go assignment.
type Some[T any] struct{ Value T }

func (Some[T]) isOption(T) {}

// NewSome constructs Some without cloning or validating its payload.
func NewSome[T any](value T) Option[T] { return Some[T]{Value: value} }

// None constructs an absent Option. Some with a nil payload remains present.
func None[T any]() Option[T] { return nil }

// ValidateOption rejects noncanonical shapes, including case pointers and
// foreign structs that acquire the marker by embedding an Option.
func ValidateOption[T any](value Option[T]) {
	switch value.(type) {
	case nil, Some[T]:
		return
	default:
		panic("hgo: invalid enum Option")
	}
}

// Result contains exactly an Ok or Err value. A nil Result is invalid.
// Both parameters are present in the marker, including each case's unused one.
type Result[T, E any] interface{ isResult(T, E) }

// Ok holds a successful value without cloning it.
type Ok[T, E any] struct{ Value T }

// Err holds a failure payload without cloning or projecting it.
type Err[T, E any] struct{ Value E }

func (Ok[T, E]) isResult(T, E)  {}
func (Err[T, E]) isResult(T, E) {}

// NewOk constructs an Ok value.
func NewOk[T, E any](value T) Result[T, E] { return Ok[T, E]{Value: value} }

// NewErr constructs Err even when the payload is nil. Deciding whether a
// native Go error indicates success belongs to the call adapter, not this API.
func NewErr[T, E any](value E) Result[T, E] { return Err[T, E]{Value: value} }

// ValidateResult checks only the outer shape. Nested enum checks belong to
// the caller's statically registered concrete payload validator.
func ValidateResult[T, E any](value Result[T, E]) {
	switch value.(type) {
	case Ok[T, E], Err[T, E]:
		return
	default:
		panic("hgo: invalid enum Result")
	}
}
