package lsp

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/source"
)

func documentURI(t *testing.T) string {
	t.Helper()
	uri, err := PathToURI(filepath.Join(t.TempDir(), "未保存.gox"))
	if err != nil {
		t.Fatal(err)
	}
	return uri
}

func textRange(line, start, end int) *source.Range {
	return &source.Range{Start: source.Position{Line: line, Character: start}, End: source.Position{Line: line, Character: end}}
}

func TestDocumentSequentialUnicodeEditsAndSnapshots(t *testing.T) {
	uri := documentURI(t)
	store := NewDocumentStore("")
	epoch, err := store.Open(uri, 1, "a😀中\r\nlast\r\n")
	if err != nil || epoch != 1 {
		t.Fatalf("open: epoch=%d, %v", epoch, err)
	}
	before := store.Snapshot()
	epoch, changed, err := store.Change(uri, 2, []ContentChange{
		{Range: textRange(0, 1, 3), Text: "ββ"},
		{Range: textRange(0, 3, 4), Text: "字"},
		{Range: &source.Range{Start: source.Position{Line: 1, Character: 4}, End: source.Position{Line: 2, Character: 0}}, Text: "!\n"},
	})
	if err != nil || !changed || epoch != 2 {
		t.Fatalf("change: epoch=%d, changed=%v, %v", epoch, changed, err)
	}
	document, _ := store.Get(uri)
	if document.Text != "aββ字\r\nlast!\n" || document.Index.Text() != document.Text || document.Version != 2 {
		t.Fatalf("sequential changes: %+v", document)
	}
	if old := before.Documents[uri]; old.Text != "a😀中\r\nlast\r\n" || old.Version != 1 || old.Index.Text() != old.Text || before.Epoch != 1 {
		t.Fatalf("published snapshot changed: %+v", before)
	}
	delete(before.Documents, uri)
	if _, exists := store.Get(uri); !exists {
		t.Fatal("snapshot map aliases store map")
	}
	if epoch, changed := store.Close(uri); !changed || epoch != 3 {
		t.Fatalf("close: %d, %v", epoch, changed)
	}
	if _, exists := store.Get(uri); exists {
		t.Fatal("closed document remained in overlay snapshot")
	}
}

func TestDocumentInvalidChangeDoesNotCommitEarlierEdits(t *testing.T) {
	uri := documentURI(t)
	store := NewDocumentStore(source.UTF16)
	if _, err := store.Open(uri, 4, "a😀z"); err != nil {
		t.Fatal(err)
	}
	changes := []ContentChange{
		{Range: textRange(0, 0, 1), Text: "b"},
		{Range: textRange(0, 2, 3), Text: "bad"}, // splits the surrogate pair
	}
	if epoch, changed, err := store.Change(uri, 5, changes); err == nil || changed || epoch != 1 {
		t.Fatalf("invalid edit committed: %d, %v, %v", epoch, changed, err)
	}
	document, _ := store.Get(uri)
	if document.Text != "a😀z" || document.Version != 4 {
		t.Fatalf("earlier edit escaped rejected notification: %+v", document)
	}
	for _, version := range []int{3, 4} {
		if epoch, changed, err := store.Change(uri, version, []ContentChange{{Text: "old"}}); err == nil || changed || epoch != 1 {
			t.Fatalf("old version %d was accepted: %d, %v, %v", version, epoch, changed, err)
		}
	}
	if _, changed, err := store.Change(uri, 5, []ContentChange{{Text: "now"}}); err != nil || !changed {
		t.Fatalf("valid change after rejection: %v, %v", changed, err)
	}
}

func TestDocumentUnchangedBytesOnlyAdvanceVersion(t *testing.T) {
	uri := documentURI(t)
	store := NewDocumentStore(source.UTF16)
	if _, err := store.Open(uri, 1, "same"); err != nil {
		t.Fatal(err)
	}
	before, _ := store.Get(uri)
	epoch, changed, err := store.Change(uri, 2, []ContentChange{{Text: "temporary"}, {Text: "same"}})
	if err != nil || changed || epoch != 1 {
		t.Fatalf("unchanged bytes caused analysis: %d, %v, %v", epoch, changed, err)
	}
	after, _ := store.Get(uri)
	if after.Version != 2 || after.Index != before.Index {
		t.Fatalf("version/index after unchanged change: %+v", after)
	}
	if epoch, err := store.Open(uri, 3, "replacement"); err == nil || epoch != 1 {
		t.Fatalf("duplicate open replaced current document: %d, %v", epoch, err)
	}
}

func TestDocumentNegotiatedEncodings(t *testing.T) {
	for _, test := range []struct {
		encoding source.Encoding
		end      int
	}{{source.UTF8, 5}, {source.UTF16, 3}, {source.UTF32, 2}} {
		t.Run(string(test.encoding), func(t *testing.T) {
			uri := documentURI(t)
			store := NewDocumentStore(test.encoding)
			if _, err := store.Open(uri, 1, "a😀z"); err != nil {
				t.Fatal(err)
			}
			if _, _, err := store.Change(uri, 2, []ContentChange{{Range: textRange(0, 1, test.end), Text: "X"}}); err != nil {
				t.Fatal(err)
			}
			if document, _ := store.Get(uri); document.Text != "aXz" {
				t.Fatalf("wrong encoded range: %q", document.Text)
			}
		})
	}
}

func TestFileURIRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "目录 #?%", "😀.gox")
	uri, err := PathToURI(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(uri, " ") || !strings.Contains(uri, "%23") || !strings.Contains(uri, "%3F") || !strings.Contains(uri, "%25") {
		t.Fatalf("URI is not escaped: %s", uri)
	}
	if got, err := URIToPath(uri); err != nil || got != path {
		t.Fatalf("URI round trip: %q, %v", got, err)
	}
	if _, err := PathToURI("relative.gox"); err == nil {
		t.Fatal("relative path was resolved implicitly")
	}
	for _, uri := range []string{"untitled:buffer", "file:relative", "file:///tmp/a?query", "file:///tmp/a#fragment", "file:///tmp/%00"} {
		if _, err := URIToPath(uri); err == nil {
			t.Errorf("invalid file URI was accepted: %s", uri)
		}
	}
	if runtime.GOOS == "windows" {
		for _, path := range []string{`C:\资料\file.gox`, `\\server\share\资料.gox`} {
			uri, err := PathToURI(path)
			if err != nil {
				t.Fatal(err)
			}
			if got, err := URIToPath(uri); err != nil || got != path {
				t.Fatalf("Windows path round trip: %q, %v", got, err)
			}
		}
	} else {
		if got, err := URIToPath("file:///C:/file.gox"); err != nil || got != "/C:/file.gox" {
			t.Fatalf("foreign Windows syntax was guessed on POSIX: %q, %v", got, err)
		}
		if _, err := URIToPath("file://server/share/a.gox"); err == nil {
			t.Fatal("remote authority was silently mapped to a local path")
		}
	}
}
