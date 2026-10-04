package impl

var Count = 1

const Label = "shared"

type Box[T any] struct{ Value T }

func (b Box[T]) Get() T { return b.Value }

func Add(a int, values ...int) (int, int) {
	for _, value := range values {
		a += value
	}
	return a, Count
}
