package acceptance_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Read the runnable example itself so the acceptance scenario cannot drift.
func TestConfigPipelineApplicationRace(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	filename := filepath.Join(filepath.Dir(here), "../../examples/prototype/config_pipeline.hgo")
	source, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	macroRaceModule(t, string(source), configPipelineAssertions)
}

const configPipelineAssertions = `package main
import (
 "bytes"
 "encoding/json"
 "errors"
 "io"
 "log/slog"
 "strings"
 "testing"
 "github.com/hxsf/goxide/serde"
)

func invoke(t *testing.T, input string) (map[string]any, error) {
 t.Helper()
 var output bytes.Buffer
 logger := slog.New(slog.NewJSONHandler(&output, nil))
 err := runConfig(logger, "request-42", []byte(input))
 decoder := json.NewDecoder(&output)
 var event map[string]any
 if decodeErr := decoder.Decode(&event); decodeErr != nil { t.Fatalf("JSON log: %v", decodeErr) }
 if decodeErr := decoder.Decode(new(any)); decodeErr != io.EOF { t.Fatalf("expected exactly one event, got %v", decodeErr) }
 if event["request_id"] != "request-42" { t.Fatalf("request correlation lost: %#v", event) }
 return event, err
}

func TestAcceptedConfig(t *testing.T) {
 for _, c := range []struct{input, mode string; workers float64}{
  {"{\"Name\":\"mail-dispatch\",\"Mode\":{\"Workers\":4}}", "workers", 4},
  {"{\"Name\":\"mail-dispatch\",\"Mode\":\"DryRun\"}", "dry_run", 0},
 } {
  event, err := invoke(t, c.input)
  if err != nil { t.Fatal(err) }
  if event["msg"] != "configuration accepted" || event["level"] != "INFO" || event["name"] != "mail-dispatch" || event["mode"] != c.mode || event["workers"] != c.workers { t.Fatalf("wrong success fields: %#v", event) }
  if _, exists := event["error"]; exists { t.Fatalf("unexpected error: %#v", event) }
 }
}

func TestRejectedConfig(t *testing.T) {
 for _, c := range []struct{name, input string; sentinel error}{
  {"bad-json", "{", nil},
  {"bad-enum-payload", "{\"Name\":\"mail-dispatch\",\"Mode\":{\"Workers\":false}}", nil},
  {"zero-workers", "{\"Name\":\"mail-dispatch\",\"Mode\":{\"Workers\":0}}", ErrInvalidWorkers},
  {"too-many-workers", "{\"Name\":\"mail-dispatch\",\"Mode\":{\"Workers\":65}}", ErrInvalidWorkers},
  {"missing-name", "{\"Name\":\"\",\"Mode\":\"DryRun\"}", ErrMissingName},
 } { t.Run(c.name, func(t *testing.T) {
  event, err := invoke(t, c.input)
  var typed ConfigError
  if !errors.As(err, &typed) || errors.Unwrap(err) == nil { t.Fatalf("#[from] error chain lost: %v", err) }
  if c.sentinel != nil && !errors.Is(err, c.sentinel) { t.Fatalf("business cause lost: %v", err) }
  if c.sentinel == nil { var detail *serde.Error; if !errors.As(err, &detail) { t.Fatalf("serde cause lost: %v", err) } }
  if event["level"] != "ERROR" || event["msg"] != "configuration rejected" || event["error"] != err.Error() || event["cause"] != errors.Unwrap(err).Error() { t.Fatalf("wrong rejection fields: %#v", event) }
  if strings.TrimSpace(event["cause"].(string)) == "" { t.Fatal("missing diagnostic cause") }
  if _, exists := event["workers"]; exists { t.Fatalf("failure emitted success fields: %#v", event) }
 }) }
}
`
