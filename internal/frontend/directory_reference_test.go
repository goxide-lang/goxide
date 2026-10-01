package frontend

import (
	"strings"
	"testing"
)

func TestDirectoryReferencePackage(t *testing.T) {
	files := map[string][]byte{
		"read.hgo":  []byte("package p\nfunc read(r &int)int{return *r}\n"),
		"write.hgo": []byte("package p\nfunc write(r mut& int){*r+=1}\n"),
		"run.hgo":   []byte("package p\nfunc Run()int{n:=41;w:=mut& n;write(w);return read(&*w)}\n"),
	}
	r, err := CompilePackage("example/p", files, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(r.Files["read.hgo"].Go), "__hgo_ref_") {
		t.Fatal("permission IR leaked")
	}
}

func TestDirectoryReferenceRejectsErasure(t *testing.T) {
	for _, tc := range []struct{ name, callee, caller, want string }{
		{"write-read", "func use(r mut& int){*r=7}", "func bad(r &int){use(r)}", "__hgo_ref_write_int value"},
		{"native-pointer", "func use(r *int){*r=7}", "func bad(r &int){use(r)}", "permission would be erased"},
		{"readonly-return", "func use(r &int) &int{return r}", "func bad(r &int){x:=use(r);*x=7}", "cannot write through a readonly"},
		{"exported-global", "var n int; var Public = mut& n", "func Run(){}", "package/global references"},
		{"exported-alias", "type Public = &int", "func Run(){}", "reference aliases/defined types"},
		{"exported-function-value", "func use(r &int)int{return *r};var Public=use", "func Run(){}", "package/global references"},
		{"method", "type box struct{};func(b box) use(r &int)int{return *r}", "func Run(){}", "reference methods/generics"},
		{"exported", "func Read(r &int)int{return *r}", "func Run(){}", "exported reference signatures"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			name := "callee.hgo"
			if tc.name == "native-pointer" {
				name = "callee.go"
			}
			_, err := CompilePackage("example/p", map[string][]byte{name: []byte("package p\n" + tc.callee + "\n"), "caller.hgo": []byte("package p\n" + tc.caller + "\n")}, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("wanted %s, got %v", tc.want, err)
			}
			t.Log(err)
		})
	}
}
