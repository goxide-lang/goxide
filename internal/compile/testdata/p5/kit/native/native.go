package native

import _ "embed"

//go:embed label.txt
var label string

func Label() string { return label }
