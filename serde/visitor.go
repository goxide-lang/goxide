package serde

// VisitorFuncs implements a visitor with opt-in callbacks. Unsupported events
// fail with Type; there are no implicit numeric coercions or null defaults.
type VisitorFuncs struct {
	OnBool    func(bool) error
	OnInt64   func(int64) error
	OnUint64  func(uint64) error
	OnFloat64 func(float64) error
	OnString  func(string) error
	OnBytes   func([]byte) error
	OnSome    func(Deserializer) error
	OnSeq     func(SeqAccess) error
	OnMap     func(MapAccess) error
	OnEnum    func(EnumAccess) error
	OnNone    func() error
}

var _ Visitor = VisitorFuncs{}

func (v VisitorFuncs) VisitBool(value bool) error {
	if v.OnBool == nil {
		return NewError(Type, "unexpected bool")
	}
	return v.OnBool(value)
}
func (v VisitorFuncs) VisitInt64(value int64) error {
	if v.OnInt64 == nil {
		return NewError(Type, "unexpected int64")
	}
	return v.OnInt64(value)
}
func (v VisitorFuncs) VisitUint64(value uint64) error {
	if v.OnUint64 == nil {
		return NewError(Type, "unexpected uint64")
	}
	return v.OnUint64(value)
}
func (v VisitorFuncs) VisitFloat64(value float64) error {
	if v.OnFloat64 == nil {
		return NewError(Type, "unexpected float64")
	}
	return v.OnFloat64(value)
}
func (v VisitorFuncs) VisitString(value string) error {
	if v.OnString == nil {
		return NewError(Type, "unexpected string")
	}
	return v.OnString(value)
}
func (v VisitorFuncs) VisitBytes(value []byte) error {
	if v.OnBytes == nil {
		return NewError(Type, "unexpected bytes")
	}
	return v.OnBytes(value)
}
func (v VisitorFuncs) VisitSome(value Deserializer) error {
	if v.OnSome == nil {
		return NewError(Type, "unexpected some")
	}
	return v.OnSome(value)
}
func (v VisitorFuncs) VisitSeq(value SeqAccess) error {
	if v.OnSeq == nil {
		return NewError(Type, "unexpected seq")
	}
	return v.OnSeq(value)
}
func (v VisitorFuncs) VisitMap(value MapAccess) error {
	if v.OnMap == nil {
		return NewError(Type, "unexpected map")
	}
	return v.OnMap(value)
}
func (v VisitorFuncs) VisitEnum(value EnumAccess) error {
	if v.OnEnum == nil {
		return NewError(Type, "unexpected enum")
	}
	return v.OnEnum(value)
}
func (v VisitorFuncs) VisitNone() error {
	if v.OnNone == nil {
		return NewError(Type, "unexpected null")
	}
	return v.OnNone()
}
