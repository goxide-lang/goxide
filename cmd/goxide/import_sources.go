package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/goxide-lang/goxide/internal/project"
)

// chooseImportSources is import-only. A reader is injected for deterministic
// service tests; the adapter enables interaction only with real terminal I/O.
func chooseImportSources(req ImportRequest, root string, a *project.SuppliedModules, out io.Writer) error {
	selected := map[string]string{}
	parse := func(raw string) error {
		module, source, ok := strings.Cut(raw, "=")
		if !ok || (source != "local" && source != "remote") {
			return fmt.Errorf("--source requires MODULE=local|remote: %q", raw)
		}
		if a == nil || a.Selected[module].Path == "" {
			return fmt.Errorf("unknown supplied module %q", module)
		}
		if _, ok := selected[module]; ok {
			return fmt.Errorf("duplicate source selection for %s", module)
		}
		if source == "remote" {
			_, version := project.SuppliedRemote(a.Selected[module])
			if version == "" {
				return fmt.Errorf("remote source for %s is unconfirmed (local replacement)", module)
			}
		}
		selected[module] = source
		return nil
	}
	for _, raw := range req.Sources {
		if e := parse(raw); e != nil {
			return e
		}
	}
	accepted := map[string]bool{}
	for _, module := range req.AcceptSourceDiff {
		if accepted[module] || selected[module] != "remote" {
			return fmt.Errorf("--accept-source-diff requires a distinct explicitly remote module: %s", module)
		}
		accepted[module] = true
	}
	if a == nil {
		return nil
	}
	if req.Plan {
		for _, module := range project.Keys(selected) {
			fmt.Fprintf(out, "# requested source %s=%s (plan only; not verified or applied)\n", module, selected[module])
		}
		return nil
	}
	var reader *bufio.Reader
	if req.Input != nil {
		reader = bufio.NewReader(req.Input)
	}
	line := func() (string, error) {
		if reader == nil {
			return "", fmt.Errorf("source selection cancelled: missing terminal input")
		}
		text, e := reader.ReadString('\n')
		if e != nil {
			return "", fmt.Errorf("source selection cancelled: incomplete input (%w)", e)
		}
		return strings.TrimSpace(text), nil
	}
	if req.Interactive && len(req.Sources) == 0 && len(a.Selected) > 0 {
		fmt.Fprintln(out, "Supplied module sources (all local by default):")
		for _, choice := range project.SuppliedSourceChoices(a, req.Offline) {
			fmt.Fprintln(out, choice)
		}
		fmt.Fprintln(out, "Enter MODULE=remote overrides separated by spaces; Enter keeps all local; cancel aborts:")
		text, e := line()
		if e != nil {
			return e
		}
		if text == "cancel" {
			return fmt.Errorf("source selection cancelled")
		}
		for _, raw := range strings.Fields(text) {
			if e := parse(raw); e != nil {
				return e
			}
		}
	}
	a.Sources = map[string]project.SuppliedSource{}
	a.UpstreamDifference = "see source_choices; verified covers supplied subset only"
	// Validate the full selection before any remote I/O; confirm every changed
	// module before publishing any output. Local source never triggers a fetch.
	for _, module := range project.Keys(a.Selected) {
		if selected[module] != "remote" {
			a.Sources[module] = project.SuppliedSource{Source: "local", Status: "unknown (no upstream comparison)"}
			continue
		}
		choice, e := project.VerifySuppliedRemote(req.GoTool, root, a, module, req.Offline)
		if e != nil {
			return e
		}
		fmt.Fprintf(out, "source %s: remote %s@%s; %s (%d supplied files compared)\n", module, choice.Module, choice.Version, choice.Status, choice.Compared)
		for _, diff := range choice.ModeDifferences {
			fmt.Fprintln(out, "  zip permission difference (supplied copy preserved): "+diff)
		}
		for _, diff := range choice.Differences {
			fmt.Fprintln(out, "  "+diff)
		}
		if len(choice.Differences) > 0 {
			if !accepted[module] {
				if !req.Interactive {
					return fmt.Errorf("remote source differs for %s; inspect differences and use --accept-source-diff %s (supplied bytes remain preserved)", module, module)
				}
				fmt.Fprintf(out, "Type remote %s to confirm changed source, or anything else to cancel:\n", module)
				text, e := line()
				if e != nil {
					return e
				}
				if text != "remote "+module {
					return fmt.Errorf("source selection cancelled for %s", module)
				}
			}
			choice.Accepted = true
		}
		a.Sources[module] = choice
	}
	return nil
}
