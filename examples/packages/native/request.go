package native

import "strings"

func RequestID(value string) string { return "request-" + strings.TrimSpace(value) }
