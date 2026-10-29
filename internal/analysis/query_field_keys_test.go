package analysis_test

import (
	"strings"
	"testing"
)

func TestFieldKeyRenameUsesTheLiteralReceiver(t *testing.T) {
	text := "package fixture\nconst value=0\ntype Box struct{pub value int}\ntype Other struct{pub value int}\nfunc Source(b Box)int{c:=Box{value:b.value};_=Other{value:2};_=map[int]int{value:2};_=[2]int{value:2};return c.value}\n"
	result, queries := analyzeQueries(t, map[string][]byte{"main.gox": []byte(text)})
	key := uint32(strings.Index(text, "Box{value:") + len("Box{"))
	definitions, err := queries.Definition("fixture/main.gox", key)
	if err != nil || len(definitions) != 1 || int(definitions[0].Span.Start) != strings.Index(text, "value int") {
		t.Fatalf("literal key definition: %+v, %v", definitions, err)
	}
	for _, name := range []string{"count", "Value"} {
		t.Run(name, func(t *testing.T) {
			edits, err := queries.Rename("fixture/main.gox", key, name)
			if err != nil || len(edits) != 4 {
				t.Fatalf("field rename omitted a key or included another binding: %+v, %v", edits, err)
			}
			sources := applyQueryEdits(t, result, edits)
			updated := string(sources["main.gox"])
			if !strings.Contains(updated, "Box{"+name+":b."+name+"}") || !strings.Contains(updated, "return c."+name) || !strings.Contains(updated, "Other{value:2}") || !strings.Contains(updated, "map[int]int{value:2}") || !strings.Contains(updated, "[2]int{value:2}") {
				t.Fatalf("field rename changed a key from another receiver: %s", updated)
			}
			after, _ := analyzeQueries(t, sources)
			if after.Err != nil {
				t.Fatal(after.Err)
			}
		})
	}
}

func TestFieldKeysFollowAliasesAndImplicitPointerLiterals(t *testing.T) {
	text := "package fixture\ntype Box struct{pub value int}\ntype Alias = Box\nfunc Source(){_=Alias{value:1};_=[]*Alias{{value:2}}}\n"
	_, queries := analyzeQueries(t, map[string][]byte{"main.gox": []byte(text)})
	refs, err := queries.FindReferences("fixture/main.gox", uint32(strings.Index(text, "value int")), true)
	if err != nil || refs == nil || len(refs.Locations) != 3 {
		t.Fatalf("alias/pointer field references: %+v, %v", refs, err)
	}
}
