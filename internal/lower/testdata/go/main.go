//go:build !goxide_excluded

// Package main exercises lowering across a module.
package main

import (
	"fixture/lib"
	"fmt"
)

func main() {
	value := lib.Widget{Value: lib.Answer(), Note: "ok"}
	fmt.Println(value.Value, value.Note, lib.Identity[int](5), lib.Exercise())
}
