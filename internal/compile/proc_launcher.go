package compile

import (
	"fmt"
	"go/format"
	"strings"

	"github.com/goxide-lang/goxide/internal/analysis"
	pm "github.com/goxide-lang/syntax/proc_macro"
)

func procLauncher(library *analysis.ProcLibrary) ([]byte, error) {
	var output strings.Builder
	alias := "macros"
	if len(library.Entries) == 0 {
		alias = "_"
	}
	fmt.Fprintf(&output, `package main
import (
 "fmt"
 "os"
 %s %q
 pm "github.com/goxide-lang/syntax/proc_macro"
)
func run() error {
 request, err := pm.ReadRequest(os.Stdin)
 if err != nil { return err }
 output, err := pm.Dispatch(request, []pm.Entry{
`, alias, library.PackagePath)
	fields := map[pm.Kind]string{pm.Function: "Function", pm.Attribute: "Attribute", pm.Derive: "Derive"}
	for _, entry := range library.Entries {
		fmt.Fprintf(&output, "{Name: %q, %s: macros.%s},\n", entry.Name, fields[entry.Kind], library.GoNames[entry.Name])
	}
	fmt.Fprintln(&output, `})
 if err != nil { return err }
 return pm.WriteResponse(os.Stdout, request, output)
}
func main() {
 if err := run(); err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
}`)
	return format.Source([]byte(output.String()))
}
