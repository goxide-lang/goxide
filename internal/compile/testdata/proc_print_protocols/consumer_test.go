package main

import (
	"errors"
	model "example.org/printing-model"
	"io"
	"os"
	"strings"
	"testing"
)

func capture(t *testing.T, run func()) (string, any) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = writer
	var caught any
	func() { defer func() { caught = recover() }(); run() }()
	os.Stdout = previous
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	return string(data), caught
}

func TestOrdinaryPrintOutput(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func()
		want string
	}{
		{"empty", model.EmptyPrint, ""},
		{"newlines", model.Newlines, "\n\ntail\n\n{}"},
		{"hygiene", model.Hygiene, "5 caller local\n"},
		{"nested", model.Nested, "inner\nouter 9\n"},
		{"forwarded", model.Forwarded, "forward 13\n"},
		{"derived", model.Derived, "Record { Number: 3, Text: \"first\" } | Changed { Flag: true, Count: 17, Note: \"new\" }\nRecord { Number: 21, Text: \"changed\" }\n"},
		{"recover", model.DirectRecover, "true\n"},
		{"scalars", model.Scalars, "true -8 -16 -32 -64 1 8 16 32 64 9 1.25 2.5 (1+2i) (3+4i) plain \"line\\n\"\n"},
		{"raw-trailing", model.RawAndTrailing, "raw {} 4\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output, caught := capture(t, tc.run)
			if caught != nil || output != tc.want {
				t.Fatalf("output = %q, panic = %v, want %q", output, caught, tc.want)
			}
		})
	}
}

func TestOrdinaryPrintEvaluation(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		run                      func()
		output, trace, panicText string
	}{
		{"order", model.Order, "7 8\n", "AxAyFxFy", ""},
		{"format-panic", model.FormattingFailure, "", "AxAyFx", "formatter failed"},
		{"invalid-enum", model.InvalidEnum, "", "Ay", "goxide: match on nil Event"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model.Reset()
			output, caught := capture(t, tc.run)
			if output != tc.output || model.Trace() != tc.trace {
				t.Fatalf("output = %q, trace = %q, want %q / %q", output, model.Trace(), tc.output, tc.trace)
			}
			if tc.panicText == "" {
				if caught != nil {
					t.Fatalf("unexpected panic: %v", caught)
				}
				return
			}
			message, ok := caught.(string)
			if !ok || !strings.Contains(message, tc.panicText) {
				t.Fatalf("panic = %v, want containing %q", caught, tc.panicText)
			}
		})
	}
	model.Reset()
	returned := false
	output, caught := capture(t, func() { returned = model.ShortCircuit() })
	if caught != nil || !returned || output != "" || model.Trace() != "Axfail" {
		t.Fatalf("question propagation: output = %q, panic = %v, returned = %t, trace = %q", output, caught, returned, model.Trace())
	}
}

func TestOrdinaryPrintWriteFailure(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = writer
	defer func() { os.Stdout = previous }()
	// An empty print performs no write; a newline must attempt the closed writer.
	model.EmptyPrint()
	var caught any
	func() { defer func() { caught = recover() }(); model.Newlines() }()
	failure, ok := caught.(error)
	if !ok || !errors.Is(failure, os.ErrClosed) {
		t.Fatalf("write panic = %v, want os.ErrClosed", caught)
	}
}
