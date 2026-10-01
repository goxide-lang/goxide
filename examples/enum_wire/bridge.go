package main

// NativeDispatch is an ordinary Go caller of the generated language API.
func NativeDispatch(data []byte, submitted *int) (Ticket, error) {
	return Process(data, submitted)
}
