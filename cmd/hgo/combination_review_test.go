package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionFeaturesScriptTargetIntegration(t *testing.T) {
	proxy, cache := versionProxy(t)
	fixture := buildScriptFixture(t, false, false)
	raw, e := os.ReadFile(filepath.Join(fixture, "goxide.toml"))
	if e != nil {
		t.Fatal(e)
	}
	var m map[string]any
	if e = decodeManifestFixture(raw, &m); e != nil {
		t.Fatal(e)
	}
	delete(m, "bins")
	m["lib"] = map[string]string{"name": "lib", "path": "lib/lib.hgo"}
	files := map[string]string{}
	if e = filepath.Walk(fixture, func(path string, info os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		if info.IsDir() {
			return nil
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		rel, _ := filepath.Rel(fixture, path)
		files[filepath.ToSlash(rel)] = string(b)
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	files["goxide.toml"] = projectManifest(m)
	files["go.mod"] = "module example.org/hgolib\n\ngo 1.27\n"
	files["lib/lib.hgo"] = "package lib\nimport \"example.org/hgolib/gen\"\nfunc Value()string{return gen.Value}\n"
	publishVersion(t, proxy, "example.org/hgolib", "v1.0.0", files)
	publishVersion(t, proxy, "example.org/hgolib", "v1.1.0", files)
	root := versionApp(t, `{"module":"example.org/hgolib","version":"v1.0.0"}`)
	rootManifest := func(version string) string {
		return manifestJSON(`{"version":1,"features":{"extra":["lib/extra"]},"bins":[{"name":"app","path":"main.hgo"}],"dependencies":{"lib":{"module":"example.org/hgolib","version":"` + version + `"}}}`)
	}
	projectWrite(t, root, map[string]string{"goxide.toml": rootManifest("v1.0.0")})
	scriptAction(t, root, "trust-project")
	plan := scriptPlan(t, root)
	if len(plan.Subjects) != 1 {
		t.Fatal(plan)
	}
	before := sourceHash(t, filepath.Join(cache, "example.org/hgolib@v1.0.0"))
	if b, e := projectRun(t, root, "run", "--offline"); e == nil || strings.Contains(b, "FIXTURE_GENERATOR_EXECUTED") {
		t.Fatalf("unlicensed script: %s %v", b, e)
	}
	scriptAction(t, root, "allow", "--subject", plan.Subjects[0].ID)
	for i := 0; i < 2; i++ {
		b, e := projectRun(t, root, "run", "--offline")
		if e != nil || !strings.Contains(b, "schema-one/template-one/alpha") {
			t.Fatalf("%s %v", b, e)
		}
		if i == 1 && !strings.Contains(b, "cache hit") {
			t.Fatal("script cache not reused", b)
		}
	}
	if got := sourceHash(t, filepath.Join(cache, "example.org/hgolib@v1.0.0")); got != before {
		t.Fatal("script changed downloaded module")
	}
	build := func(args ...string) string {
		t.Helper()
		out, e := projectRun(t, root, append([]string{"build", "--offline"}, args...)...)
		if e != nil {
			t.Fatalf("build: %s %v", out, e)
		}
		lines := strings.Split(strings.TrimSpace(out), "\n")
		return lines[len(lines)-1]
	}
	a := build()
	runLayoutBinary(t, a, "schema-one/template-one/alpha//local")
	b := build("--features", "extra")
	if a == b {
		t.Fatal("feature target identity collision")
	}
	runLayoutBinary(t, b, "schema-one/template-one/alpha/extra/local")
	if again := build("--locked"); again != a {
		t.Fatalf("A/B/A target identity %q != %q", again, a)
	}
	projectWrite(t, root, map[string]string{"goxide.toml": rootManifest("v1.1.0")})
	changed := scriptPlan(t, root)
	if len(changed.Subjects) != 1 || changed.Subjects[0].ID == plan.Subjects[0].ID || changed.Subjects[0].Trusted {
		t.Fatal("version upgrade reused permission", changed)
	}
	if b, e := projectRun(t, root, "run", "--offline"); e == nil || strings.Contains(b, "FIXTURE_GENERATOR_EXECUTED") {
		t.Fatalf("version auto-authorized: %s %v", b, e)
	}
	scriptAction(t, root, "allow", "--subject", changed.Subjects[0].ID)
	if b, e := projectRun(t, root, "run", "--offline"); e != nil {
		t.Fatalf("%s %v", b, e)
	}
	v2 := build("--features", "extra")
	if v2 == a || v2 == b {
		t.Fatal("version target identity collision")
	}
	runLayoutBinary(t, v2, "schema-one/template-one/alpha/extra/local")
	scriptAction(t, root, "revoke", "--subject", changed.Subjects[0].ID)
	if b, e := projectRun(t, root, "run", "--offline"); e == nil || strings.Contains(b, "schema-one") {
		t.Fatalf("revoked cache executed: %s %v", b, e)
	}
}
