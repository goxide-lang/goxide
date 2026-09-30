package invalidreceiver

type Ptr[T any] *T

func (p Ptr[T]) AsPtr() Ptr[T] { return p }
