package refs

import (
	"errors"
	"fmt"
	"go/token"
)

// Error reports a checker location in the input buffer passed to Finish.
// Generated is true when restoration could not prove an exact copied input
// byte. Frontends must map a non-generated position through their provenance
// before claiming that it is an original .gox source location.
type Error struct {
	Position  token.Position
	Message   string
	Generated bool
}

func (e *Error) Error() string { return fmt.Sprintf("%s: refs: %s", e.Position, e.Message) }

func restoreError(err error, restored mapped, filename string, source []byte) error {
	var diagnostic *Error
	if !errors.As(err, &diagnostic) {
		return err
	}
	copy := *diagnostic
	at := copy.Position.Offset
	if copy.Generated || at < 0 || at >= len(restored.copied) || restored.copied[at] < 0 {
		copy.Generated = true
		return &copy
	}
	input := restored.copied[at]
	if input >= len(source) || at >= len(restored.text) || source[input] != restored.text[at] {
		copy.Generated = true
		return &copy
	}
	fs := token.NewFileSet()
	file := fs.AddFile(filename, -1, len(source))
	file.SetLinesForContent(source)
	copy.Position = file.Position(file.Pos(input))
	return &copy
}
