package lsp

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/source"
)

func TestSourceEditsBindOpenAndClosedAuthoredFiles(t *testing.T) {
	s, current, _, _ := editFixture(t, source.UTF16)
	current.editable = map[string]bool{editSourceKey: true, editNativeKey: true}
	closedURI, _ := PathToURI(current.names[editNativeKey])
	if err := os.WriteFile(current.names[editNativeKey], []byte(current.indexes[editNativeKey].Text()), 0644); err != nil {
		t.Fatal(err)
	}
	if _, closed := s.documents.Close(closedURI); !closed {
		t.Fatal("fixture document was not open")
	}
	current.documents = s.documents.Snapshot()
	changes := []analysis.SourceEdit{}
	for _, key := range []string{editSourceKey, editNativeKey} {
		old := "41"
		if key == editNativeKey {
			old = "1"
		}
		span := editByteSpan(t, current.indexes[key].Text(), old)
		changes = append(changes, analysis.SourceEdit{File: key, Span: syntaxSpan(span.Start, span.End), NewText: "42"})
	}
	edits, err := s.guardSourceEdits(current, changes)
	if err != nil || len(edits.DocumentChanges) != 2 {
		t.Fatalf("source edits: %#v, %v", edits, err)
	}
	for _, group := range edits.DocumentChanges {
		key := editSourceKey
		if group.TextDocument.URI == closedURI {
			key = editNativeKey
			if group.TextDocument.Version != nil {
				t.Fatalf("closed file has a version: %+v", group)
			}
		} else if group.TextDocument.Version == nil || *group.TextDocument.Version != 7 {
			t.Fatalf("open version missing: %+v", group)
		}
		start, end, err := current.indexes[key].Offsets(group.Edits[0].Range, source.UTF16)
		if err != nil || current.indexes[key].Text()[start:end] != map[string]string{editSourceKey: "41", editNativeKey: "1"}[key] {
			t.Fatalf("UTF16 range after emoji: %+v, %v", group, err)
		}
	}
	wire, err := json.Marshal(edits)
	if err != nil || !bytes.Contains(wire, []byte(`"version":null`)) || !bytes.Contains(wire, []byte(`"version":7`)) {
		t.Fatalf("nullable document versions: %s, %v", wire, err)
	}
	if err := os.WriteFile(current.names[editNativeKey], []byte("package p\n// changed without a watcher notification\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.guardSourceEdits(current, changes); !isStaleEdit(err) {
		t.Fatalf("accepted stale closed source: %v", err)
	}
}

func TestSourceEditsPreserveEquivalentOpenDocumentURI(t *testing.T) {
	for _, alternate := range []string{"localhost", "escaped"} {
		t.Run(alternate, func(t *testing.T) {
			s, current, _, _ := editFixture(t, source.UTF16)
			current.editable = map[string]bool{editSourceKey: true}
			uri, _ := PathToURI(current.names[editSourceKey])
			s.documents.Close(uri)
			if alternate == "localhost" {
				uri = strings.Replace(uri, "file:///", "file://localhost/", 1)
			} else {
				uri = strings.Replace(uri, "main.gox", "%6dain.gox", 1)
			}
			if _, err := s.documents.Open(uri, 12, current.indexes[editSourceKey].Text()); err != nil {
				t.Fatal(err)
			}
			current.documents = s.documents.Snapshot()
			span := editByteSpan(t, current.indexes[editSourceKey].Text(), "41")
			edits, err := s.guardSourceEdits(current, []analysis.SourceEdit{{File: editSourceKey, Span: syntaxSpan(span.Start, span.End), NewText: "42"}})
			if err != nil || len(edits.DocumentChanges) != 1 {
				t.Fatalf("edit: %+v, %v", edits, err)
			}
			document := edits.DocumentChanges[0].TextDocument
			if document.URI != uri || document.Version == nil || *document.Version != 12 {
				t.Fatalf("lost client URI/version: %+v", document)
			}
		})
	}
}

func TestSourceEditsRejectClosedPathReplacedBySymlink(t *testing.T) {
	for _, component := range []string{"file", "parent"} {
		t.Run(component, func(t *testing.T) {
			s, current, _, _ := editFixture(t, source.UTF16)
			current.editable = map[string]bool{editNativeKey: true}
			name := current.names[editNativeKey]
			uri, _ := PathToURI(name)
			s.documents.Close(uri)
			current.documents = s.documents.Snapshot()
			outside := filepath.Join(t.TempDir(), filepath.Base(name))
			if err := os.WriteFile(outside, []byte(current.indexes[editNativeKey].Text()), 0644); err != nil {
				t.Fatal(err)
			}
			link, target := name, outside
			if component == "parent" {
				link, target = filepath.Dir(name), filepath.Dir(outside)
				if err := os.Rename(link, link+"-original"); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.RemoveAll(link + "-original") })
			}
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			span := editByteSpan(t, current.indexes[editNativeKey].Text(), "1")
			if _, err := s.guardSourceEdits(current, []analysis.SourceEdit{{File: editNativeKey, Span: syntaxSpan(span.Start, span.End), NewText: "2"}}); !isStaleEdit(err) {
				t.Fatalf("accepted a replaced %s symlink: %v", component, err)
			}
		})
	}
}

func TestSourceEditsRejectOutsideOwnershipAndOverlap(t *testing.T) {
	s, current, _, _ := editFixture(t, source.UTF16)
	span := editByteSpan(t, current.indexes[editSourceKey].Text(), "41")
	change := analysis.SourceEdit{File: editSourceKey, Span: syntaxSpan(span.Start, span.End), NewText: "42"}
	if _, err := s.guardSourceEdits(current, []analysis.SourceEdit{change}); err == nil {
		t.Fatal("accepted a source without authored workspace ownership")
	}
	current.editable = map[string]bool{editSourceKey: true}
	if _, err := s.guardSourceEdits(current, []analysis.SourceEdit{change, change}); err == nil {
		t.Fatal("accepted overlapping edits")
	}
	uri, _ := PathToURI(current.names[editSourceKey])
	if _, _, err := s.documents.Change(uri, 8, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.guardSourceEdits(current, []analysis.SourceEdit{change}); !isStaleEdit(err) {
		t.Fatalf("accepted a superseded document version: %v", err)
	}
}
