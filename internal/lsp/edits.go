package lsp

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/goxide-lang/goxide/internal/source"
)

func (s *Server) goCompletions(current *publishedAnalysis, key string, raw json.RawMessage) (any, error) {
	var list struct {
		IsIncomplete bool                       `json:"isIncomplete"`
		Items        []json.RawMessage          `json:"items"`
		Defaults     map[string]json.RawMessage `json:"itemDefaults"`
	}
	if err := json.Unmarshal(raw, &list.Items); err != nil {
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, err
		}
	}
	items := []any{}
	var accepted *sourceWorkspaceEdit
	answer := map[string]any{"isIncomplete": list.IsIncomplete, "items": items}
	if current == nil || current.goProjection == nil {
		return answer, nil
	}
	// The backend client does not advertise item-default support. Inherited
	// ranges or formatting must not silently bypass per-candidate validation.
	if len(list.Defaults) != 0 {
		return answer, nil
	}
	uri := current.goProjection.uris[key]
	targetURI, err := PathToURI(current.names[key])
	if uri == "" || err != nil {
		return answer, nil
	}
	for _, rawItem := range list.Items {
		var item map[string]json.RawMessage
		if err := json.Unmarshal(rawItem, &item); err != nil {
			return nil, err
		}
		var label string
		if err := json.Unmarshal(item["label"], &label); err != nil || label == "" || presentJSON(item["command"]) {
			continue
		}
		valid := true
		for _, field := range []string{"insertTextFormat", "insertTextMode"} {
			if presentJSON(item[field]) {
				var value int
				if json.Unmarshal(item[field], &value) != nil || value != 1 {
					valid = false
				}
			}
		}
		if !valid {
			continue
		}
		var edit struct {
			Range   *completionRange `json:"range"`
			Insert  *completionRange `json:"insert"`
			Replace *completionRange `json:"replace"`
			NewText *string          `json:"newText"`
		}
		if json.Unmarshal(item["textEdit"], &edit) != nil || edit.NewText == nil {
			continue
		}
		primary := edit.Range
		insertReplace := edit.Insert != nil && edit.Replace != nil && edit.Range == nil
		if insertReplace {
			if edit.Insert.Start != edit.Replace.Start || !rangeContains(edit.Replace.Range, edit.Insert.Range) || edit.Replace.Start.Line != edit.Replace.End.Line {
				continue
			}
			primary = edit.Replace
		} else if edit.Range == nil || edit.Insert != nil || edit.Replace != nil {
			continue
		}
		changes := []goEdit{{URI: uri, Edit: wireTextEdit{Range: primary.Range, NewText: *edit.NewText}}}
		var additional []json.RawMessage
		if presentJSON(item["additionalTextEdits"]) && json.Unmarshal(item["additionalTextEdits"], &additional) != nil {
			continue
		}
		for _, rawEdit := range additional {
			var next struct {
				Range   *completionRange `json:"range"`
				NewText *string          `json:"newText"`
			}
			if json.Unmarshal(rawEdit, &next) != nil || next.Range == nil || next.NewText == nil {
				valid = false
				break
			}
			changes = append(changes, goEdit{URI: uri, Edit: wireTextEdit{Range: next.Range.Range, NewText: *next.NewText}})
		}
		if !valid {
			continue
		}
		mapped, err := s.guardGoEdits(current, changes)
		if err != nil {
			if isStaleEdit(err) {
				return nil, err
			}
			continue
		}
		// CompletionItem edits always target its own document. An otherwise
		// valid cross-file mapping cannot be represented by this protocol item.
		if len(mapped.DocumentChanges) != 1 || mapped.DocumentChanges[0].TextDocument.URI != targetURI {
			continue
		}
		edits := mapped.DocumentChanges[0].Edits
		output := map[string]any{}
		for _, field := range []string{"label", "labelDetails", "kind", "tags", "detail", "documentation", "deprecated", "preselect", "sortText", "filterText", "commitCharacters"} {
			if value, ok := item[field]; ok {
				output[field] = value
			}
		}
		output["textEdit"] = edits[0]
		if insertReplace {
			changes[0].Edit.Range = edit.Insert.Range
			insertion, err := s.guardGoEdits(current, changes)
			if err != nil {
				if isStaleEdit(err) {
					return nil, err
				}
				continue
			}
			if len(insertion.DocumentChanges) != 1 || insertion.DocumentChanges[0].TextDocument.URI != targetURI {
				continue
			}
			insert := insertion.DocumentChanges[0].Edits[0].Range
			if insert.Start != edits[0].Range.Start || !rangeContains(edits[0].Range, insert) {
				continue
			}
			output["textEdit"] = map[string]any{"newText": *edit.NewText, "insert": insert, "replace": edits[0].Range}
		}
		if len(edits) > 1 {
			output["additionalTextEdits"] = edits[1:]
		}
		items = append(items, output)
		accepted = mapped
	}
	if accepted != nil {
		if err := s.checkEditVersions(current, accepted); err != nil {
			return nil, err
		}
	}
	answer["items"] = items
	return answer, nil
}

