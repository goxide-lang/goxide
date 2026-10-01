package frontend

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDiagnosticLocationsAndUnsupported(t *testing.T) {
	for _, tc := range []struct{ src, msg string }{
		{"#[derive(Serialize)]\ntype S struct { X chan int }", "unsupported field type"},
		{"#[derive(Serialize)]\ntype S struct { X Other }", "unsupported field type"},
		{"#[derive(Clone)]\ntype S struct { X Other }", "nested type requires Clone"},
		{"#[derive(Serialize)]\ntype S struct { X int }\nfunc SerializeS() {}", "conflicts"},
		{"#[derive(Serialize)]\ntype S struct { __hgo_serde int }", "reserved"},
		{"#[derive(Serialize)]\ntype S[T any] struct { X T }", "generic"},
		{"#[derive(Serialize)]\ntype S struct {\n#[serde(unknown)]\nX int\n}", "unsupported serde attribute"},
	} {
		_, e := Compile("input.hgo", []byte("package p\n"+tc.src))
		if e == nil || !strings.Contains(e.Error(), tc.msg) || !strings.Contains(e.Error(), "input.hgo:") {
			t.Errorf("%q: %v", tc.src, e)
		}
	}
}
func TestExtrasAndMap(t *testing.T) {
	r, e := Compile("input.hgo", []byte(`package p
#[derive(Debug, Clone)]
type S struct { Values []int }
#[derive(Error)]
#[error("failed")]
type Failure struct {
#[from]
Cause error
}
`))
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"DebugS", "CloneS", "FromFailure", "Unwrap"} {
		if !strings.Contains(string(r.Go), name) {
			t.Error(name)
		}
	}
	var m map[string]any
	if e := json.Unmarshal(r.SourceMap, &m); e != nil {
		t.Fatal(e)
	}
	if len(m) == 0 {
		t.Fatal("empty source map")
	}
}
func TestEmptyAndCommaNames(t *testing.T) {
	r, e := Compile("input.hgo", []byte(`package p
#[derive(Serialize, Deserialize)]
type S struct {
#[serde(rename="")]
X int
#[serde(rename="a,b")]
Y string
}
`))
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(r.Go), `Field("",`) || !strings.Contains(string(r.Go), `Field("a,b",`) {
		t.Fatal(string(r.Go))
	}
}

func TestErrorDeriveFieldMethodCollisions(t *testing.T) {
	for _, tc := range []struct{ fields, collision string }{
		{"Error string", "Error"},
		{"Unwrap string\n#[source]\nCause error", "Unwrap"},
	} {
		_, err := Compile("collision.hgo", []byte("package p\n#[derive(Error)]\n#[error(\"failed\")]\ntype Failure struct {\n"+tc.fields+"\n}\n"))
		if err == nil || !strings.Contains(err.Error(), "collision.hgo:5:") || !strings.Contains(err.Error(), "derived method conflicts with field "+tc.collision) {
			t.Fatalf("%s: %v", tc.collision, err)
		}
	}
	// Unwrap is an ordinary field if no source requests an Unwrap method.
	if _, err := Compile("valid.hgo", []byte("package p\n#[derive(Error)]\ntype Failure struct { Unwrap string }\n")); err != nil {
		t.Fatal(err)
	}
}

func TestAttributesCannotBeSilentlyIgnored(t *testing.T) {
	for _, src := range []string{
		"#[derive(Debug)]\n#[serde(deny_unknown_fields)]\ntype S struct { X int }",
		"#[derive(Clone)]\ntype S struct {\n#[serde(rename=\"x\")]\nX int\n}",
		"#[derive(Serialize)]\ntype (\n A struct { X int }\n B struct { X int }\n)",
	} {
		if _, err := Compile("ignored.hgo", []byte("package p\n"+src)); err == nil {
			t.Errorf("silently ignored attribute: %s", src)
		}
	}
}
