package goxideruntime

// Assignment is compiler support for an assignment whose hidden promoted
// pointer operands must be staged by Go itself. It is not a source protocol.
// Each statement owns a distinct guard and defers Catch in the source function,
// after all earlier source defers. Ordinary panics and Goexit are untouched.
type Assignment[T any] struct {
	pending bool
	result  T
	token   byte // Nonzero-sized storage gives each guard a distinct address.
}

// Fail is called only after validation and error conversion have completed.
// There are no user frames between this panic and the statement's Catch.
func (g *Assignment[T]) Fail(result T) {
	g.result = result
	g.pending = true
	panic(&g.token)
}

// Catch must itself be deferred by the source function, not called through a
// wrapper. Inactive guards do not even call recover, preserving ordinary panic,
// legacy panic(nil), and Goexit behavior.
func (g *Assignment[T]) Catch(result *T) {
	if !g.pending {
		return
	}
	if value := recover(); value != &g.token {
		panic(value)
	}
	*result = g.result
	g.pending = false
}
