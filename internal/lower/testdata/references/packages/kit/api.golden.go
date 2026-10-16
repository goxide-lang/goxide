package kit

type Store struct{ Value int }
type Cargo[T any] interface {
	isCargo(T)
}
type Named[T any] struct {
	Item T
}

func (*Named[T]) isCargo(T) {}

type Empty[T any] struct {
}

func (*Empty[T]) isCargo(T) {}

func Read(store *Store) *int { return &store.Value }
func Edit(store *Store) *int { return &store.Value }
