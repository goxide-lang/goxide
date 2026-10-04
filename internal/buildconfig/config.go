// Package buildconfig defines the declarative host-generator manifest contract.
// It has no dependency on the runner or frontend.
package buildconfig

type Config struct {
	Entry          string            `json:"entry" toml:"entry"`
	Inputs         []string          `json:"inputs" toml:"inputs"`
	Env            []string          `json:"env" toml:"env"`
	Outputs        []string          `json:"outputs" toml:"outputs"`
	Dependencies   map[string]string `json:"dependencies" toml:"dependencies"`
	Tools          map[string]string `json:"tools" toml:"tools"`
	TimeoutSeconds int               `json:"timeout_seconds" toml:"timeout_seconds"`
}
