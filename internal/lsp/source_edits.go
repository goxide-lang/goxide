package lsp

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/project"
	"github.com/goxide-lang/goxide/internal/source"
)

// guardSourceEdits admits a complete source operation or rejects it. A closed
// file has no LSP version, so its bytes must still match the analyzed snapshot.
func (s *Server) guardSourceEdits(current *publishedAnalysis, edits []analysis.SourceEdit) (*sourceWorkspaceEdit, error) {
	if current == nil || s.documents == nil {
		return nil, fmt.Errorf("source edit has no current document snapshot")
	}
	groups := map[string]*sourceDocumentEdits{}
	spans := map[string][]source.ByteSpan{}
	for _, edit := range edits {
		name, index := current.names[edit.File], current.indexes[edit.File]
		if name == "" || index == nil || !current.editable[edit.File] || source.IsGenerated(name, []byte(index.Text())) {
			return nil, fmt.Errorf("source edit targets a file outside the selected authored workspace: %s", edit.File)
		}
		span := source.ByteSpan{Start: int(edit.Span.Start), End: int(edit.Span.End)}
		if span.End < span.Start || span.End > index.Len() {
			return nil, fmt.Errorf("source edit range is outside %s", edit.File)
		}
		for _, previous := range spans[edit.File] {
			if editSpansConflict(previous, span) {
				return nil, fmt.Errorf("source edit ranges overlap in %s", edit.File)
			}
		}
		spans[edit.File] = append(spans[edit.File], span)
		range_, err := index.Range(span.Start, span.End, s.encoding)
		if err != nil {
			return nil, err
		}
		uri, err := PathToURI(name)
		if err != nil {
			return nil, err
		}
		document, open, err := documentAtPath(current.documents.Documents, name)
		if err != nil {
			return nil, err
		}
		if open {
			uri = document.URI
		}
		group := groups[uri]
		if group == nil {
			group = &sourceDocumentEdits{TextDocument: versionedDocument{URI: uri}}
			if open {
				if document.Text != index.Text() {
					return nil, staleResult()
				}
				group.TextDocument.Version = &document.Version
			}
			groups[uri] = group
		}
		group.Edits = append(group.Edits, wireTextEdit{Range: range_, NewText: edit.NewText})
	}
	result := &sourceWorkspaceEdit{DocumentChanges: []sourceDocumentEdits{}}
	for _, group := range groups {
		sort.Slice(group.Edits, func(i, j int) bool {
			return positionBefore(group.Edits[i].Range.Start, group.Edits[j].Range.Start)
		})
		result.DocumentChanges = append(result.DocumentChanges, *group)
	}
	sort.Slice(result.DocumentChanges, func(i, j int) bool {
		return result.DocumentChanges[i].TextDocument.URI < result.DocumentChanges[j].TextDocument.URI
	})
	if err := s.checkEditVersions(current, result); err != nil {
		return nil, err
	}
	return result, nil
}

func closedSourceUnchanged(current *publishedAnalysis, uri string) bool {
	name, err := URIToPath(uri)
	if err != nil {
		return false
	}
	key := current.keys[filepath.Clean(name)]
	index := current.indexes[key]
	if !current.editable[key] || index == nil {
		return false
	}
	if project.NoSymlinks(name) != nil {
		return false
	}
	data, err := os.ReadFile(name)
	return err == nil && bytes.Equal(data, []byte(index.Text()))
}

func documentAtPath(documents map[string]Document, name string) (Document, bool, error) {
	var found Document
	open := false
	for uri, document := range documents {
		path, err := URIToPath(uri)
		if err != nil || filepath.Clean(path) != filepath.Clean(name) {
			continue
		}
		if open {
			return Document{}, false, fmt.Errorf("source has multiple open document URIs: %s", name)
		}
		found, open = document, true
	}
	return found, open, nil
}
