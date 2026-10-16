package main

import "testing"

func TestDescribe(t *testing.T) {
	cases := []struct {
		order Order
		want  string
	}{
		{&Pending{}, "order 7: pending"},
		{&Paid{Value: 42}, "order 7: paid 42"},
		{&Shipped{Value: "TRACK-7"}, "order 7: shipped TRACK-7"},
	}
	for _, test := range cases {
		if got := Describe(7, test.order); got != test.want {
			t.Fatalf("Describe() = %q, want %q", got, test.want)
		}
	}
}
