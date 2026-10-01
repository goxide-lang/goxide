package frontend

import (
	"strings"
	"testing"
)

func TestMacroPackageSharedFilesAndGeneratedNames(t *testing.T) {
	sources := map[string][]byte{
		"defs.hgo":   []byte("package p\nmacro_rules! add {($x:expr)=>{Helper($x)};}\n"),
		"runtime.go": []byte("package p\nfunc Helper(x int)int{return x+1}\nvar hgoMacroGeneratedF1N1=99\n"),
		"first.hgo":  []byte("package p\nfunc First()int{return add!(1)}\n"),
		"second.hgo": []byte("package p\nfunc Second()int{return add!(2)}\n"),
	}
	r, err := CompilePackage("example/p", sources, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(r.Files["first.hgo"].Go) == string(r.Files["second.hgo"].Go) {
		t.Fatal("generated aliases not file specific")
	}
}
func TestMacroPackageBuildBudgetAcrossFiles(t *testing.T) {
	source := "package p\nmacro_rules! one {()=>{1};}\nfunc F(){\n" + strings.Repeat("_=one!();", 2200) + "\n}\n"
	_, err := CompilePackage("example/p", map[string][]byte{"a.hgo": []byte(source), "b.hgo": []byte("package p\nfunc G(){" + strings.Repeat("_=one!();", 2200) + "}\n")}, nil)
	if err == nil || !strings.Contains(err.Error(), "expansion count budget") {
		t.Fatalf("cross-file budget: %v", err)
	}
}
