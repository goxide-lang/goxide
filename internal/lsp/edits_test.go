package lsp

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/source"
)

const editSourceKey = "example.org/edits/main.gox"
const editNativeKey = "example.org/edits/native.go"

func editFixture(t *testing.T, encoding source.Encoding) (*Server, *publishedAnalysis, goEdit, goEdit) {
	t.Helper()
	original := "package p\r\nfunc answer() int { _ = \"😀\"; return 41 }\r\n"
	generated := "// generated view\npackage p\nfunc answer() int { _ = \"😀\"; return 41 }\n"
	native := "package p\r\nfunc native() int { _ = \"😀\"; return 1 }\r\n"
	texts := map[string]string{editSourceKey: original, editNativeKey: native}
	mapping := source.NewMap([]source.Mapping{
		{Generated: source.ByteSpan{Start: 0, End: len(generated)}, Kind: source.Synthetic},
		{Generated: editByteSpan(t, generated, "41"), Source: source.Location{File: editSourceKey, Span: editByteSpan(t, original, "41")}, Kind: source.Exact},
	})
	server, current := editState(t, encoding, texts, map[string]string{editSourceKey: generated, editNativeKey: native}, map[string]*source.Map{editSourceKey: mapping})
	return server, current, editAt(t, current, editSourceKey, "41", "42"), editAt(t, current, editNativeKey, "1", "2")
}

