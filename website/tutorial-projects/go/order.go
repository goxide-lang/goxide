package main

import "fmt"

type Status int

const (
	Pending Status = iota
	Paid
	Shipped
)

type Order struct {
	Status   Status
	Amount   int
	Tracking string
}

func Describe(id int, order Order) string {
	var status string
	switch order.Status {
	case Pending:
		status = "pending"
	case Paid:
		status = fmt.Sprintf("paid %d", order.Amount)
	case Shipped:
		status = "shipped " + order.Tracking
	default:
		panic("unknown order status")
	}
	return fmt.Sprintf("order %d: %s", id, status)
}
