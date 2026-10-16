package main

import "fmt"

func main() {
	fmt.Println(Describe(1001, Order{Status: Pending}))
	fmt.Println(Describe(1002, Order{Status: Paid, Amount: 42}))
	fmt.Println(Describe(1003, Order{Status: Shipped, Tracking: "TRACK-7"}))
}
