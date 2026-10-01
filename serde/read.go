package serde

// Scalar readers return zero values on error. Deserializers must invoke exactly
// one appropriate visitor event; even a faulty implementation that calls no
// visitor or repeats its callback is rejected as a protocol State error.
func ReadBool(d Deserializer) (bool, error) {
	var value bool
	count := 0
	err := d.Bool(VisitorFuncs{OnBool: func(v bool) error {
		count++
		if count != 1 {
			return NewError(State, "scalar visitor called more than once")
		}
		value = v
		return nil
	}})
	if err == nil && count != 1 {
		err = NewError(State, "scalar visitor must be called exactly once")
	}
	if err != nil {
		var zero bool
		return zero, err
	}
	return value, nil
}
func ReadInt64(d Deserializer) (int64, error) {
	var value int64
	count := 0
	err := d.Int64(VisitorFuncs{OnInt64: func(v int64) error {
		count++
		if count != 1 {
			return NewError(State, "scalar visitor called more than once")
		}
		value = v
		return nil
	}})
	if err == nil && count != 1 {
		err = NewError(State, "scalar visitor must be called exactly once")
	}
	if err != nil {
		var zero int64
		return zero, err
	}
	return value, nil
}
func ReadUint64(d Deserializer) (uint64, error) {
	var value uint64
	count := 0
	err := d.Uint64(VisitorFuncs{OnUint64: func(v uint64) error {
		count++
		if count != 1 {
			return NewError(State, "scalar visitor called more than once")
		}
		value = v
		return nil
	}})
	if err == nil && count != 1 {
		err = NewError(State, "scalar visitor must be called exactly once")
	}
	if err != nil {
		var zero uint64
		return zero, err
	}
	return value, nil
}
func ReadFloat64(d Deserializer) (float64, error) {
	var value float64
	count := 0
	err := d.Float64(VisitorFuncs{OnFloat64: func(v float64) error {
		count++
		if count != 1 {
			return NewError(State, "scalar visitor called more than once")
		}
		value = v
		return nil
	}})
	if err == nil && count != 1 {
		err = NewError(State, "scalar visitor must be called exactly once")
	}
	if err != nil {
		var zero float64
		return zero, err
	}
	return value, nil
}
func ReadString(d Deserializer) (string, error) {
	var value string
	count := 0
	err := d.String(VisitorFuncs{OnString: func(v string) error {
		count++
		if count != 1 {
			return NewError(State, "scalar visitor called more than once")
		}
		value = v
		return nil
	}})
	if err == nil && count != 1 {
		err = NewError(State, "scalar visitor must be called exactly once")
	}
	if err != nil {
		var zero string
		return zero, err
	}
	return value, nil
}
func ReadBytes(d Deserializer) ([]byte, error) {
	var value []byte
	count := 0
	err := d.Bytes(VisitorFuncs{OnBytes: func(v []byte) error {
		count++
		if count != 1 {
			return NewError(State, "scalar visitor called more than once")
		}
		value = append([]byte{}, v...)
		return nil
	}})
	if err == nil && count != 1 {
		err = NewError(State, "scalar visitor must be called exactly once")
	}
	if err != nil {
		var zero []byte
		return zero, err
	}
	return value, nil
}
