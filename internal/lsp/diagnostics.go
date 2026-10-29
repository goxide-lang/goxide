package lsp

import (
	"context"
	"errors"
	"fmt"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/source"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/parser"
)

type wireDiagnostic struct {
	Range    source.Range        `json:"range"`
	Severity int                 `json:"severity"`
	Source   string              `json:"source"`
	Message  string              `json:"message"`
	Related  []relatedDiagnostic `json:"relatedInformation,omitempty"`
}
type relatedDiagnostic struct {
	Location wireLocation `json:"location"`
	Message  string       `json:"message"`
}

func (s *Server) publishFailure(epoch uint64, documents DocumentSnapshot, err error) {
	if err == nil || errors.Is(err, context.Canceled) {
		return
	}
	s.publish(&publishedAnalysis{complete: true, epoch: epoch, documents: documents, names: map[string]string{}, indexes: map[string]*source.Index{}, keys: map[string]string{}}, err)
}

func (s *Server) publish(current *publishedAnalysis, err error) {
	diagnostics := s.diagnostics(current, err)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.epoch != current.epoch || s.shutdown || s.ctx.Err() != nil {
		return
	}
	s.current = current
	if current.complete {
		s.backendMaintenance = nil
		s.maintenanceDeadline = time.Time{}
	}
	s.signalLocked()
	for uri := range s.publishedURIs {
		if _, exists := diagnostics[uri]; !exists {
			diagnostics[uri] = []wireDiagnostic{}
		}
	}
	next := map[string]bool{}
	for uri, items := range diagnostics {
		params := map[string]any{"uri": uri, "diagnostics": items}
		if document, ok := s.documents.Get(uri); ok {
			params["version"] = document.Version
		}
		if err := s.connection.Notify(s.ctx, "textDocument/publishDiagnostics", params); err != nil {
			fmt.Fprintln(s.options.Log, "LSP diagnostics:", err)
		}
		if len(items) > 0 {
			next[uri] = true
		}
	}
	s.publishedURIs = next
}

