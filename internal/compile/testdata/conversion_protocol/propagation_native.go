package native

type OtherFault struct{ Code int }

func (f *OtherFault) Error() string { return "other fault" }

func Read(value *Fault) (int, error) {
	if value == nil {
		return 7, nil
	}
	return 0, value
}

func ReadTypedNil() (int, error) {
	var pointer *Fault
	return 0, pointer
}
