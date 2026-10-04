// Package buildconfig defines the declarative host-generator manifest contract.
// It has no dependency on the runner or frontend.
package buildconfig

type Config struct {
	Entry          string            `json:"entry"`
	Inputs         []string          `json:"inputs"`
	Env            []string          `json:"env"`
	Outputs        []string          `json:"outputs"`
	Dependencies   map[string]string `json:"dependencies"`
	Tools          map[string]string `json:"tools"`
	TimeoutSeconds int               `json:"timeout_seconds"`
}
