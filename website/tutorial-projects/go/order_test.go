package main

import "testing"

func TestDescribe(t *testing.T) {
	cases := []struct {
		order Order
		want  string
	}{
		{Order{Status: Pending}, "order 7: pending"},
		{Order{Status: Paid, Amount: 42}, "order 7: paid 42"},
		{Order{Status: Shipped, Tracking: "TRACK-7"}, "order 7: shipped TRACK-7"},
	}
	for _, test := range cases {
		if got := Describe(7, test.order); got != test.want {
			t.Fatalf("Describe() = %q, want %q", got, test.want)
		}
	}
}
