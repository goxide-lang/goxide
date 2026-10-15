package main

import (
	"fmt"
	"github.com/hxsf/goxide/internal/naming"
	"golang.org/x/term"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// All Cobra/pflag dependencies live in this adapter. Services accept ordinary
// request structs and return errors; only main decides the process exit status.
func run(args []string, out, stderr io.Writer) error {
	if err := naming.Environment(); err != nil {
		return err
	}
	root := newCLI(out, stderr)
	if len(args) == 0 {
		return fmt.Errorf("usage: goxide COMMAND [flags] INPUT; use goxide --help")
	}
	// Cobra's completion protocol calls no service or business pre-run hook.
	if args[0] == "__complete" || args[0] == "__completeNoDesc" {
		// The final token is an unfinished completion prefix, not an option.
		// Validate completed tokens with an opaque value placeholder so pending
		// option values can still be completed without accepting old spellings.
		if len(args) > 2 {
			c, tail, err := routeCLI(root, args[1:len(args)-1])
			if err != nil {
				return err
			}
			if err := checkOptionTokens(c.Flags(), append(append([]string(nil), tail...), "")); err != nil {
				return err
			}
		}
		root.SetArgs(args)
		return root.Execute()
	}
	cmd, tail, err := routeCLI(root, args)
	if err != nil {
		return err
	}
	return executeCLI(cmd, tail)
}
func runImportGo(args []string, out, stderr io.Writer) error {
	return run(append([]string{"import-go"}, args...), out, stderr)
}
func runScripts(args []string, out, stderr io.Writer) error {
	return run(append([]string{"scripts"}, args...), out, stderr)
}
func cliCommand(use, short string) *cobra.Command {
	c := &cobra.Command{Use: use, Short: short, SilenceErrors: true, SilenceUsage: true,
		DisableSuggestions: true, ValidArgsFunction: cobra.NoFileCompletions}
	c.Flags().SetInterspersed(false)
	c.InitDefaultHelpFlag()
	return c
}
func newCLI(out, stderr io.Writer) *cobra.Command {
	root := cliCommand("goxide", "Compile Goxide source to ordinary Go")
	root.SetOut(out)
	root.SetErr(stderr)
	root.CompletionOptions.DisableDefaultCmd = true
	for _, mode := range []string{"build", "run", "test", "transpile", "expand"} {
		req := CompileRequest{Mode: mode}
		c := cliCommand(mode+" [flags] [INPUT]", "Compile "+mode+" input (defaults to the current project)")
		if mode == "run" {
			c.Use = "run [flags] [INPUT] [-- PROGRAM_ARGS...]"
		}
		if mode == "transpile" {
			c.Aliases = []string{"gen"}
		}
		f := c.Flags()
		f.StringVarP(&req.Output, "o", "o", "", "output source/directory (transpile), expanded source (expand), or binary (build)")
		f.StringVar(&req.SourceMap, "source-map", "", "write single-file source mapping JSON")
		f.StringVar(&req.Expanded, "expanded", "", "write single-file expanded Go source")
		f.StringVar(&req.Runtime, "runtime", "", "Go module root selecting pinned public std/serde dependencies (standalone single-file run/build only)")
		f.StringVar(&req.RootsFile, "roots", "", "explicit dependency namespace mapping file (native Go module or standalone source)")
		f.StringVar(&req.Tags, "tags", "", "Go build tags (directory mode; comma/space separated)")
		f.StringVar(&req.GoTool, "go", "go", "Go compiler executable (Go 1.27+; unused for single-file transpile/expand)")
		f.StringArrayVarP(&req.Packages, "package", "p", nil, "select workspace member by module identity (repeatable)")
		f.BoolVar(&req.Workspace, "workspace", false, "build all workspace members")
		f.StringVar(&req.Features, "features", "", "project features (comma/space separated)")
		f.BoolVar(&req.NoDefaultFeatures, "no-default-features", false, "disable project default features")
		f.StringVar(&req.Bin, "bin", "", "select project binary")
		f.StringVar(&req.Example, "example", "", "select project example")
		f.BoolVar(&req.Lib, "lib", false, "select project library")
		f.StringVar(&req.TargetDir, "target-dir", "", "managed project artifacts (flag > GOXIDE_TARGET_DIR > target)")
		f.BoolVar(&req.Offline, "offline", false, "use cached dependencies; does not sandbox scripts")
		f.BoolVar(&req.Locked, "locked", false, "require unchanged goxide.lock")
		c.RunE = func(c *cobra.Command, operands []string) error {
			req.Input = ""
			req.ProgramArgs = nil
			if len(operands) > 0 {
				req.Input = operands[0]
				if mode == "run" && len(operands) >= 2 && operands[1] == "--" {
					req.ProgramArgs = append([]string(nil), operands[2:]...)
				} else if len(operands) != 1 {
					return usageError(c, inputError())
				}
			}
			if req.Input == "" && mode == "expand" {
				return usageError(c, inputError())
			}
			return executeCompile(req, out, stderr)
		}
		root.AddCommand(c)
	}
	scripts := scriptsCommand("", out, stderr)
	for _, action := range scriptActions {
		scripts.AddCommand(scriptsCommand(action, out, stderr))
	}
	req := ImportRequest{}
	imp := cliCommand("import-go [flags] ROOT", "Plan or migrate a Go module into a new project")
	f := imp.Flags()
	f.BoolVar(&req.Workspace, "workspace", false, "import explicit ROOT/go.work in native mode")
	f.StringArrayVar(&req.MemberBins, "member-bin", nil, "workspace member MODULE::NAME=PATH binary")
	f.StringArrayVar(&req.MemberLibs, "member-lib", nil, "workspace member MODULE=PATH library")
	f.BoolVar(&req.Plan, "plan", false, "show migration plan (default without --out)")
	f.StringVar(&req.Out, "out", "", "new migrated project directory (never overwritten)")
	f.StringVar(&req.GoTool, "go", "go", "Go compiler executable")
	f.BoolVar(&req.Offline, "offline", false, "disable remote probing/downloads; use local files and cached modules only")
	f.StringVar(&req.Lib, "lib", "", "explicit library directory/file")
	f.StringArrayVar(&req.Sources, "source", nil, "supplied MODULE=local|remote; repeatable, disables automatic selection")
	f.StringArrayVar(&req.AcceptSourceDiff, "accept-source-diff", nil, "confirm differences for an explicitly remote MODULE")
	f.StringArrayVar(&req.Bins, "bin", nil, "explicit name=path binary; repeatable, commas preserved")
	imp.RunE = func(c *cobra.Command, args []string) error {
		if len(args) != 1 {
			return usageError(c, fmt.Errorf("exactly one ROOT is required; put flags before the input"))
		}
		req.Root = args[0]
		req.Input = os.Stdin
		req.Interactive = term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stderr.Fd()))
		return executeImportGo(req, out, stderr)
	}
	help := cliCommand("help [COMMAND ...]", "Show command help")
	help.RunE = func(_ *cobra.Command, args []string) error {
		c := root
		for _, name := range args {
			next := childCommand(c, name)
			if next == nil {
				return fmt.Errorf("unknown help command %q", name)
			}
			c = next
		}
		return c.Help()
	}
	root.SetHelpCommand(help)
	root.AddCommand(help)
	completion := cliCommand("completion", "Print a shell completion script without running services")
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		c := cliCommand(shell, "Generate "+shell+" completion")
		c.RunE = func(c *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("completion %s accepts no operands", shell)
			}
			switch shell {
			case "bash":
				return root.GenBashCompletionV2(out, true)
			case "zsh":
				return root.GenZshCompletion(out)
			case "fish":
				return root.GenFishCompletion(out, true)
			default:
				return root.GenPowerShellCompletionWithDesc(out)
			}
		}
		completion.AddCommand(c)
	}
	root.AddCommand(completion)
	configureCLIHelp(root)
	return root
}