func editState(t *testing.T, encoding source.Encoding, originals, projected map[string]string, maps map[string]*source.Map) (*Server, *publishedAnalysis) {
	t.Helper()
	root := t.TempDir()
	server := &Server{documents: NewDocumentStore(encoding), encoding: encoding, epoch: 11}
	current := &publishedAnalysis{
		epoch: 11, names: map[string]string{}, keys: map[string]string{}, indexes: map[string]*source.Index{},
		result:       &analysis.Result{Sources: map[string]*analysis.Source{}},
		goProjection: &goProjection{keys: map[string]string{}, uris: map[string]string{}, indexes: map[string]*source.Index{}, files: map[string]string{}, maps: maps},
	}
	for key, text := range originals {
		name := filepath.Join(root, filepath.Base(key))
		uri, err := PathToURI(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := server.documents.Open(uri, 7, text); err != nil {
			t.Fatal(err)
		}
		current.names[key], current.keys[name], current.indexes[key] = name, key, source.New(text)
		current.result.Sources[key] = &analysis.Source{Content: []byte(text)}
	}
	for key, text := range projected {
		name := filepath.Base(key)
		if filepath.Ext(name) == ".gox" {
			name = strings.TrimSuffix(name, ".gox") + "_gox.go"
		}
		uri, err := PathToURI(filepath.Join(root, "private", name))
		if err != nil {
			t.Fatal(err)
		}
		current.goProjection.keys[uri], current.goProjection.uris[key] = key, uri
		current.goProjection.indexes[uri], current.goProjection.files[uri] = source.New(text), text
	}
	current.documents = server.documents.Snapshot()
	server.current = current
	return server, current
}

func editByteSpan(t *testing.T, text, fragment string) source.ByteSpan {
	t.Helper()
	start := strings.Index(text, fragment)
	if start < 0 {
		t.Fatalf("fixture lacks %q in %q", fragment, text)
	}
	return source.ByteSpan{Start: start, End: start + len(fragment)}
}

func editAt(t *testing.T, current *publishedAnalysis, key, old, replacement string) goEdit {
	t.Helper()
	uri := current.goProjection.uris[key]
	index := current.goProjection.indexes[uri]
	span := editByteSpan(t, index.Text(), old)
	range_, err := index.Range(span.Start, span.End, source.UTF16)
	if err != nil {
		t.Fatal(err)
	}
	return goEdit{URI: uri, Edit: wireTextEdit{Range: range_, NewText: replacement}}
}

func TestGoEditGuardMapsWholeVersionedGroup(t *testing.T) {
	for _, encoding := range []source.Encoding{source.UTF8, source.UTF16, source.UTF32} {
		t.Run(string(encoding), func(t *testing.T) {
			server, current, generated, native := editFixture(t, encoding)
			mapped, err := server.guardGoEdits(current, []goEdit{generated, native})
			if err != nil {
				t.Fatal(err)
			}
			if len(mapped.DocumentChanges) != 2 {
				t.Fatalf("group = %+v", mapped)
			}
			for _, change := range mapped.DocumentChanges {
				document, originalURI := current.documents.Documents[change.TextDocument.URI]
				if change.TextDocument.Version == nil || *change.TextDocument.Version != 7 || !originalURI {
					t.Fatalf("lost source URI/version: %+v", change)
				}
				start, end, err := document.Index.Offsets(change.Edits[0].Range, encoding)
				if err != nil || document.Text[start:end] != map[string]string{"42": "41", "2": "1"}[change.Edits[0].NewText] {
					t.Fatalf("mapped wrong Unicode/CRLF range: %+v, %v", change, err)
				}
				latest, _ := server.documents.Get(change.TextDocument.URI)
				if latest.Text != document.Text {
					t.Fatal("validation changed a document")
				}
			}
		})
	}
}

func TestGoEditGuardRejectsWholeGroup(t *testing.T) {
	for _, test := range []struct {
		name  string
		alter func(*Server, *publishedAnalysis, *goEdit, *goEdit)
		stale bool
	}{
		{"epoch", func(s *Server, _ *publishedAnalysis, _, _ *goEdit) { s.epoch++ }, true},
		{"same_text_version", func(s *Server, c *publishedAnalysis, _, _ *goEdit) {
			uri, _ := PathToURI(c.names[editNativeKey])
			_, _, _ = s.documents.Change(uri, 8, nil)
		}, true},
		{"closed_target", func(s *Server, c *publishedAnalysis, _, _ *goEdit) {
			uri, _ := PathToURI(c.names[editNativeKey])
			s.documents.Close(uri)
		}, true},
		{"external_target", func(_ *Server, _ *publishedAnalysis, _, native *goEdit) { native.URI = "file:///external/library.go" }, false},
		{"unknown_original", func(_ *Server, c *publishedAnalysis, _, _ *goEdit) { delete(c.result.Sources, editNativeKey) }, false},
		{"synthetic", func(_ *Server, c *publishedAnalysis, _, _ *goEdit) {
			entries := c.goProjection.maps[editSourceKey].Entries()
			entries[1].Kind = source.Synthetic
			c.goProjection.maps[editSourceKey] = source.NewMap(entries)
		}, false},
		{"derived", func(_ *Server, c *publishedAnalysis, _, _ *goEdit) {
			entries := c.goProjection.maps[editSourceKey].Entries()
			entries[1].Kind = source.Derived
			c.goProjection.maps[editSourceKey] = source.NewMap(entries)
		}, false},
		{"macro_expansion", func(_ *Server, c *publishedAnalysis, _, _ *goEdit) {
			entries := c.goProjection.maps[editSourceKey].Entries()
			entries[1].Expansions = []source.Expansion{{ID: 1}}
			c.goProjection.maps[editSourceKey] = source.NewMap(entries)
		}, false},
		{"copied_source", func(_ *Server, c *publishedAnalysis, _, _ *goEdit) {
			c.goProjection.maps["another.gox"] = c.goProjection.maps[editSourceKey]
		}, false},
		{"ambiguous_generated", func(_ *Server, c *publishedAnalysis, _, _ *goEdit) {
			entries := c.goProjection.maps[editSourceKey].Entries()
			entries = append(entries, entries[1])
			c.goProjection.maps[editSourceKey] = source.NewMap(entries)
		}, false},
		{"unequal_mapping_lengths", func(_ *Server, c *publishedAnalysis, _, _ *goEdit) {
			entries := c.goProjection.maps[editSourceKey].Entries()
			entries[1].Source.Span.End++
			c.goProjection.maps[editSourceKey] = source.NewMap(entries)
		}, false},
		{"outside_document", func(_ *Server, _ *publishedAnalysis, generated, _ *goEdit) { generated.Edit.Range.End.Line = 1000 }, false},
		{"overlapping", func(_ *Server, _ *publishedAnalysis, generated, native *goEdit) { *native = *generated }, false},
		{"same_position_insert", func(_ *Server, _ *publishedAnalysis, generated, native *goEdit) {
			generated.Edit.Range.End = generated.Edit.Range.Start
			*native = *generated
		}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			server, current, generated, native := editFixture(t, source.UTF16)
			test.alter(server, current, &generated, &native)
			mapped, err := server.guardGoEdits(current, []goEdit{generated, native})
			if err == nil || mapped != nil {
				t.Fatalf("unsafe group admitted: %+v, %v", mapped, err)
			}
			if test.stale && !isStaleEdit(err) {
				t.Fatalf("stale edit did not return ContentModified: %v", err)
			}
		})
	}
}

