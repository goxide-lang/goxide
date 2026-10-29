package compile

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/goxide-lang/goxide/internal/project"
	pm "github.com/goxide-lang/syntax/proc_macro"
)

func TestProceduralSourceProjectionKeepsIndependentFeaturePlans(t *testing.T) {
	sdk := compilerSDKForTest(t)
	root := t.TempDir()
	app, macros, outer := filepath.Join(root, "app"), filepath.Join(root, "macros"), filepath.Join(root, "outer")
	writeProjectFile(t, macros, "goxide.toml", fmt.Sprintf(`[go_mod]
module="example.org/macros"
go="1.27"
[lib]
name="macros"
path="."
proc_macro=true
[dependencies]
syntax={path=%q}
[features]
left=[]
right=[]
`, filepath.ToSlash(sdk)))
	writeProjectFile(t, macros, "lib.gox", `package macros
use syntax::token;
use syntax::proc_macro as pm;
#[cfg(feature("left"))]
func text() string { return "1" }
#[cfg(feature("right"))]
func text() string { return "2" }
#[proc_macro]
pub func value(ctx pm::Context, input token::TokenStream) pm::Output {
 var output, err = token::ParseText(text(), ctx.DefSite())
 return pm::FromParse(output, err)
}
`)
	writeProjectFile(t, outer, "goxide.toml", fmt.Sprintf(`[go_mod]
module="example.org/outer"
go="1.27"
[lib]
name="outer"
path="."
proc_macro=true
[dependencies]
syntax={path=%q}
upstream={path="../macros",features=["right"]}
`, filepath.ToSlash(sdk)))
	writeProjectFile(t, outer, "lib.gox", `package outer
use syntax::token;
use syntax::proc_macro as pm;
use upstream;
#[proc_macro]
pub func nested(ctx pm::Context, input token::TokenStream) pm::Output {
 var output, err = token::ParseText("crate::forward!()", ctx.DefSite())
 return pm::FromParse(output, err)
}
#[proc_macro]
pub func forward(ctx pm::Context, input token::TokenStream) pm::Output {
 var output, err = token::ParseText("upstream::value!()", ctx.DefSite())
 return pm::FromParse(output, err)
}
`)
	writeProjectFile(t, app, "goxide.toml", `[go_mod]
module="example.org/app"
go="1.27"
[[bin]]
name="app"
path="."
[dependencies]
direct={path="../macros",features=["left"]}
outer={path="../outer"}
fmt={go="fmt"}
`)
	writeProjectFile(t, app, "main.gox", `package main
use direct;
use outer;
use fmt;
func main() { fmt::Println(direct::value!(), outer::nested!()) }
`)
	entries := map[string]int{}
	result, err := PrepareProject(app, Options{Offline: true, RunProcMacro: testWASI(t, func(request pm.Request) {
		entries[request.Entry()]++
	})})
	if err != nil {
		t.Fatal(err)
	}
	if entries["value"] != 2 || entries["forward"] != 1 || entries["nested"] != 1 {
		t.Fatalf("independent plans were not both invoked: %v", entries)
	}
	command := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "run", "-mod=readonly", result.Entry)
	command.Dir, command.Env = result.Root, project.GoEnv(true)
	output, err := command.CombinedOutput()
	if err != nil || string(output) != "1 2\n" {
		t.Fatalf("nested DefSite macro used a different role's source projection: %v\n%s", err, output)
	}
}