var scriptActions = []string{"plan", "trust-project", "allow", "revoke", "forget-project", "clean"}

func scriptsCommand(action string, out, stderr io.Writer) *cobra.Command {
	use := action + " [flags] PROJECT"
	if action == "" {
		use = "scripts [flags] PROJECT"
	}
	c := cliCommand(use, "Inspect or manage explicit build-script authorization")
	req := ScriptsRequest{Action: "plan"}
	if action != "" {
		req.Action = action
	}
	f := c.Flags()
	f.StringVar(&req.Action, "action", req.Action, "legacy action: plan|trust-project|allow|revoke|forget-project|clean")
	f.StringVar(&req.Subject, "subject", "", "exact SHA-256 host execution subject from plan")
	f.StringVar(&req.TargetDir, "target-dir", "", "managed project artifacts (default target or GOXIDE_TARGET_DIR)")
	f.StringVar(&req.GoTool, "go", "go", "Go 1.27+ compiler")
	f.StringVar(&req.Features, "features", "", "project features")
	f.BoolVar(&req.NoDefaultFeatures, "no-default-features", false, "disable root default features")
	f.BoolVar(&req.Offline, "offline", false, "use cached dependencies; does not sandbox scripts")
	f.BoolVar(&req.Locked, "locked", false, "require unchanged dependency lock")
	c.RunE = func(c *cobra.Command, args []string) error {
		if len(args) != 1 {
			return usageError(c, fmt.Errorf("exactly one PROJECT is required; put flags before the input"))
		}
		if action != "" && req.Action != action {
			return usageError(c, fmt.Errorf("--action %s conflicts with scripts %s", req.Action, action))
		}
		req.Root = args[0]
		return executeScripts(req, out, stderr)
	}
	return c
}
func inputError() error {
	return fmt.Errorf("exactly one .gox file or directory is required; put flags before the input; run program arguments require INPUT -- ARGS")
}