func TestGoCompletionsValidateAllEdits(t *testing.T) {
	server, current, generated, native := editFixture(t, source.UTF16)
	unsafeAdditional := wireTextEdit{Range: source.Range{Start: source.Position{}, End: source.Position{Character: 2}}, NewText: "unsafe"}
	items := []any{
		map[string]any{"label": "safe", "textEdit": generated.Edit},
		map[string]any{"label": "command", "textEdit": generated.Edit, "command": map[string]any{"command": "gopls.apply_fix"}},
		map[string]any{"label": "partial", "textEdit": generated.Edit, "additionalTextEdits": []wireTextEdit{unsafeAdditional}},
		map[string]any{"label": "snippet", "textEdit": generated.Edit, "insertTextFormat": 2},
		map[string]any{"label": "implicit", "insertText": "unsafe"},
		map[string]any{"label": "missing_positions", "textEdit": map[string]any{"range": map[string]any{}, "newText": "unsafe"}},
	}
	raw, _ := json.Marshal(map[string]any{"isIncomplete": true, "items": items})
	value, err := server.goCompletions(current, editSourceKey, raw)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(value)
	var result struct {
		IsIncomplete bool `json:"isIncomplete"`
		Items        []struct {
			Label    string       `json:"label"`
			TextEdit wireTextEdit `json:"textEdit"`
		} `json:"items"`
	}
	if err := json.Unmarshal(encoded, &result); err != nil || !result.IsIncomplete || len(result.Items) != 1 || result.Items[0].Label != "safe" {
		t.Fatalf("completion group filtering: %s, %v", encoded, err)
	}
	if result.Items[0].TextEdit.Range == generated.Edit.Range {
		t.Fatal("generated coordinates were forwarded without mapping")
	}
	// InsertReplaceEdit alternatives are checked separately, while keeping each
	// group atomic with its additional edits.
	insert := native.Edit.Range
	insert.End = insert.Start
	raw, _ = json.Marshal([]any{map[string]any{"label": "native", "textEdit": map[string]any{"newText": "2", "insert": insert, "replace": native.Edit.Range}}})
	value, err = server.goCompletions(current, editNativeKey, raw)
	encoded, _ = json.Marshal(value)
	if err != nil || !bytes.Contains(encoded, []byte(`"insert"`)) || !bytes.Contains(encoded, []byte(`"replace"`)) {
		t.Fatalf("native insert/replace completion lost: %s, %v", encoded, err)
	}
	uri, _ := PathToURI(current.names[editNativeKey])
	_, _, _ = server.documents.Change(uri, 8, nil)
	if _, err := server.goCompletions(current, editNativeKey, raw); !isStaleEdit(err) {
		t.Fatalf("stale completion returned: %v", err)
	}
}

func TestGoCompletionsKeepSafeAdditionalEditsAndRejectDefaults(t *testing.T) {
	text := "package p\nvar First = 1\nvar Second = 2\n"
	server, current := editState(t, source.UTF16, map[string]string{editNativeKey: text}, map[string]string{editNativeKey: text}, nil)
	first, second := editAt(t, current, editNativeKey, "First", "A"), editAt(t, current, editNativeKey, "Second", "B")
	item := map[string]any{"label": "two", "textEdit": first.Edit, "additionalTextEdits": []wireTextEdit{second.Edit}}
	raw, _ := json.Marshal([]any{item})
	value, err := server.goCompletions(current, editNativeKey, raw)
	encoded, _ := json.Marshal(value)
	if err != nil || !bytes.Contains(encoded, []byte(`"additionalTextEdits"`)) || !bytes.Contains(encoded, []byte(`"newText":"B"`)) {
		t.Fatalf("safe additional edit omitted: %s, %v", encoded, err)
	}
	raw, _ = json.Marshal(map[string]any{"items": []any{item}, "itemDefaults": map[string]any{"editRange": first.Edit.Range}})
	value, err = server.goCompletions(current, editNativeKey, raw)
	encoded, _ = json.Marshal(value)
	if err != nil || string(encoded) != `{"isIncomplete":false,"items":[]}` {
		t.Fatalf("unadvertised inherited edits escaped validation: %s, %v", encoded, err)
	}
}

