package main

import fmt "fmt"
import kit "macrofixture/kit"

func Generated() int {
	return kit.Output(3)
}

func main() {
	Output := func(int) int { return -1 }
	offset := 4
	fmt.Println(kit.Output(7)+offset, fmt.Sprint(8), Generated(), kit.Own(), 1+1, Output(0))
}