// UsageError preserves the underlying parse error for callers without selecting
// an exit code. Business errors retain their existing wrapping and diagnostics.
type UsageError struct {
	Cause error
	Usage string
}

func (e *UsageError) Error() string { return e.Cause.Error() + "\nusage: " + e.Usage }
func (e *UsageError) Unwrap() error { return e.Cause }
func usageError(c *cobra.Command, err error) error {
	return &UsageError{Cause: err, Usage: c.UseLine()}
}

func childCommand(c *cobra.Command, name string) *cobra.Command {
	for _, child := range c.Commands() {
		if child.Name() == name || child.HasAlias(name) {
			return child
		}
	}
	return nil
}

// Route only explicit leading command words, never scan operands for command
// names. In particular scripts -- plan and scripts plan retain legacy path use.
func routeCLI(root *cobra.Command, args []string) (*cobra.Command, []string, error) {
	if strings.HasPrefix(args[0], "-") {
		return root, args, nil
	}
	c := childCommand(root, args[0])
	if c == nil {
		return nil, nil, fmt.Errorf("unknown command %q; use goxide --help", args[0])
	}
	tail := args[1:]
	if c.Name() == "scripts" && len(tail) > 1 {
		if child := childCommand(c, tail[0]); child != nil {
			c, tail = child, tail[1:]
		}
	} else if c.Name() == "completion" && len(tail) > 0 && !strings.HasPrefix(tail[0], "-") {
		child := childCommand(c, tail[0])
		if child == nil {
			return nil, nil, fmt.Errorf("unknown completion shell %q", tail[0])
		}
		c, tail = child, tail[1:]
	}
	return c, tail, nil
}
func executeCLI(c *cobra.Command, args []string) error {
	if err := checkOptionTokens(c.Flags(), args); err != nil {
		return usageError(c, err)
	}
	if err := c.ParseFlags(args); err != nil {
		return usageError(c, err)
	}
	help, _ := c.Flags().GetBool("help")
	if help {
		return c.Help()
	}
	if c.RunE == nil {
		return fmt.Errorf("usage: %s; use --help", c.UseLine())
	}
	return c.RunE(c, c.Flags().Args())
}

// Reject historical Go spellings before pflag can interpret -offline as -o ffline.
// Values and everything after the first operand/-- are deliberately opaque.
var oldLongOptions = map[string]bool{
	"go": true, "offline": true, "features": true, "source-map": true,
	"expanded": true, "runtime": true, "roots": true, "tags": true, "no-default-features": true,
	"bin": true, "example": true, "lib": true, "target-dir": true, "locked": true,
	"action": true, "subject": true, "plan": true, "out": true, "help": true,
	"source": true, "accept-source-diff": true,
	"package": true, "workspace": true, "member-bin": true, "member-lib": true,
}

func checkOptionTokens(f *pflag.FlagSet, args []string) error {
	for i := 0; i < len(args); i++ {
		token := args[i]
		if token == "--" || token == "-" || !strings.HasPrefix(token, "-") {
			return nil
		}
		needsValue := false
		if strings.HasPrefix(token, "--") {
			name, _, assigned := strings.Cut(token[2:], "=")
			flag := f.Lookup(name)
			if flag == nil {
				return fmt.Errorf("unknown flag: --%s", name)
			}
			needsValue = !assigned && flag.NoOptDefVal == ""
		} else {
			name, _, _ := strings.Cut(token[1:], "=")
			if oldLongOptions[name] {
				return fmt.Errorf("-%s is not supported; use --%s", name, name)
			}
			seen := map[byte]bool{}
			for j := 1; j < len(token); j++ {
				short := token[j : j+1]
				flag := f.ShorthandLookup(short)
				if flag == nil {
					return fmt.Errorf("unknown shorthand flag: %q in %s; long options require --name", short, token)
				}
				if j+1 < len(token) && token[j+1] == '=' {
					return fmt.Errorf("-%s=value is not supported; use --%s=value", short, flag.Name)
				}
				if flag.NoOptDefVal == "" {
					needsValue = j == len(token)-1
					break
				}
				if seen[token[j]] {
					return fmt.Errorf("repeated short option -%s in %s is not supported", short, token)
				}
				seen[token[j]] = true
			}
		}
		if needsValue {
			if i+1 == len(args) || args[i+1] == "--" {
				return fmt.Errorf("flag needs an argument: %s", token)
			}
			i++
		}
	}
	return nil
}