func TestGoSymbolsMapNativeRanges(t *testing.T) {
	server, current, _, native := editFixture(t, source.UTF8)
	index := current.goProjection.indexes[native.URI]
	span := editByteSpan(t, index.Text(), "native")
	selection, _ := index.Range(span.Start, span.End, source.UTF16)
	whole, _ := index.Range(strings.Index(index.Text(), "func"), len(index.Text())-2, source.UTF16)
	raw, _ := json.Marshal([]goDocumentSymbol{{Name: "native", Kind: 12, Range: whole, SelectionRange: selection}})
	value, err := server.goSymbols(current, native.URI, raw)
	encoded, _ := json.Marshal(value)
	var symbols []goDocumentSymbol
	if json.Unmarshal(encoded, &symbols) != nil || err != nil || len(symbols) != 1 || symbols[0].Name != "native" {
		t.Fatalf("document symbols: %s, %v", encoded, err)
	}
	if symbols[0].Range.End.Character <= whole.End.Character {
		t.Fatal("native symbol end did not convert UTF-16 emoji width to UTF-8")
	}
}

func TestGoEditGuardSourceAndNativeEditsRegenerateAndRun(t *testing.T) {
	originals := map[string]string{
		editSourceKey: "package main\nfunc answer() int { return 41 }\nfunc main() { if answer() != 42 || native() != 2 { panic(\"wrong edited result\") } }\n",
		editNativeKey: "package main\nfunc native() int { return 1 }\n",
	}
	analyze := func(texts map[string]string) (*analysis.Result, map[string][]byte, map[string]*source.Map) {
		t.Helper()
		sources := map[string][]byte{}
		for key, text := range texts {
			sources[key] = []byte(text)
		}
		result, err := analysis.Analyze(t.Context(), analysis.Input{PackagePath: "example.org/edits", Sources: sources, Options: analysis.Options{PreserveSyntax: true}})
		if err != nil {
			t.Fatal(err)
		}
		maps := map[string]*source.Map{}
		output, err := lower.Generate(t.Context(), result, lower.GenerationOptions{SourceMaps: maps})
		if err != nil {
			t.Fatal(err)
		}
		return result, output, maps
	}
	result, output, maps := analyze(originals)
	projected := map[string]string{editNativeKey: originals[editNativeKey]}
	for key, data := range output {
		projected[key] = string(data)
	}
	server, current := editState(t, source.UTF16, originals, projected, maps)
	current.result = result
	changes := []goEdit{editAt(t, current, editSourceKey, "41", "42"), editAt(t, current, editNativeKey, "1", "2")}
	mapped, err := server.guardGoEdits(current, changes)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range mapped.DocumentChanges {
		name, _ := URIToPath(change.TextDocument.URI)
		key := current.keys[name]
		index := source.New(originals[key])
		type replacement struct {
			start, end int
			text       string
		}
		var edits []replacement
		for _, edit := range change.Edits {
			start, end, err := index.Offsets(edit.Range, source.UTF16)
			if err != nil {
				t.Fatal(err)
			}
			edits = append(edits, replacement{start, end, edit.NewText})
		}
		sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
		for _, edit := range edits {
			originals[key] = originals[key][:edit.start] + edit.text + originals[key][edit.end:]
		}
	}
	_, first, _ := analyze(originals)
	_, second, _ := analyze(originals)
	if !bytes.Equal(first[editSourceKey], second[editSourceKey]) {
		t.Fatal("same mapped edits produced different generated bytes")
	}
	root := t.TempDir()
	for name, content := range map[string]string{"go.mod": "module example.org/edits\n\ngo 1.27\n", "main_gox.go": string(first[editSourceKey]), "native.go": originals[editNativeKey]} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	program := filepath.Join(root, "edited")
	if runtime.GOOS == "windows" {
		program += ".exe"
	}
	command := exec.CommandContext(t.Context(), filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "-o", program, ".")
	command.Dir = root
	command.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off", "GOSUMDB=off")
	if data, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build mapped source/native edits: %v\n%s", err, data)
	}
	if data, err := exec.CommandContext(t.Context(), program).CombinedOutput(); err != nil {
		t.Fatalf("run mapped source/native edits: %v\n%s", err, data)
	}
}
