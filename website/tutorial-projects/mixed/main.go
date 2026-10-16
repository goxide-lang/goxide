package main

import "fmt"

//go:generate goxide gen .

func main() {
	fmt.Println(Describe(1001, &Pending{}))
	fmt.Println(Describe(1002, &Paid{Value: 42}))
	fmt.Println(Describe(1003, &Shipped{Value: "TRACK-7"}))
}
