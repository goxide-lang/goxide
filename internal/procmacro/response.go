package procmacro

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	pm "github.com/goxide-lang/syntax/proc_macro"
	"github.com/goxide-lang/syntax/token"
)

const (
	MaxInvocationTime  = 5 * time.Second
	MaxCompilationTime = 60 * time.Second
)

// ValidateResponse applies the same SDK admission and combined diagnostic limit
// to CLI and browser hosts. Error diagnostics are valid protocol responses.
func ValidateResponse(request pm.Request, response []byte, stderrBytes int) error {
	if stderrBytes < 0 || stderrBytes > pm.MaxDiagnosticBytes {
		return fmt.Errorf("procedural macro stderr exceeds %d bytes", pm.MaxDiagnosticBytes)
	}
	_, err := pm.ReadResponse(bytes.NewReader(response), request)
	var diagnostic *pm.DiagnosticError
	if err != nil && !errors.As(err, &diagnostic) {
		return fmt.Errorf("procedural macro response: %w", err)
	}
	// Count the same canonical diagnostic JSON as the SDK. Raw response length
	// alone differs when strings are escaped or objects contain whitespace.
	var envelope struct {
		Diagnostics []struct {
			Severity pm.Severity     `json:"severity"`
			Message  string          `json:"message"`
			Span     *token.WireSpan `json:"span,omitempty"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal(response, &envelope); err != nil {
		return fmt.Errorf("procedural macro diagnostics: %w", err)
	}
	diagnosticBytes := 0
	if len(envelope.Diagnostics) != 0 {
		encoded, err := json.Marshal(envelope.Diagnostics)
		if err != nil {
			return err
		}
		diagnosticBytes = len(encoded)
	}
	if diagnosticBytes > pm.MaxDiagnosticBytes-stderrBytes {
		return fmt.Errorf("procedural macro diagnostics and stderr exceed %d bytes", pm.MaxDiagnosticBytes)
	}
	return nil
}
