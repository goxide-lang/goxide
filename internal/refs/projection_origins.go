package refs

import "github.com/hxsf/goxide/internal/sourcemap"

// MapSource carries only actual copied bytes from permission binding. Generated
// stubs, aliases and replaced call heads receive no invented source token.
func (b ImportBindings) MapSource(filename string, output []byte, input *sourcemap.Map) *sourcemap.Map {
	if input == nil {
		return nil
	}
	stage := b.Stage
	if stage == "" {
		stage = "refs.BindPermissions"
	}
	result := &sourcemap.Map{Source: output, Stages: append(append([]string{}, input.Stages...), stage)}
	copied := b.Copied[filename]
	for start := 0; start < len(copied) && start < len(output); {
		at := copied[start]
		if at < 0 || at >= len(input.Source) || output[start] != input.Source[at] {
			start++
			continue
		}
		end := start + 1
		for end < len(copied) && end < len(output) && copied[end] == at+end-start && copied[end] < len(input.Source) && output[end] == input.Source[copied[end]] {
			end++
		}
		result.Segments = append(result.Segments, input.Copy(at, at+end-start, start)...)
		start = end
	}
	return result
}
