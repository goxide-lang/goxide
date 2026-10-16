package main

import strings "strings"

func DefaultLabel() Label {
	calls = append(calls, "label")
	return Label{Text: strings.ToUpper("untitled")}
}
func DefaultStep() Step {
	calls = append(calls, "step")
	return Step{Number: len(calls)}
}
