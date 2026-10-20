package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/goxide-lang/goxide/internal/config"
	"github.com/spf13/cobra"
)

// env reports target selection without discovering a project or executing source.
func envCommand(out io.Writer) *cobra.Command {
	c := cliCommand("env [flags] [NAME ...]", "Show host, Go environment and Goxide compilation targets")
	var target, tool string
	var asJSON, list bool
	c.Flags().StringVar(&target, "target", "", "Goxide toolchain target: GOARCH-unknown-GOOS or GOOS/GOARCH")
	c.Flags().StringVar(&tool, "go", "go", "Go compiler executable")
	c.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	c.Flags().BoolVar(&list, "list", false, "list target predicate values and supported target combinations")
	c.RunE = func(_ *cobra.Command, names []string) error {
		captured := os.Environ()
		writeJSON := func(value any) error {
			encoder := json.NewEncoder(out)
			encoder.SetIndent("", "  ")
			return encoder.Encode(value)
		}
		if len(names) != 0 {
			if list || target != "" {
				return fmt.Errorf("named environment queries cannot be combined with --list or --target")
			}
			original, err := config.NewEnvironment(config.HostTarget(), captured)
			if err != nil {
				return err
			}
			type variable struct {
				Key         *string `json:"key"`
				KeyBase64   string  `json:"key_base64,omitempty"`
				Present     bool    `json:"present"`
				Value       *string `json:"value"`
				ValueBase64 string  `json:"value_base64,omitempty"`
			}
			values := make([]variable, 0, len(names))
			for _, name := range names {
				if name == "" || strings.ContainsAny(name, "=\x00") {
					return fmt.Errorf("invalid environment name %q", name)
				}
				value, present := original.LookupEnv(name)
				item := variable{Present: present}
				if utf8.ValidString(name) {
					item.Key = &name
				} else {
					item.KeyBase64 = base64.StdEncoding.EncodeToString([]byte(name))
				}
				if present {
					if utf8.ValidString(value) {
						item.Value = &value
					} else {
						item.ValueBase64 = base64.StdEncoding.EncodeToString([]byte(value))
					}
				}
				values = append(values, item)
				if !asJSON {
					if !present {
						fmt.Fprintf(out, "%s=<unset>\n", name)
					} else {
						fmt.Fprintf(out, "%s=%s\n", name, strconv.Quote(value))
					}
				}
			}
			if asJSON {
				return writeJSON(values)
			}
			return nil
		}
		effective, err := config.Prepare(tool, "", target, false, captured)
		if err != nil {
			return err
		}
		if list {
			values := map[string]any{}
			for _, predicate := range []string{"target_os", "target_arch", "target_platform", "target_endian", "target_pointer_width"} {
				values[predicate] = config.Values(predicate)
			}
			targets := make([]string, 0, len(effective.SupportedTargets))
			for _, supported := range effective.SupportedTargets {
				targets = append(targets, supported.String())
			}
			sort.Strings(targets)
			values["targets"] = targets
			if asJSON {
				return writeJSON(values)
			}
			keys := make([]string, 0, len(values))
			for key := range values {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				fmt.Fprintf(out, "%s=%s\n", key, strings.Join(values[key].([]string), ","))
			}
			return nil
		}
		inherited, err := config.Prepare(tool, "", "", true, captured)
		if err != nil {
			return err
		}
		values := struct {
			Host          config.Target `json:"host"`
			GoEnvironment config.Target `json:"go_environment"`
			Target        config.Target `json:"target"`
			GoTool        string        `json:"go_tool"`
			GoVersion     string        `json:"go_version"`
		}{effective.Host, inherited.Target, effective.Target, effective.GoTool, effective.GoVersion}
		if asJSON {
			return writeJSON(values)
		}
		_, err = fmt.Fprintf(out, "host=%s\ngo_environment=%s\ntarget=%s\ngo_tool=%s\ngo_version=%s\n", values.Host, values.GoEnvironment, values.Target, values.GoTool, values.GoVersion)
		return err
	}
	return c
}
