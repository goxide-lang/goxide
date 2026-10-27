package analysis_test

import (
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/analysis"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

func TestCandidateBindingsRejectValidButCapturedReferences(t *testing.T) {
	text := "package fixture\nfunc F()int{outer:=21;{inner:=42;_ = inner;return outer}}\n"
	_, queries := analyzeQueries(t, map[string][]byte{"main.gox": []byte(text)})
	at := strings.LastIndex(text, "outer")
	edits := []analysis.SourceEdit{{File: "fixture/main.gox", Span: syntax.Span{Start: uint32(at), End: uint32(at + 5)}, NewText: "inner"}}
	after, _ := analyzeQueries(t, map[string][]byte{"main.gox": []byte(applyAction(t, text, []analysis.CodeAction{{Title: "candidate", Edits: edits}}, "candidate"))})
	if after.Err != nil {
		t.Fatal(after.Err)
	}
	if err := queries.ValidateBindings(after, edits); err == nil {
		t.Fatal("accepted a type-correct reference captured by a different local")
	}
}

func TestCandidateBindingsFollowRenamedDefinitionsAndInsertedText(t *testing.T) {
	text := "package fixture\nfunc F(value int)int{return value}\n"
	_, queries := analyzeQueries(t, map[string][]byte{"main.gox": []byte(text)})
	var edits []analysis.SourceEdit
	for at := strings.Index(text, "value"); at >= 0; {
		edits = append(edits, analysis.SourceEdit{File: "fixture/main.gox", Span: syntax.Span{Start: uint32(at), End: uint32(at + 5)}, NewText: "renamedArgument"})
		next := strings.Index(text[at+5:], "value")
		if next < 0 {
			break
		}
		at += 5 + next
	}
	at := strings.Index(text, "func F")
	edits = append(edits, analysis.SourceEdit{File: "fixture/main.gox", Span: syntax.Span{Start: uint32(at), End: uint32(at)}, NewText: "// preserved binding after insertion\n"})
	after, _ := analyzeQueries(t, map[string][]byte{"main.gox": []byte(applyAction(t, text, []analysis.CodeAction{{Title: "candidate", Edits: edits}}, "candidate"))})
	if after.Err != nil {
		t.Fatal(after.Err)
	}
	if err := queries.ValidateBindings(after, edits); err != nil {
		t.Fatal(err)
	}
}
