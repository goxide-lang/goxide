//go:build goxideweb

package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmbeddedAssetsAndBoundaries(t *testing.T) {
	h := handler()
	for _, test := range []struct {
		method, path string
		code         int
		content      string
	}{
		{"GET", "/rfc.html?feature=traits", 200, "id=\"root\""},
		{"GET", "/playground.html", 200, "id=\"root\""},
		{"GET", "/local.html", 200, "id=\"root\""},
		{"GET", "/project-reexports.zip", 200, "PK"},
		{"GET", "/", 200, "写得更明确"}, {"GET", "/compiler.wasm", 200, ""},
		{"HEAD", "/wasm_exec.js", 200, ""}, {"GET", "/examples.json", 200, "compilerDigest"},
		{"POST", "/", 405, ""}, {"GET", "/../go.mod", 404, ""},
		{"GET", "/dist/", 404, ""}, {"GET", "/.git/config", 404, ""},
		{"GET", "/%2e%2e/go.mod", 404, ""}, {"GET", "/missing", 404, ""},
	} {
		t.Run(test.method+test.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(test.method, test.path, nil))
			if w.Code != test.code {
				t.Fatalf("status %d, want %d", w.Code, test.code)
			}
			if !strings.Contains(w.Body.String(), test.content) {
				t.Fatal("missing embedded content")
			}
			if w.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal("missing nosniff")
			}
			if !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
				t.Fatal("missing CSP")
			}
			if test.path == "/compiler.wasm" && w.Header().Get("Content-Type") != "application/wasm" {
				t.Fatal("wrong WASM MIME")
			}
		})
	}
}
