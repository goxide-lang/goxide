package main

import fmt "fmt"

type Count = int

func identity(value Count) int {
	return value
}
func Generated() int {
	return 9
}

type Numbers = []int

func main() {
	saved2 := 41
	helper2 := func(int) int { return -1000 }
	calls := 0
	next := func() int { calls++; return 1 }
	// The capture keeps the caller's saved value.
	first := func() int {
		saved3 := 1
		value := saved2 + next()
		return helper(value) + saved3
	}()
	repeated := next() + next()
	total := 0
	saved4 := saved2
	total += saved4
	total += 1
	saved5 := 2
	total += saved5
	total += 1

	type int2 string
	var typed int2 = "local"
	len2, cap2, copy2, append2, make2, new2 := 1, 2, 3, 4, 5, 6
	fmt.Println(first, repeated, calls, saved2, total, identity(Generated()), func() int {
		values := append(Numbers{7}, []int{3}...)
		dest := make([]int, len(values), cap(values))
		count := copy(dest, values)
		ptr := new(int)
		*ptr = dest[0]
		return count + *ptr
	}(), helper2(0), len2+cap2+copy2+append2+make2+new2, typed)
}
