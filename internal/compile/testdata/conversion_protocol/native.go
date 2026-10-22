package native

type Fault struct{ Message string }

func (f *Fault) Error() string {
	if f == nil {
		return "typed nil"
	}
	return f.Message
}

var Sentinel = &Fault{Message: "source identity"}
