package analysis_test

import (
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/analysis"
)

func TestActionCandidateExpectedDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, text, title string }{
		{"associated", "package fixture\ntrait Read { type Item; func read(self Self) Self::Item }\ntype Value struct{}\nimpl Read for Value {}\n", "Implement all missing members"},
		{"associated_name_prefix", "package fixture\ntrait Read { type Items; type Item; func read(self Self) Self::Item }\ntype Value struct{}\nimpl Read for Value {}\n", "Implement all missing members"},
		{"existing_placeholder_name", "package fixture\ntrait Read { type Item; func read(self Self) Self::Item }\ntype Value struct{}\ntype __GoxideTODO_Item int\nimpl Read for Value {}\n", "Implement all missing members"},
		{"partial", "package fixture\ntrait Read { func read(self Self) int; func write(self Self) }\ntype Value struct{}\nimpl Read for Value {}\n", "Implement read"},
		{"associated_partial", "package fixture\ntrait Read { type Item; func read(self Self) Self::Item; func write(self Self) }\ntype Value struct{}\nimpl Read for Value {}\n", "Implement read"},
		{"existing_diagnostic", "package fixture\ntrait Read { func read(self Self) int; func write(self Self) }\ntype Value struct{}\nimpl Read for Value {}\nfunc unfinished(){missing()}\n", "Implement read"},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := analysis.Input{Sources: map[string][]byte{"main.gox": []byte(test.text)}, PackagePath: "fixture", Options: analysis.Options{Editor: true}}
			before, actions := editorActions(t, input, "impl Read")
			edited := applyAction(t, test.text, actions, test.title)
			input.Sources["main.gox"] = []byte(edited)
			after, err := analysis.Analyze(t.Context(), input)
			if err != nil {
				t.Fatal(err)
			}
			if test.name == "existing_placeholder_name" && !strings.Contains(edited, "type Item = __GoxideTODO_Item_2") {
				t.Fatalf("placeholder selected an existing type: %s", edited)
			}
			for _, action := range actions {
				if action.Title == test.title {
					if err := analysis.NewQueries(before).ValidateCodeAction(after, action); err != nil {
						t.Fatal(err)
					}
				}
			}
		})
	}
}

func TestActionCandidateRejectsNewDiagnosticsAndUnplannedPlaceholders(t *testing.T) {
	text := "package fixture\ntrait Read { type Item; func read(self Self) Self::Item }\ntype Value struct{}\nimpl Read for Value {}\n"
	for _, replacement := range []string{"panic(missing())", "panic(__GoxideTODO_Other)", "panic(__GoxideTODO_Item)"} {
		t.Run(replacement, func(t *testing.T) {
			input := analysis.Input{Sources: map[string][]byte{"main.gox": []byte(text)}, PackagePath: "fixture", Options: analysis.Options{Editor: true}}
			before, actions := editorActions(t, input, "impl Read")
			for _, action := range actions {
				if action.Title != "Implement all missing members" {
					continue
				}
				for i := range action.Edits {
					action.Edits[i].NewText = strings.Replace(action.Edits[i].NewText, `panic("TODO: implement read")`, replacement, 1)
				}
				edited := applyAction(t, text, []analysis.CodeAction{action}, action.Title)
				input.Sources["main.gox"] = []byte(edited)
				after, err := analysis.Analyze(t.Context(), input)
				if err != nil {
					t.Fatal(err)
				}
				if err := analysis.NewQueries(before).ValidateCodeAction(after, action); err == nil {
					t.Fatalf("accepted an unplanned unresolved reference: %s", edited)
				}
				return
			}
			t.Fatal("fixture did not plan an action")
		})
	}
}