func (s *Server) diagnostics(current *publishedAnalysis, extra error) map[string][]wireDiagnostic {
	out := map[string][]wireDiagnostic{}
	seen := map[string]bool{}
	add := func(file string, span syntax.Span, message string, severity int, related []relatedDiagnostic) bool {
		location, ok := s.location(current, analysis.Location{File: file, Span: span})
		if !ok {
			return false
		}
		key := fmt.Sprintf("%s:%v:%d:%s", location.URI, location.Range, severity, message)
		if seen[key] {
			return true
		}
		seen[key] = true
		out[location.URI] = append(out[location.URI], wireDiagnostic{Range: location.Range, Severity: severity, Source: "goxide", Message: message, Related: related})
		return true
	}
	macroRelated := func(definition, call expand.Origin) []relatedDiagnostic {
		if current.result == nil {
			return nil
		}
		var related []relatedDiagnostic
		for _, entry := range []struct {
			origin  expand.Origin
			message string
		}{{definition, "macro definition"}, {call, "macro invocation"}} {
			if entry.origin.File == nil {
				continue
			}
			for key, input := range current.result.Sources {
				if input.Syntax == entry.origin.File || current.result.Files[key] == entry.origin.File {
					if location, ok := s.location(current, analysis.Location{File: key, Span: entry.origin.Span}); ok {
						related = append(related, relatedDiagnostic{location, entry.message})
					}
					break
				}
			}
		}
		return related
	}
	var report func(error)
	report = func(err error) {
		if err == nil {
			return
		}
		var a *analysis.Diagnostic
		var c check.Diagnostic
		var r resolve.Diagnostic
		var e *expand.Diagnostic
		var goErr types.Error
		switch {
		case errors.As(err, &a):
			if add(a.File, a.Span, a.Message, 1, macroRelated(a.Definition, a.Call)) {
				return
			}
		case errors.As(err, &c):
			if add(c.File, c.Span, c.Message, int(c.Severity)+1, nil) {
				return
			}
		case errors.As(err, &r):
			if add(r.File, r.Span, r.Message, int(r.Severity)+1, macroRelated(r.Definition, r.Call)) {
				return
			}
		case errors.As(err, &e):
			if current.result != nil && e.Location.File != nil {
				for key, input := range current.result.Sources {
					if input.Syntax == e.Location.File || current.result.Files[key] == e.Location.File {
						if add(key, e.Location.Span, e.Message, 1, macroRelated(e.Definition, e.Call)) {
							return
						}
					}
				}
			}
		case errors.As(err, &goErr):
			if goErr.Fset != nil {
				position := goErr.Fset.PositionFor(goErr.Pos, false)
				if mapping, ok := current.sourceMaps[position.Filename].Generated(source.ByteSpan{Start: position.Offset, End: position.Offset}); ok && mapping.Kind != source.Synthetic {
					var related []relatedDiagnostic
					for _, expansion := range mapping.Expansions {
						for _, origin := range []struct {
							location source.Location
							message  string
						}{{expansion.Definition, "macro definition"}, {expansion.Call, "macro invocation"}} {
							if location, ok := s.location(current, analysis.Location{File: origin.location.File, Span: syntaxSpan(origin.location.Span.Start, origin.location.Span.End)}); ok {
								related = append(related, relatedDiagnostic{Location: location, Message: origin.message})
							}
						}
					}
					if add(mapping.Source.File, syntaxSpan(mapping.Source.Span.Start, mapping.Source.Span.End), goErr.Msg, 1, related) {
						return
					}
				}
				if filepath.Ext(position.Filename) == ".go" && add(position.Filename, syntaxSpan(position.Offset, position.Offset), goErr.Msg, 1, nil) {
					return
				}
				if data := current.generated[position.Filename]; data != nil {
					index := source.New(string(data))
					start, positionErr := index.Position(position.Offset, s.encoding)
					if positionErr == nil {
						uri := "goxide-generated:" + position.Filename
						out[uri] = append(out[uri], wireDiagnostic{Range: source.Range{Start: start, End: start}, Severity: 1, Source: "Go", Message: goErr.Msg})
						return
					}
				}
			}
		}
		// Project/load failures have no source span. Attach them to project metadata.
		directory := s.directory
		if filepath.Ext(directory) == ".gox" || filepath.Ext(directory) == ".go" {
			directory = filepath.Dir(directory)
		}
		filename := filepath.Join(directory, "goxide.toml")
		if _, statErr := os.Stat(filename); statErr != nil {
			filename = filepath.Join(directory, "go.mod")
		}
		uri, uriErr := PathToURI(filename)
		if uriErr == nil && !seen[err.Error()] {
			seen[err.Error()] = true
			out[uri] = append(out[uri], wireDiagnostic{Severity: 1, Source: "goxide", Message: err.Error()})
		}
	}
	if result := current.result; result != nil {
		for key, input := range result.Sources {
			for _, diagnostic := range input.Diagnostics {
				severity := 1
				if diagnostic.Severity != parser.Error {
					severity = 2
				}
				add(key, diagnostic.Span, diagnostic.Message, severity, nil)
			}
		}
		for _, diagnostic := range result.Diagnostics {
			report(diagnostic)
		}
		if result.Resolved != nil {
			for _, diagnostic := range result.Resolved.Diagnostics {
				report(diagnostic)
			}
		}
		if result.Checked != nil {
			for _, diagnostic := range result.Checked.Diagnostics {
				report(diagnostic)
			}
		}
	}
	report(extra)
	for uri, items := range current.goDiagnostics {
		out[uri] = append(out[uri], items...)
	}
	for uri, items := range out {
		clean := items[:0]
		unique := map[string]bool{}
		for _, item := range items {
			item.Message = strings.TrimSpace(item.Message)
			key := fmt.Sprintf("%v:%d:%s", item.Range, item.Severity, item.Message)
			if unique[key] {
				continue
			}
			unique[key] = true
			clean = append(clean, item)
		}
		out[uri] = clean
	}
	return out
}
