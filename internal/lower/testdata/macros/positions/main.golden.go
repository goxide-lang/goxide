package main

import fmt "fmt"

type Count = gxIntdc3bf4c5_1

func identity(value Count) gxIntdc3bf4c5_1 {
	return value
}
func Generated() gxIntdc3bf4c5_1 {
	return 9
}

type Numbers = []gxIntdc3bf4c5_1

func main() {
	saved2 := 41
	helper2 := func(gxIntdc3bf4c5_1) gxIntdc3bf4c5_1 { return -1000 }
	calls := 0
	next := func() gxIntdc3bf4c5_1 { calls++; return 1 }
	// The capture keeps the caller's saved value.
	first := func() gxIntdc3bf4c5_1 {
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

	type int string
	var typed int = "local"
	len2, cap2, copy2, append2, make2, new2 := 1, 2, 3, 4, 5, 6
	fmt.Println(first, repeated, calls, saved2, total, identity(Generated()), func() gxIntdc3bf4c5_1 {
		values := append(Numbers{7}, []gxIntdc3bf4c5_1{3}...)
		dest := make([]gxIntdc3bf4c5_1, len(values), cap(values))
		count := copy(dest, values)
		ptr := new(gxIntdc3bf4c5_1)
		*ptr = dest[0]
		return count + *ptr
	}(), helper2(0), len2+cap2+copy2+append2+make2+new2, typed)
}

type gxIntdc3bf4c5_1 = int
