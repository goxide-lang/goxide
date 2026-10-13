package lib

import "fmt"

func pair() (int, int) { return 3, 4 }

func sum(values ...int) (total int) {
	for _, value := range values {
		total += value
	}
	return
}

func iter(yield func(int) bool) {
	for i := 0; i < 4; i++ {
		if !yield(i) {
			return
		}
	}
}

func channels() int {
	input := make(chan int, 1)
	var send chan<- int = input
	var receive <-chan int = input
	go func() {
		defer close(input)
		send <- 7
	}()
	total := 0
	for {
		select {
		case value, ok := <-receive:
			if !ok {
				return total
			}
			total += value
		}
	}
}

func recovered() (value int) {
	defer func() {
		if recover() != nil {
			value = 9
		}
	}()
	panic("recover fixture")
}

func Exercise() string {
	_ = Offset + Seed
	_, _ = choose[int, string](1, "x")
	a, b := pair()
	match := a + b
	trace := 0
	func(value int) {
		defer func() { trace += 10 }()
		trace += value // closure step
	}(match)
	values := make([]int, 1, 4)
	values[0] = 2
	values = append(values, 3, 4)
	cloned := make([]int, len(values))
	copy(cloned, values)
	window := cloned[0:2:3]
	ptr := new(int)
	*ptr = sum(window...)
	arr := [...]int{0: 1, 2: 3}
	table := map[string]int{"x": *ptr}
	_, exists := table["x"]
	delete(table, "x")
	clear(table)
	total := 0
	remaining := 1
	for remaining > 0 {
		remaining--
	}
	switch {
	case remaining == 0:
	default:
		panic("condition loop")
	}
	var index, item int
	for index, item = range values {
		_, _ = index, item
	}
	for value := range iter {
		total += value
	}
	for i := range 3 {
		total += i
	}
	for range []int{1} {
		total++
	}
outer:
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			if j == 0 {
				continue
			}
			if i == 1 {
				continue outer
			}
			if i == 2 {
				break outer
			}
			total++
			break
		}
	}
	goto done
	total = -100
done:
	if n := len(values); n == 3 {
		total += n
	} else {
		total--
	}
	switch n := len(arr); n {
	case 3:
		total++
		fallthrough
	default:
		total += 2
	}
	var dynamic any = "go"
	switch value := dynamic.(type) {
	case string:
		total += len(value)
	case int, int64:
		total += 100
	case nil:
		total -= 100
	default:
		total = 0
	}
	text := dynamic.(string)
	switch dynamic.(type) {
	case string:
	default:
		panic("type switch")
	}
	queue := make(chan int, 1)
	select {
	case queue <- 2:
	default:
		total = 0
	}
	received := 0
	select {
	case received = <-queue:
	default:
		total = 0
	}
	select {
	case <-queue:
		total = 0
	default:
		total += received
	}
	method := Base.Value
	address := &Widget{Value: 1}
	flag := exists && len(table) == 0 || !exists
	bits := (5 & 3) | (8^2)&^1
	bits = bits << 1 >> 1
	bits %= 7
	bits *= 2
	bits /= 2
	bits -= 1
	complexValue := complex(2, 3)
	return fmt.Sprintf("%d/%d/%d/%d/%d/%s/%t/%d/%d", trace, total, channels(), recovered(), cap(values), text, flag, method(Base{Offset: 4})+address.Value, bits+int(real(complexValue))+int(imag(complexValue)))
}