func presentJSON(raw json.RawMessage) bool { return len(raw) != 0 && string(raw) != "null" }

type completionRange struct{ source.Range }

func (r *completionRange) UnmarshalJSON(raw []byte) error {
	type position struct {
		Line      *int `json:"line"`
		Character *int `json:"character"`
	}
	var value struct {
		Start *position `json:"start"`
		End   *position `json:"end"`
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	if value.Start == nil || value.End == nil || value.Start.Line == nil || value.Start.Character == nil || value.End.Line == nil || value.End.Character == nil {
		return fmt.Errorf("completion edit range requires complete start/end positions")
	}
	r.Range = source.Range{Start: source.Position{Line: *value.Start.Line, Character: *value.Start.Character}, End: source.Position{Line: *value.End.Line, Character: *value.End.Character}}
	return nil
}

func isStaleEdit(err error) bool {
	var rpc *RPCError
	return errors.As(err, &rpc) && rpc.Code == CodeContentModified
}

type wireTextEdit struct {
	Range   source.Range `json:"range"`
	NewText string       `json:"newText"`
}

type goEdit struct {
	URI  string
	Edit wireTextEdit
}

type versionedDocument struct {
	URI     string `json:"uri"`
	Version *int   `json:"version"`
}

type sourceDocumentEdits struct {
	TextDocument versionedDocument `json:"textDocument"`
	Edits        []wireTextEdit    `json:"edits"`
}

type sourceWorkspaceEdit struct {
	DocumentChanges []sourceDocumentEdits `json:"documentChanges"`
}

type mappedGoEdit struct {
	uri     string
	version int
	span    source.ByteSpan
	edit    wireTextEdit
}

// guardGoEdits is the single admission path for backend edits. No filesystem or
// document is changed here. Every destination must still be an open, selected
// source document at its captured version; closed/external targets fail closed.
// WorkspaceEdit preserves those versions. CompletionItem has no version field,
// so it uses this same check immediately before returning its mapped ranges.
func (s *Server) guardGoEdits(current *publishedAnalysis, changes []goEdit) (*sourceWorkspaceEdit, error) {
	if current == nil || current.goProjection == nil || s.documents == nil || len(changes) == 0 {
		return nil, fmt.Errorf("backend edit has no current source projection")
	}
	mapped := make([]mappedGoEdit, 0, len(changes))
	for _, change := range changes {
		edit, err := s.mapGoEdit(current, change)
		if err != nil {
			return nil, err
		}
		for _, previous := range mapped {
			if previous.uri == edit.uri && editSpansConflict(previous.span, edit.span) {
				return nil, fmt.Errorf("backend edit ranges overlap in %s", edit.uri)
			}
		}
		mapped = append(mapped, edit)
	}
	grouped := map[string]*sourceDocumentEdits{}
	for _, edit := range mapped {
		group := grouped[edit.uri]
		if group == nil {
			group = &sourceDocumentEdits{TextDocument: versionedDocument{URI: edit.uri, Version: &edit.version}}
			grouped[edit.uri] = group
		}
		group.Edits = append(group.Edits, edit.edit)
	}
	var uris []string
	for uri := range grouped {
		uris = append(uris, uri)
	}
	sort.Strings(uris)
	result := &sourceWorkspaceEdit{DocumentChanges: make([]sourceDocumentEdits, 0, len(uris))}
	for _, uri := range uris {
		result.DocumentChanges = append(result.DocumentChanges, *grouped[uri])
	}
	if err := s.checkEditVersions(current, result); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Server) checkEditVersions(current *publishedAnalysis, edits *sourceWorkspaceEdit) error {
	// Match the notification lock order: Server.mu, then DocumentStore.mu. Both
	// are held while admitting the group, including same-text version changes.
	s.mu.Lock()
	defer s.mu.Unlock()
	s.documents.mu.RLock()
	defer s.documents.mu.RUnlock()
	if s.shutdown || s.epoch != current.epoch {
		return staleResult()
	}
	for _, change := range edits.DocumentChanges {
		uri, version := change.TextDocument.URI, change.TextDocument.Version
		captured, exists := current.documents.Documents[uri]
		latest, open := s.documents.documents[uri]
		if version == nil {
			name, pathErr := URIToPath(uri)
			_, equivalentOpen, lookupErr := documentAtPath(s.documents.documents, name)
			if exists || open || pathErr != nil || lookupErr != nil || equivalentOpen || !closedSourceUnchanged(current, uri) {
				return staleResult()
			}
			continue
		}
		if !exists || !open || captured.Version != *version || latest.Version != *version || latest.Text != captured.Text {
			return staleResult()
		}
	}
	return nil
}

func (s *Server) mapGoEdit(current *publishedAnalysis, change goEdit) (mappedGoEdit, error) {
	p := current.goProjection
	key, index := p.keys[change.URI], p.indexes[change.URI]
	if key == "" || index == nil {
		return mappedGoEdit{}, fmt.Errorf("backend edit targets an unselected Go document")
	}
	start, end, err := strictOffsets(index, change.Edit.Range, source.UTF16)
	if err != nil {
		return mappedGoEdit{}, err
	}
	location := source.Location{File: key, Span: source.ByteSpan{Start: start, End: end}}
	if filepath.Ext(key) != ".go" {
		location, err = exactGoSource(p, key, location.Span)
		if err != nil {
			return mappedGoEdit{}, err
		}
	}
	sourceIndex, name := current.indexes[location.File], current.names[location.File]
	if sourceIndex == nil || name == "" || current.result == nil || current.result.Sources[location.File] == nil {
		return mappedGoEdit{}, fmt.Errorf("backend edit has no selected original source")
	}
	uri, err := PathToURI(name)
	if err != nil {
		return mappedGoEdit{}, err
	}
	document, open := current.documents.Documents[uri]
	if !open || document.Text != sourceIndex.Text() || source.IsGenerated(name, []byte(document.Text)) {
		return mappedGoEdit{}, fmt.Errorf("backend edit target is not an open authored source document")
	}
	span := location.Span
	if span.Start < 0 || span.End < span.Start || span.End > sourceIndex.Len() ||
		sourceIndex.Text()[span.Start:span.End] != index.Text()[start:end] {
		return mappedGoEdit{}, fmt.Errorf("backend edit mapping does not preserve source bytes")
	}
	range_, err := sourceIndex.Range(span.Start, span.End, s.encoding)
	if err != nil {
		return mappedGoEdit{}, err
	}
	return mappedGoEdit{uri: uri, version: document.Version, span: span,
		edit: wireTextEdit{Range: range_, NewText: change.Edit.NewText}}, nil
}

func strictOffsets(index *source.Index, range_ source.Range, encoding source.Encoding) (int, int, error) {
	start, end, err := index.Offsets(range_, encoding)
	if err != nil {
		return 0, 0, err
	}
	roundtrip, err := index.Range(start, end, encoding)
	if err != nil || roundtrip != range_ {
		return 0, 0, fmt.Errorf("backend edit range is outside the document")
	}
	return start, end, nil
}

func exactGoSource(p *goProjection, key string, span source.ByteSpan) (source.Location, error) {
	var selected *source.Mapping
	entries := p.maps[key].Entries()
	for _, entry := range entries {
		if entry.Kind != source.Exact || !editSpanContains(entry.Generated, span) {
			continue
		}
		if selected != nil || len(entry.Expansions) != 0 || entry.Generated.End-entry.Generated.Start != entry.Source.Span.End-entry.Source.Span.Start {
			return source.Location{}, fmt.Errorf("backend edit has an ambiguous or expanded source mapping")
		}
		copy := entry
		selected = &copy
	}
	if selected == nil {
		return source.Location{}, fmt.Errorf("backend edit has no exact source mapping")
	}
	for _, entry := range entries {
		if entry.Kind != source.Exact && entry.Generated.End-entry.Generated.Start < selected.Generated.End-selected.Generated.Start &&
			editSpansConflict(entry.Generated, span) {
			return source.Location{}, fmt.Errorf("backend edit intersects synthetic or derived text")
		}
	}
	start := selected.Source.Span.Start + span.Start - selected.Generated.Start
	location := source.Location{File: selected.Source.File, Span: source.ByteSpan{Start: start, End: start + span.End - span.Start}}
	// The same source token can be emitted more than once, including in another
	// output file. Such copies need source-level replanning, never text reversal.
	occurrences := 0
	for _, mapping := range p.maps {
		for _, entry := range mapping.Entries() {
			if entry.Kind == source.Exact && entry.Source.File == location.File && editSpanContains(entry.Source.Span, location.Span) {
				occurrences++
			}
		}
	}
	if occurrences != 1 {
		return source.Location{}, fmt.Errorf("backend edit source has %d generated occurrences", occurrences)
	}
	return location, nil
}

func editSpanContains(outer, inner source.ByteSpan) bool {
	return outer.Start >= 0 && outer.End >= outer.Start && inner.Start >= outer.Start && inner.End >= inner.Start && inner.End <= outer.End
}

func editSpansConflict(left, right source.ByteSpan) bool {
	if left.Start == left.End {
		return left.Start >= right.Start && left.Start <= right.End
	}
	if right.Start == right.End {
		return right.Start >= left.Start && right.Start <= left.End
	}
	return left.Start < right.End && right.Start < left.End
}

type goDocumentSymbol struct {
	Name           string             `json:"name"`
	Detail         string             `json:"detail,omitempty"`
	Kind           int                `json:"kind"`
	Tags           []int              `json:"tags,omitempty"`
	Deprecated     bool               `json:"deprecated,omitempty"`
	Range          source.Range       `json:"range"`
	SelectionRange source.Range       `json:"selectionRange"`
	Children       []goDocumentSymbol `json:"children,omitempty"`
}

func (s *Server) goSymbols(current *publishedAnalysis, uri string, raw json.RawMessage) (any, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	if current == nil || current.goProjection == nil {
		return []any{}, nil
	}
	key := current.goProjection.keys[uri]
	name := current.names[key]
	if key == "" || name == "" {
		return []any{}, nil
	}
	targetURI, err := PathToURI(name)
	if err != nil {
		return nil, err
	}
	var mapSymbol func(goDocumentSymbol) (goDocumentSymbol, bool)
	mapSymbol = func(symbol goDocumentSymbol) (goDocumentSymbol, bool) {
		index := current.goProjection.indexes[uri]
		if index == nil || symbol.Name == "" || symbol.Kind < 1 || symbol.Kind > 26 {
			return goDocumentSymbol{}, false
		}
		if _, _, err := strictOffsets(index, symbol.Range, source.UTF16); err != nil {
			return goDocumentSymbol{}, false
		}
		if _, _, err := strictOffsets(index, symbol.SelectionRange, source.UTF16); err != nil {
			return goDocumentSymbol{}, false
		}
		whole, ok := s.mapGoLocation(current, wireLocation{URI: uri, Range: symbol.Range})
		if !ok || whole.URI != targetURI {
			return goDocumentSymbol{}, false
		}
		selection, ok := s.mapGoLocation(current, wireLocation{URI: uri, Range: symbol.SelectionRange})
		if !ok || selection.URI != targetURI || !rangeContains(whole.Range, selection.Range) {
			return goDocumentSymbol{}, false
		}
		symbol.Range, symbol.SelectionRange = whole.Range, selection.Range
		children := make([]goDocumentSymbol, 0, len(symbol.Children))
		for _, child := range symbol.Children {
			if mapped, ok := mapSymbol(child); ok && rangeContains(symbol.Range, mapped.Range) {
				children = append(children, mapped)
			}
		}
		symbol.Children = children
		return symbol, true
	}
	result := []any{}
	for _, item := range items {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(item, &fields); err != nil {
			return nil, err
		}
		if _, flat := fields["location"]; flat {
			var symbol struct {
				Name          string       `json:"name"`
				Kind          int          `json:"kind"`
				Tags          []int        `json:"tags,omitempty"`
				Deprecated    bool         `json:"deprecated,omitempty"`
				ContainerName string       `json:"containerName,omitempty"`
				Location      wireLocation `json:"location"`
			}
			if err := json.Unmarshal(item, &symbol); err != nil {
				return nil, err
			}
			if location, ok := s.mapGoLocation(current, symbol.Location); ok && location.URI == targetURI {
				symbol.Location = location
				result = append(result, symbol)
			}
			continue
		}
		var symbol goDocumentSymbol
		if err := json.Unmarshal(item, &symbol); err != nil {
			return nil, err
		}
		if mapped, ok := mapSymbol(symbol); ok {
			result = append(result, mapped)
		}
	}
	return result, nil
}

func rangeContains(outer, inner source.Range) bool {
	return positionBefore(outer.Start, inner.Start) && positionBefore(inner.Start, inner.End) && positionBefore(inner.End, outer.End)
}

func positionBefore(left, right source.Position) bool {
	return left.Line < right.Line || left.Line == right.Line && left.Character <= right.Character
}
