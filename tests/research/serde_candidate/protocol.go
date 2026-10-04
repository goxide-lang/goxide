// Package serde defines format-independent serialization protocols.
package serde

type WriteValue func(Serializer) error
type ReadValue func(Deserializer) error

type Serializer interface {
	Bool(bool) error
	Int64(int64) error
	Uint64(uint64) error
	Float64(float64) error
	String(string) error
	Bytes([]byte) error
	None() error
	Some(WriteValue) error
	Seq(int) (SeqWriter, error)
	Map(int) (MapWriter, error)
	Struct(string, int) (StructWriter, error)
	UnitVariant(string, string) error
	NewtypeVariant(string, string, WriteValue) error
}
type SeqWriter interface {
	Element(WriteValue) error
	End() error
}
type MapWriter interface {
	Entry(WriteValue, WriteValue) error
	End() error
}
type StructWriter interface {
	Field(string, WriteValue) error
	End() error
}

type Deserializer interface {
	Bool(Visitor) error
	Int64(Visitor) error
	Uint64(Visitor) error
	Float64(Visitor) error
	String(Visitor) error
	Bytes(Visitor) error
	Option(Visitor) error
	Seq(Visitor) error
	Map(Visitor) error
	Struct(string, []string, Visitor) error
	Enum(string, []string, Visitor) error
	Any(Visitor) error
	Skip() error
}
type Visitor interface {
	VisitBool(bool) error
	VisitInt64(int64) error
	VisitUint64(uint64) error
	VisitFloat64(float64) error
	VisitString(string) error
	VisitBytes([]byte) error
	VisitNone() error
	VisitSome(Deserializer) error
	VisitSeq(SeqAccess) error
	VisitMap(MapAccess) error
	VisitEnum(EnumAccess) error
}
type SeqAccess interface{ NextElement(ReadValue) (bool, error) }
type MapAccess interface {
	NextKey(ReadValue) (bool, error)
	NextValue(ReadValue) error
}
type EnumAccess interface {
	Variant() (string, VariantAccess, error)
}
type VariantAccess interface {
	Unit() error
	Newtype(ReadValue) error
}
