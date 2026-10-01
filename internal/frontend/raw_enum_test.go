package frontend

import (
	"strings"
	"testing"
)

func TestRawEnumConstantAndOpenMatch(t *testing.T) {
	src := `package p
enum Status string { Pending="pending", Paid="paid", }
enum Code uint8 { Zero=0, Two=1<<1, Three=Code::Two+1, }
const Count=Code::Three
var array [Count]int
func f(s Status)string{
 return match s {Status::Pending if false=>"guard",Status::Pending=>"pending",Status::Paid=>"paid",other=>string(other),}
}
func g()Status{return Status("future")}
`
	r, err := Compile("raw.hgo", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(r.Go), "invalid enum Status") || strings.Contains(string(r.Go), ".(type)") {
		t.Fatal("raw represented as closed case")
	}
}
func TestRawEnumCheckedConstants(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"duplicate-int", "enum Code int { A=1<<2,B=4,}", "same-value aliases pending user decision"},
		{"duplicate-string", `enum Code string { A="x"+"y",B="xy",}`, "same-value aliases pending user decision"},
		{"overflow", "enum Code uint8 { A=256,}", "overflows"},
		{"negative", "enum Code uint8 { A=-1,}", "overflows"},
		{"nonconst", "func value()int{return 1};enum Code int { A=value(),}", "not constant"},
		{"iota", "enum Code int { A=(iota),}", "iota is unsupported"},
		{"base", "enum Code bool { A=true,}", "integer or string"},
		{"exhaustive-known", "enum Code int { A=1,B=2,};func f(c Code){match c{Code::A=>{},Code::B=>{},}}", "unguarded wildcard"},
		{"guarded-rest", "enum Code int { A=1,};func f(c Code){match c{Code::A=>{},_ if true=>{},}}", "unguarded wildcard"},
		{"unreachable", "enum Code int { A=1,};func f(c Code){match c{Code::A=>{},Code::A if true=>{},_=>{},}}", "unreachable"},
		{"derive", "#[derive(Clone)]\nenum Code int {A=1,}", "derive on raw"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compile("raw.hgo", []byte("package p\n"+tc.body+"\n"))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %s, got %v", tc.want, err)
			}
		})
	}
}
func TestRawEnumPackageConstantsAndShadowing(t *testing.T) {
	_, err := CompilePackage("example/p", map[string][]byte{
		"native.go": []byte("package p\nconst base=1<<3\n"),
		"defs.hgo":  []byte("package p\nenum code uint8 {A=base,B=base+1,}\n"),
		"uses.hgo":  []byte("package p\nconst size=code::B\nvar table [size]int\n"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = CompilePackage("example/p", map[string][]byte{
		"native.go": []byte("package p\ntype int=string\n"),
		"defs.hgo":  []byte("package p\nenum code int {A=\"wrong\",}\n"),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "underlying type must resolve") {
		t.Fatal(err)
	}
}

func TestRawEnumLocalShadowAndDistinctIdentity(t *testing.T) {
	_, err := Compile("shadow.hgo", []byte(`package p
enum Status string {Known="known",}
func run(s Status)string{Status:=0;_=Status;return match s {Status::Known=>"known",value=>string(value),}}
`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = Compile("identity.hgo", []byte(`package p
enum First int {A=1,}
enum Second int {A=1,}
func bad(){var x First=Second::A;_=x}
`))
	if err == nil || !strings.Contains(err.Error(), "cannot use") {
		t.Fatal(err)
	}
	_, err = Compile("distinct.hgo", []byte("package p\nenum First int {A=1,};enum Second int {A=1,}\n"))
	if err != nil {
		t.Fatal(err)
	}
}
