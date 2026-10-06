// Package frontend implements the deliberately small executable macro prototype.
package frontend

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/hxsf/goxide/internal/deriveextras"
	"github.com/hxsf/goxide/internal/enums"
	"github.com/hxsf/goxide/internal/equality"
	"github.com/hxsf/goxide/internal/macros"
	"github.com/hxsf/goxide/internal/matchexpr"
	"github.com/hxsf/goxide/internal/printing"
	"github.com/hxsf/goxide/internal/propagate"
	"github.com/hxsf/goxide/internal/refs"
	"github.com/hxsf/goxide/internal/sourcemap"
	"github.com/hxsf/goxide/internal/typecheck"
	"go/ast"
	"go/format"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type Result struct {
	typeFunctions           []*ast.FuncDecl
	typeImports             []*ast.ImportSpec
	fieldProtocols          map[string][]string
	defaultRegistrations    map[string]string
	manualEquality          map[string]manualEquality
	matchReferenceTemps     map[string]bool
	Diagnostics             *sourcemap.Map
	Go, SourceMap, Expanded []byte
	Enums                   []enums.Decl
	References              []refs.Reference
	conversions             []propagate.Conversion
	models                  map[string]model
	debugTypes              map[string]bool
	typeSpecs               map[string]*ast.TypeSpec
	defaultHelpers          map[string]bool
	equalityDerives         map[string]equality.Derive
}
type attrs struct {
	derive          map[string]bool
	rename          string
	message         string
	source, from    bool
	hasRename       bool
	hasMessage      bool
	skip, def, deny bool
}
type field struct {
	name, wire string
	typ        ast.Expr
	skip, def  bool
	line       int
}
type model struct {
	enum         *enums.Decl
	name         string
	fields       []field
	attrs        attrs
	line         int
	spec         *ast.TypeSpec
	source, from string
}
type generator struct {
	sumExternal  []externalSumProtocol
	protocolPkg  *types.Package
	protocolFile *ast.File
	protocolSums []enums.Decl
	sumHelpers   map[string]string
	sumImports   map[string]string
	sumError     error

	localProtocols     map[string]map[string]string
	deriveFile         int
	importedFields     map[string]string
	importedDefaults   map[string]string
	defaultImportPaths map[string]string
	enumPosition       *token.Position
	provenance         *sourcemap.Map
	fs                 *token.FileSet
	file               string
	models             map[string]model
	enumDecls          map[string]*enums.Decl
	alias              string
	helpers            strings.Builder
	next               int
}

var attribute = regexp.MustCompile(`^\s*#\[(.*)\]\s*$`)
var renameAttr = regexp.MustCompile(`^rename\s*=\s*("(?:[^"\\]|\\.)*")$`)

func Compile(filename string, source []byte) (Result, error) {
	return CompileWithOptions(filename, source, FileOptions{})
}

type compileConfig struct {
	single            bool
	equalityImports   []equality.Protocol
	uses              *usePackage
	protocolFunctions map[string][]*ast.FuncDecl
	sumDeclarations   []enums.Decl
	packagePath       string
	enumRegistry      map[string]PackageResult
	localProtocols    map[string]map[string]string
	fieldImports      map[string]map[string]string
	defaultImports    map[string]map[string]string
	builtinTypes      map[string]macros.TypeContext
	macros            *macros.Package
	macroImporter     types.Importer
	macroBudget       *macros.Budget
	declarations      []enums.Decl
	named             map[string]ast.Expr
	models            map[string]model
	collect           bool
	imported          map[string][]enums.Decl
	preludes          map[string]string
	enumGoImports     map[string]string
	enumUnavailable   map[string]map[string]string
	manualDefaults    map[string]manualDefault
	manualEquality    map[string]manualEquality
	typeSpecs         map[string]*ast.TypeSpec
}

func compile(filename string, source []byte, cfg *compileConfig, options ...FileOptions) (Result, error) {
	single := cfg == nil || cfg.single
	packagePath := "goxide/generated"
	var sharedImporter types.Importer
	if cfg != nil {
		sharedImporter = cfg.macroImporter
		if cfg.packagePath != "" {
			packagePath = cfg.packagePath
		}
	} else if len(options) > 0 {
		sharedImporter = options[0].Importer
	}
	if sharedImporter == nil {
		sharedImporter = typecheck.NewImporter(filename)
	}
	if bytes.IndexByte(source, 0) >= 0 {
		return Result{}, fmt.Errorf("%s: NUL byte in source", filename)
	}
	originalSource := append([]byte(nil), source...)
	provenance := sourcemap.Identity(filename, source)
	// Reject user access to reserved generated names before adding enum helpers.
	reserveFS := token.NewFileSet()
	reserveFile := reserveFS.AddFile(filename, -1, len(source))
	var reserveScanner scanner.Scanner
	reserveScanner.Init(reserveFile, source, func(token.Position, string) {}, 0)
	enumHeader, enumDepth := false, 0
	for {
		pos, tok, lit := reserveScanner.Scan()
		if tok == token.EOF {
			break
		}
		if tok == token.IDENT && lit == "enum" {
			enumHeader = true
		}
		if tok == token.LBRACE && (enumHeader || enumDepth > 0) {
			enumDepth++
			enumHeader = false
		}
		if tok == token.RBRACE && enumDepth > 0 {
			enumDepth--
		}
		if tok == token.IDENT && strings.HasPrefix(lit, "__goxide_") {
			return Result{}, fmt.Errorf("%s: reserved generated identifier %s", reserveFS.Position(pos), lit)
		}
	}
	prepared, err := propagate.PrepareMapped(filename, source, provenance)
	if err != nil {
		return Result{}, err
	}
	preparedMap := provenance
	macroOrigins := macroSourceMapper{filename: filename, original: originalSource, prepared: preparedMap}
	var expanded macros.Result
	if cfg != nil && cfg.macros != nil {
		expanded, err = macros.ExpandPackage(filename, prepared, []string{"__goxide_question"}, cfg.macros, sharedImporter)
	} else {
		expanded, err = macros.ExpandPackage(filename, prepared, []string{"__goxide_question"}, nil, sharedImporter)
	}
	if err != nil {
		var diagnostic *macros.Diagnostic
		if errors.As(err, &diagnostic) {
			origin := macroOrigins.point(sourcemap.Span{File: diagnostic.Position.Filename, Start: diagnostic.Position.Offset, End: diagnostic.End, Line: diagnostic.Position.Line, Column: diagnostic.Position.Column}, diagnostic.Original)
			stack := macroOrigins.stack(diagnostic.Stack)
			return Result{}, &sourcemap.Error{Message: "macro: " + diagnostic.Message, Origin: origin, Stack: stack, Generated: diagnostic.Position}
		}
		return Result{}, err
	}
	if cfg != nil && cfg.macroBudget != nil {
		if err := cfg.macroBudget.Add(expanded); err != nil {
			return Result{}, fmt.Errorf("%s: %w", filename, err)
		}
	}
	if len(expanded.Origins) > 0 {
		provenance = &sourcemap.Map{Source: expanded.Source, Stages: append(provenance.Stages, "macros.Expand")}
		for _, o := range expanded.Origins {
			origin := macroOrigins.point(sourcemap.Span{File: o.File, Start: o.SourceStart, End: o.SourceEnd, Line: o.Line, Column: o.Column}, o.Original)
			stack := macroOrigins.stack(o.Stack)
			kind := "source"
			if origin == nil {
				kind = "generated/unmapped"
			} else if len(stack) > 0 {
				kind = "macro"
			}
			provenance.Attach(o.OutputStart, o.OutputEnd, origin, stack, kind)
		}
	}
	manualSource, manual, err := lowerManualDefaults(filename, expanded.Source)
	if err != nil {
		return Result{}, err
	}
	for n, entry := range manual {
		entry.origin = provenance.SpanFor(entry.position.Offset, entry.position.Offset+4)
		if seg := provenance.Lookup(entry.position.Offset); seg != nil {
			entry.stack = seg.Stack
		}
		manual[n] = entry
	}
	provenance = provenance.Compose(expanded.Source, manualSource, "manualDefault.Lower")
	equalitySource, manualEqualities, err := lowerManualEquality(filename, manualSource, provenance)
	if err != nil {
		return Result{}, err
	}
	source, err = matchexpr.LowerMapped(filename, equalitySource, provenance)
	if err != nil {
		return Result{}, stageError("matchexpr.Lower", err)
	}
	if cfg != nil {
		if err = checkUnavailableEnumSyntax(filename, source, cfg.enumUnavailable[filename]); err != nil {
			return Result{}, stageError("enums.import", err)
		}
	}
	// Enum metadata retains positions from the input of this lowering.
	enumDiagnosticMap := *provenance
	failEnum := func(position token.Position, message string) (Result, error) {
		d := &sourcemap.Error{Message: message, Generated: position}
		if position.Filename == filename {
			if segment := enumDiagnosticMap.Lookup(position.Offset); segment != nil {
				d.Origin, d.Stack = segment.Origin, segment.Stack
			}
		}
		return Result{}, d
	}
	var enumResult enums.Result
	if cfg == nil {
		enumResult, err = enums.LowerMapped(filename, source, provenance)
	} else {
		enumResult, err = enums.LowerPackage(filename, source, provenance, append(append([]enums.Decl{}, cfg.declarations...), cfg.imported[filename]...), cfg.named)
	}
	if err != nil {
		return Result{}, stageError("enums.Lower", err)
	}
	source = enumResult.Source
	if cfg != nil {
		source, err = insertEnumImports(filename, source, cfg.enumGoImports[filename], provenance)
		if err != nil {
			return Result{}, err
		}
		source = append(source, []byte(cfg.preludes[filename])...)
	}
	refResult, err := refs.Prepare(filename, source)
	if err != nil {
		return Result{}, stageError("refs.Prepare", err)
	}
	provenance = provenance.Compose(source, refResult.Source, "refs.Prepare")
	source = refResult.Source
	// Line-based derive diagnostics refer to this stage, before attributes are
	// erased and generated helpers are appended. Capture its map now rather than
	// interpreting these line numbers in a later lowering's byte offsets.
	diagnosticMap := provenance
	diagnosticSource := source
	diagnosticFile := token.NewFileSet().AddFile(filename, -1, len(source))
	diagnosticFile.SetLinesForContent(source)
	fail := func(line int, msg string) (Result, error) {
		d := &sourcemap.Error{Message: msg, Generated: token.Position{Filename: filename, Line: line, Column: 1}}
		if line > 0 && line <= diagnosticFile.LineCount() {
			offset := diagnosticFile.Offset(diagnosticFile.LineStart(line))
			for offset < len(diagnosticSource) && (diagnosticSource[offset] == ' ' || diagnosticSource[offset] == '\t' || diagnosticSource[offset] == '\r') {
				offset++
			}
			d.Generated = diagnosticFile.Position(diagnosticFile.Pos(offset))
			if offset < len(diagnosticSource) && diagnosticSource[offset] != '\n' {
				if segment := diagnosticMap.Lookup(offset); segment != nil {
					d.Origin, d.Stack = segment.Origin, segment.Stack
				}
			}
		}
		return Result{}, d
	}
	failModel := func(m model, message string) (Result, error) {
		if m.enum != nil {
			return failEnum(m.enum.Position, message)
		}
		return fail(m.line, message)
	}
	lines := strings.Split(string(source), "\n")
	protected := protectedLines(source)
	pending := attrs{}
	byLine := map[int]attrs{}
	for i, l := range lines {
		if protected[i+1] {
			continue
		}
		if m := attribute.FindStringSubmatch(l); m != nil {
			body := m[1]
			if strings.HasPrefix(body, "derive(") && strings.HasSuffix(body, ")") {
				if pending.derive != nil {
					return fail(i+1, "duplicate derive")
				}
				pending.derive = map[string]bool{}
				for _, x := range strings.Split(body[7:len(body)-1], ",") {
					x = strings.TrimSpace(x)
					if x != "Serialize" && x != "Deserialize" && x != "Debug" && x != "Clone" && x != "Default" && x != "Error" && x != "PartialEq" && x != "Eq" {
						return fail(i+1, "unsupported derive "+x)
					}
					if pending.derive[x] {
						return fail(i+1, "duplicate derive "+x)
					}
					pending.derive[x] = true
				}
			} else if strings.HasPrefix(body, "error(") && strings.HasSuffix(body, ")") {
				v, e := strconv.Unquote(body[6 : len(body)-1])
				if e != nil {
					return fail(i+1, "error requires a quoted message")
				}
				if pending.hasMessage {
					return fail(i+1, "duplicate error message")
				}
				pending.message = v
				pending.hasMessage = true
			} else if body == "source" {
				pending.source = true
			} else if body == "from" {
				pending.from = true
				pending.source = true
			} else if strings.HasPrefix(body, "serde(") && strings.HasSuffix(body, ")") {
				for _, x := range splitAttrs(body[6 : len(body)-1]) {
					x = strings.TrimSpace(x)
					switch x {
					case "skip":
						pending.skip = true
					case "default":
						pending.def = true
					case "deny_unknown_fields":
						pending.deny = true
					default:
						if m := renameAttr.FindStringSubmatch(x); m != nil {
							v, e := strconv.Unquote(m[1])
							if e != nil {
								return fail(i+1, e.Error())
							}
							if pending.hasRename {
								return fail(i+1, "duplicate rename")
							}
							pending.rename = v
							pending.hasRename = true
						} else {
							return fail(i+1, "unsupported serde attribute "+x)
						}
					}
				}
			} else {
				return fail(i+1, "unsupported attribute "+body)
			}
			lines[i] = ""
			continue
		}
		if strings.TrimSpace(l) == "" || strings.HasPrefix(strings.TrimSpace(l), "//") {
			continue
		}
		if pending.derive != nil || pending.hasRename || pending.skip || pending.def || pending.deny || pending.hasMessage || pending.source || pending.from {
			byLine[i+1] = pending
			pending = attrs{}
		}
		if strings.HasPrefix(strings.TrimSpace(l), "struct ") {
			lines[i] = strings.Replace(l, "struct ", "type ", 1)
			j := strings.Index(lines[i], "{")
			if j >= 0 {
				lines[i] = lines[i][:j] + "struct " + lines[i][j:]
			}
		}
	}
	if pending.derive != nil || pending.hasRename || pending.skip || pending.def || pending.deny || pending.hasMessage || pending.source || pending.from {
		return fail(len(lines), "attribute has no target")
	}
	// Attribute erasure and struct sugar preserve known unchanged line slices.
	parsedSource := []byte(strings.Join(lines, "\n"))
	parsedMap := &sourcemap.Map{Source: parsedSource, Stages: append(provenance.Stages, "attributes")}
	oldOffset, newOffset := 0, 0
	for i, oldLine := range strings.Split(string(source), "\n") {
		if oldLine == lines[i] {
			parsedMap.Segments = append(parsedMap.Segments, provenance.Copy(oldOffset, oldOffset+len(oldLine), newOffset)...)
		} else if strings.HasPrefix(strings.TrimSpace(oldLine), "struct ") {
			// The field body following the opening brace is copied verbatim.
			a, b := strings.Index(oldLine, "{"), strings.Index(lines[i], "{")
			if a >= 0 && b >= 0 {
				parsedMap.Segments = append(parsedMap.Segments, provenance.Copy(oldOffset+a, oldOffset+len(oldLine), newOffset+b)...)
			}
		}
		oldOffset += len(oldLine) + 1
		newOffset += len(lines[i]) + 1
	}
	provenance = parsedMap
	if cfg != nil && cfg.uses != nil {
		before := []byte(strings.Join(lines, "\n"))
		after, err := cfg.uses.rewrite(filename, before)
		if err != nil {
			return Result{}, err
		}
		provenance = provenance.Compose(before, after, "use binding rewrite")
		lines = strings.Split(string(after), "\n")
	}
	fs := token.NewFileSet()
	f, e := parser.ParseFile(fs, filename, strings.Join(lines, "\n"), parser.ParseComments)
	if e != nil {
		return Result{}, e
	}
	astEvidence := provenance.CaptureAST(fs, f)
	builtinImporter := sharedImporter
	var builtinNamed map[string]macros.TypeContext
	if cfg != nil {
		builtinNamed = cfg.builtinTypes
	}
	if err := macros.ResolveBuiltinsWithPath(packagePath, fs, f, expanded.Builtins, builtinImporter, builtinNamed); err != nil {
		if e, ok := err.(*macros.BuiltinBindingError); ok {
			mapped := &sourcemap.Error{Message: e.Message, Generated: e.Position}
			if seg := provenance.Lookup(e.Position.Offset); seg != nil {
				mapped.Origin = seg.Origin
				mapped.Stack = seg.Stack
			}
			return Result{}, mapped
		}
		return Result{}, stageError("macros.ResolveBuiltins", err)
	}
	g := generator{provenance: provenance, fs: fs, file: filename, models: map[string]model{}, alias: "__goxide_serde"}
	if cfg != nil {
		g.localProtocols = cfg.localProtocols
		g.importedDefaults = cfg.defaultImports[filename]
		g.importedFields = cfg.fieldImports[filename]
		names := make([]string, 0, len(cfg.builtinTypes))
		for name := range cfg.builtinTypes {
			names = append(names, name)
		}
		sort.Strings(names)
		for i, name := range names {
			if name == filename {
				g.deriveFile = i
				break
			}
		}
	}
	{
		g.defaultImportPaths = defaultImportPaths(f, builtinImporter)
		if original, e := parseSourceImports(filename, originalSource); e == nil {
			for alias, path := range defaultImportPaths(original, builtinImporter) {
				if g.defaultImportPaths[alias] == "" {
					g.defaultImportPaths[alias] = path
				}
			}
		}
	}
	var collision string
	ast.Inspect(f, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && strings.HasPrefix(id.Name, "__goxide_") && !strings.HasPrefix(id.Name, "__goxide_enum_") && !strings.HasPrefix(id.Name, "__goxide_match_expr_") && !strings.HasPrefix(id.Name, "__goxide_ref_") && !strings.HasPrefix(id.Name, "__goxide_go_result") && id.Name != "__goxide_question" && id.Name != "__goxide_print" && id.Name != "__goxide_println" {
			collision = id.Name
		}
		return true
	})
	if collision != "" {
		return fail(1, "reserved generated identifier "+collision)
	}
	enumByName := map[string]*enums.Decl{}
	for i := range enumResult.Enums {
		d := &enumResult.Enums[i]
		enumByName[d.Name] = d
	}
	if cfg != nil {
		for i := range cfg.declarations {
			d := &cfg.declarations[i]
			if enumByName[d.Name] == nil {
				enumByName[d.Name] = d
			}
		}
		for name, m := range cfg.models {
			g.models[name] = m
		}
	}
	g.enumDecls = enumByName
	var ordered []model
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		if gd.Lparen.IsValid() && byLine[fs.Position(gd.Pos()).Line].derive != nil {
			return fail(fs.Position(gd.Pos()).Line, "derive on a grouped type declaration is unsupported; annotate each type separately")
		}
		for _, sp := range gd.Specs {
			ts := sp.(*ast.TypeSpec)
			line := fs.Position(ts.Pos()).Line
			a := byLine[line]
			if a.derive == nil {
				a = byLine[fs.Position(gd.Pos()).Line]
			}
			if a.derive == nil {
				continue
			}
			delete(byLine, line)
			delete(byLine, fs.Position(gd.Pos()).Line)
			if a.hasRename || a.skip || a.def || a.source || a.from {
				return fail(line, "field attribute on type")
			}
			hasSerde := a.derive["Serialize"] || a.derive["Deserialize"]
			if a.deny && !hasSerde {
				return fail(line, "serde attributes require Serialize or Deserialize derive")
			}
			if ts.Assign.IsValid() {
				return fail(line, "derive on aliases is unsupported")
			}
			if ts.TypeParams != nil {
				return fail(line, "generic derive is not supported in this prototype")
			}
			if ed := enumByName[ts.Name.Name]; ed != nil {
				for kind := range a.derive {
					if kind != "Serialize" && kind != "Deserialize" && kind != "Debug" && kind != "Clone" && kind != "Default" && kind != "Error" && kind != "PartialEq" && kind != "Eq" {
						return failEnum(ed.Position, "unsupported enum derive "+kind)
					}
				}
				if a.deny || a.hasMessage {
					return failEnum(ed.Position, "enum serde and error attributes are unsupported")
				}
				m := model{name: ed.Name, attrs: a, line: ed.Position.Line, spec: ts, enum: ed}
				g.models[m.name] = m
				ordered = append(ordered, m)
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				equalityOnly := true
				for kind := range a.derive {
					if kind != "PartialEq" && kind != "Eq" {
						equalityOnly = false
					}
				}
				if equalityOnly {
					m := model{name: ts.Name.Name, attrs: a, line: line, spec: ts}
					g.models[m.name] = m
					ordered = append(ordered, m)
					continue
				}
				return fail(line, "derive requires struct; enum and aliases are not supported")
			}
			m := model{name: ts.Name.Name, attrs: a, line: line, spec: ts}
			seen := map[string]bool{}
			for _, af := range st.Fields.List {
				fl := fs.Position(af.Pos()).Line
				fa := byLine[fl]
				delete(byLine, fl)
				if (fa.hasRename || fa.skip || fa.def) && !hasSerde {
					return fail(fl, "serde attributes require Serialize or Deserialize derive")
				}
				if fa.derive != nil || fa.deny || fa.hasMessage {
					return fail(fl, "container attribute on field")
				}
				if len(af.Names) != 1 {
					return fail(fl, "derive requires one named field per declaration")
				}
				if af.Tag != nil {
					return fail(fl, "use serde attributes instead of Go field tags")
				}
				name := af.Names[0].Name
				if name == "_" {
					return fail(fl, "blank fields cannot be derived")
				}
				wire := name
				if fa.hasRename {
					wire = fa.rename
				}
				if seen[wire] {
					return fail(fl, "duplicate serialized field "+wire)
				}
				seen[wire] = true
				if fa.source {
					if m.source != "" {
						return fail(fl, "multiple source fields")
					}
					m.source = name
				}
				if fa.from {
					m.from = name
				}
				m.fields = append(m.fields, field{name, wire, af.Type, fa.skip, fa.def, fl})
			}
			g.models[m.name] = m
			ordered = append(ordered, m)
		}
	}
	if len(byLine) > 0 {
		for line := range byLine {
			return fail(line, "attribute on unsupported target")
		}
	}

	// Public derive helper names may not replace user declarations.
	declared := map[string]bool{}
	for _, d := range f.Decls {
		switch x := d.(type) {
		case *ast.FuncDecl:
			if x.Recv == nil {
				declared[x.Name.Name] = true
			}
		case *ast.GenDecl:
			for _, sp := range x.Specs {
				switch v := sp.(type) {
				case *ast.TypeSpec:
					declared[v.Name.Name] = true
				case *ast.ValueSpec:
					for _, n := range v.Names {
						declared[n.Name] = true
					}
				}
			}
		}
	}
	for _, m := range ordered {
		needsDefault := m.attrs.derive["Default"]
		if m.attrs.derive["Deserialize"] {
			for _, field := range m.fields {
				needsDefault = needsDefault || field.def || field.skip
			}
		}
		for _, n := range []string{"bool", "string", "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "float32", "float64", "byte", "rune", "error"} {
			if declared[n] || (needsDefault && cfg != nil && cfg.named[n] != nil) {
				return failModel(m, "predeclared type shadowing is unsupported by derive: "+n)
			}
		}
		if m.attrs.derive["Error"] {
			for _, f := range m.fields {
				if f.name == "Error" || (f.name == "Unwrap" && m.source != "") {
					return fail(f.line, "derived method conflicts with field "+f.name)
				}
			}
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Recv == nil {
					continue
				}
				rt := fd.Recv.List[0].Type
				if p, ok := rt.(*ast.StarExpr); ok {
					rt = p.X
				}
				id, ok := rt.(*ast.Ident)
				if ok && id.Name == m.name && (fd.Name.Name == "Error" || (m.source != "" && fd.Name.Name == "Unwrap")) {
					return failModel(m, "derived method conflicts with existing "+fd.Name.Name)
				}
			}
		}
	}
	for _, m := range ordered {
		for _, kind := range []string{"Serialize", "Deserialize", "Debug", "Clone", "Default"} {
			if m.attrs.derive[kind] && declared[kind+m.name] {
				return failModel(m, "generated helper conflicts with declaration "+kind+m.name)
			}
		}
		if m.from != "" && declared["From"+m.name] {
			return failModel(m, "generated helper conflicts with declaration From"+m.name)
		}
	}
	for _, d := range enumResult.Enums {
		if err := enums.ValidateSerdeMapping(d); err != nil {
			return Result{}, err
		}
		for _, v := range d.Variants {
			if (v.HasSerdeRename || v.HasSerdeValue) && !g.models[d.Name].attrs.derive["Serialize"] && !g.models[d.Name].attrs.derive["Deserialize"] {
				return failEnum(d.Position, "variant serde attributes require Serialize or Deserialize derive")
			}
			if v.Default && !g.models[d.Name].attrs.derive["Default"] {
				return failEnum(d.Position, "default attribute requires Default derive")
			}
			if (v.HasMessage || v.Source || v.From) && !g.models[d.Name].attrs.derive["Error"] {
				return failEnum(d.Position, "error attributes require Error derive")
			}
		}
	}
	// Variant error attributes are explicit and never infer a dynamic source case.
	for i, m := range ordered {
		if m.enum == nil {
			continue
		}
		for _, v := range m.enum.Variants {
			if !m.attrs.derive["Error"] && (v.HasMessage || v.Source || v.From) {
				return failModel(m, "error attributes require Error derive")
			}
			if m.attrs.derive["Error"] && !v.HasMessage {
				return failModel(m, "enum Error requires error message on every variant")
			}
			if v.From {
				if m.from != "" {
					return failModel(m, "ambiguous enum From: multiple error variants")
				}
				m.from = v.Name
			}
		}
		ordered[i] = m
		g.models[m.name] = m
		if m.from != "" && declared["From"+m.name] {
			return failModel(m, "generated helper conflicts with declaration From"+m.name)
		}
	}
	if cfg != nil && cfg.collect {
		var functions []*ast.FuncDecl
		for _, declaration := range f.Decls {
			if fn, ok := declaration.(*ast.FuncDecl); ok {
				signature := *fn
				signature.Body = nil
				signature.Doc = nil
				functions = append(functions, &signature)
			}
		}
		return Result{models: g.models, typeSpecs: typeSpecs(f), typeImports: f.Imports, typeFunctions: functions, Enums: enumResult.Enums, manualEquality: manualEqualities}, nil
	}
	allManual := manual
	if cfg != nil {
		allManual = cfg.manualDefaults
	}
	specs := typeSpecs(f)
	if cfg != nil {
		for n, sp := range cfg.typeSpecs {
			if specs[n] == nil {
				specs[n] = sp
			}
		}
	}
	for n, entry := range allManual {
		spec := specs[n]
		if spec == nil || spec.Assign.IsValid() || spec.TypeParams != nil {
			return Result{}, fmt.Errorf("%s: manual Default requires a local defined non-generic struct: %s", entry.position, n)
		}
		if _, ok := spec.Type.(*ast.StructType); !ok {
			return Result{}, fmt.Errorf("%s: manual Default target must be a struct: %s", entry.position, n)
		}
		m := g.models[n]
		if m.attrs.derive["Default"] {
			return Result{}, fmt.Errorf("%s: duplicate Default implementation (manual and derive): %s", entry.position, n)
		}
		derives := map[string]bool{}
		for k, v := range m.attrs.derive {
			derives[k] = v
		}
		derives["Default"] = true
		m.attrs.derive = derives
		m.name = n
		g.models[n] = m
	}
	if cfg == nil {
		local := &compileConfig{
			builtinTypes: map[string]macros.TypeContext{filename: {Types: specs, Imports: f.Imports}},
			models:       g.models, manualDefaults: allManual,
		}
		if _, err := bindLocalProtocolAliases(packagePath, map[string][]byte{filename: originalSource}, local, nil, sharedImporter); err != nil {
			return Result{}, err
		}
		g.localProtocols = local.localProtocols
	}
	equalityDerives := map[string]equality.Derive{}
	for _, m := range ordered {
		if m.attrs.derive["PartialEq"] || m.attrs.derive["Eq"] {
			equalityDerives[m.name] = equality.Derive{PartialEq: m.attrs.derive["PartialEq"], Eq: m.attrs.derive["Eq"]}
		}
	}
	allEqualityManual := manualEqualities
	if cfg != nil {
		allEqualityManual = map[string]manualEquality{}
		for n, e := range cfg.manualEquality {
			allEqualityManual[n] = e
		}
		// The final pass owns local positions; the declaration collection pass
		// supplies other files' explicit registrations for conflict checks.
		for n, e := range manualEqualities {
			prior := allEqualityManual[n]
			if e.partial != nil {
				prior.partial = e.partial
			}
			if e.eq != nil {
				prior.eq = e.eq
			}
			allEqualityManual[n] = prior
		}
	}
	if err = applyManualEquality(filename, equalityDerives, allEqualityManual, specs, g.models); err != nil {
		return Result{}, err
	}
	// Shared sums need real imported type identity even in single-file derives.
	g.prepareSumProtocols(packagePath, f, cfg, builtinImporter)
	fieldProtocols := map[string][]string{}
	for _, m := range ordered {
		for _, kind := range []string{"Clone", "Debug", "Serialize", "Deserialize"} {
			if m.attrs.derive[kind] {
				fieldProtocols[m.name] = append(fieldProtocols[m.name], kind)
			}
		}
	}
	defaultRegistrations := map[string]string{}
	for _, m := range ordered {
		if m.attrs.derive["Default"] {
			defaultRegistrations[m.name] = "derive"
		}
	}
	for n := range manual {
		defaultRegistrations[n] = "manual"
	}
	defaultHelpers := map[string]bool{}
	for n, m := range g.models {
		if m.attrs.derive["Default"] {
			defaultHelpers["Default"+n] = true
		}
	}

	conversions := []propagate.Conversion{}
	for _, m := range ordered {
		if m.from != "" {
			conversions = append(conversions, propagate.Conversion{Source: "error", Target: m.name, Helper: "From" + m.name})
		}
	}
	imports := map[string]string{}
	for _, m := range ordered {
		if m.attrs.derive["Serialize"] || m.attrs.derive["Deserialize"] {
			imports[g.alias] = "github.com/goxide-lang/serde"
			if e := g.emitModel(m); e != nil {
				return Result{}, e
			}
		}
		if (m.attrs.hasMessage || m.source != "") && !m.attrs.derive["Error"] {
			return failModel(m, "error attributes require Error derive")
		}
		for _, kind := range []string{"Debug", "Clone", "Default", "Error"} {
			if !m.attrs.derive[kind] {
				continue
			}
			var code string
			var im map[string]string
			var e error
			if kind != "Error" {
				for _, f := range m.fields {
					if e := g.validateExtra(f.typ, kind); e != nil {
						return Result{}, e
					}
				}
			}
			if m.enum != nil {
				for _, v := range m.enum.Variants {
					if kind != "Error" && (kind != "Default" || v.Default) && v.Type != nil {
						g.enumPosition = &v.Position
						err := g.validateExtra(v.Type, kind)
						g.enumPosition = nil
						if err != nil {
							return Result{}, err
						}
					}
				}
				if kind == "Error" {
					code, im, e = deriveextras.GenerateEnumError(*m.enum)
				} else {
					code, im, e = deriveextras.GenerateEnum(kind, *m.enum, g.deriveProtocol)
				}
			} else if kind == "Error" {
				code, im, e = deriveextras.GenerateError(m.spec, m.attrs.message, m.source, m.from)
			} else {
				code, im, e = deriveextras.Generate(kind, m.spec, g.deriveProtocol)
			}
			if e != nil {
				return failModel(m, e.Error())
			}
			g.helpers.WriteString("\n" + code)
			for k, v := range im {
				imports[k] = v
			}
		}
	}
	if g.sumError != nil {
		return Result{}, g.sumError
	}
	for alias, path := range g.sumImports {
		imports[alias] = path
	}
	var original bytes.Buffer
	if e := format.Node(&original, fs, f); e != nil {
		return Result{}, e
	}
	// Insert a distinct import declaration after package; other imports remain intact.
	provenance = astEvidence.Print(f, original.Bytes(), "format.original")
	text := original.String()
	headerFS := token.NewFileSet()
	header, err := parser.ParseFile(headerFS, filename, text, parser.PackageClauseOnly)
	if err != nil {
		return Result{}, fmt.Errorf("generated package clause: %w", err)
	}
	// Leading cfg/build comments are not the package clause. Insert only after
	// the AST-confirmed package name, retaining the original provenance ranges.
	idx := headerFS.Position(header.Name.End()).Offset - 1
	var ib strings.Builder
	keys := []string{}
	for k := range imports {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&ib, "\nimport %s %q\n", k, imports[k])
	}
	joinedMap := &sourcemap.Map{Stages: append(provenance.Stages, "derive.generate")}
	joinedMap.Segments = append(joinedMap.Segments, provenance.Copy(0, idx+1, 0)...)
	joinedMap.Segments = append(joinedMap.Segments, provenance.Copy(idx+1, len(text), idx+1+ib.Len())...)
	generatedStart := len(text) + ib.Len()
	text = text[:idx+1] + ib.String() + text[idx+1:] + g.helpers.String()

	joinedMap.Source = []byte(text)
	provenance = joinedMap
	attachDeriveOrigins(provenance, []byte(text), ordered, g.provenance, fs, generatedStart)
	out, e := format.Source([]byte(text))
	if e != nil {
		return Result{}, fmt.Errorf("internal expansion error: %w\n%s", e, text)
	}
	provenance = provenance.Format([]byte(text), out, "format.generated")
	attachManualDefaultOrigins(provenance, out, manual)
	debugTypes := map[string]bool{}
	for _, m := range ordered {
		if m.attrs.derive["Debug"] {
			debugTypes[m.name] = true
		}
	}
	needsTypecheck := len(defaultHelpers) > 0 || bytes.Contains(out, []byte("__goxide_enum_infer(")) || bytes.Contains(out, []byte("__goxide_match_expr_")) || bytes.Contains(out, []byte("__goxide_print"))
	for _, d := range enumResult.Enums {
		needsTypecheck = needsTypecheck || d.Family != ""
	}
	referenceIR := out
	var importedBindings refs.ImportBindings
	singleImporter := sharedImporter
	if single {
		for _, spec := range f.Imports {
			path, _ := strconv.Unquote(spec.Path.Value)
			pkg, err := singleImporter.Import(path)
			if err == nil && pkg != nil && pkg.Scope().Lookup(useMetadataMarker) != nil {
				return Result{}, fmt.Errorf("%s: imports with use metadata require directory/package compilation: %s", filename, path)
			}
		}
		bound, bindings, err := refs.BindImportsWithPath(packagePath, map[string][]byte{filename: out}, singleImporter, nil)
		importedBindings = bindings
		if err != nil {
			if e, ok := err.(*refs.ImportError); ok {
				mapped := &sourcemap.Error{Message: e.Message, Generated: e.Position}
				if seg := provenance.Lookup(e.Position.Offset); seg != nil {
					mapped.Origin = seg.Origin
					mapped.Stack = seg.Stack
				}
				return Result{}, mapped
			}
			return Result{}, err
		}
		provenance = provenance.Compose(out, bound[filename], "refs.BindImports")
		out = bound[filename]
	}
	singleFamilies := append([]enums.Decl(nil), enumResult.Enums...)
	if cfg != nil {
		singleFamilies = append(singleFamilies, cfg.imported[filename]...)
	}
	singleEqCfg := equality.Config{Importer: singleImporter, PackagePath: packagePath, Derives: equalityDerives, Enums: singleFamilies, Maps: map[string]*sourcemap.Map{filename: provenance}}
	if cfg != nil {
		singleEqCfg.Imported = cfg.equalityImports
	}
	if single {
		eqFiles, err := equality.Prepare(map[string][]byte{filename: out}, singleEqCfg)
		if err != nil {
			return Result{}, stageError("equality.Prepare", err)
		}
		out = eqFiles[filename]
		provenance = singleEqCfg.Maps[filename]
		needsTypecheck = needsTypecheck || len(equalityDerives) > 0 || bytes.Contains(originalSource, []byte("equals"))
	}
	matchReferenceTemps := map[string]bool{}
	if single && (bytes.Contains(out, []byte("__goxide_question(")) || needsTypecheck || bytes.Contains(out, []byte("__goxide_ref_"))) {
		conversions := []propagate.Conversion{}
		for _, m := range ordered {
			if m.from != "" {
				conversions = append(conversions, propagate.Conversion{Source: "error", Target: m.name, Helper: "From" + m.name})
			}
		}
		imp := singleImporter
		var inferred []enums.Decl
		out, e = propagate.Lower(filename, out, propagate.Config{InferredFamilies: &inferred, Families: singleFamilies, From: conversions, Importer: imp, PackagePath: packagePath, Provenance: provenance, MatchReferenceTemps: matchReferenceTemps})
		if e != nil {
			return Result{}, stageError("propagate.Lower", e)
		}
		enumResult.Enums = append(enumResult.Enums, inferred...)
		singleEqCfg.Enums = append(singleEqCfg.Enums, inferred...)
		eqFiles, err := equality.Lower(map[string][]byte{filename: out}, singleEqCfg)
		if err != nil {
			return Result{}, stageError("equality.Lower", err)
		}
		out = eqFiles[filename]
		provenance = singleEqCfg.Maps[filename]
		if err := checkEnumEquality(packagePath, map[string][]byte{filename: out}, imp, singleEqCfg.Enums, true); err != nil {
			return Result{}, err
		}
		printed, err := printing.Lower(map[string][]byte{filename: out}, printing.Config{Importer: imp, PackagePath: packagePath, DebugTypes: debugTypes, Maps: map[string]*sourcemap.Map{filename: provenance}})
		if err != nil {
			return Result{}, err
		}
		out = printed[filename]
		out = unusedValidatorOwnerImports(filename, out, provenance)
		beforeReferences := out
		refResult, e = refs.FinishWithPackagePath(packagePath, filename, out, imp, matchReferenceTemps)
		if e != nil {
			return Result{}, stageError("refs.Finish", e)
		}
		refResult.Source, e = importedBindings.Restore(filename, refResult.Source)
		if e != nil {
			return Result{}, e
		}
		provenance = provenance.Compose(beforeReferences, refResult.Source, "refs.Finish")
		out = refResult.Source
		if e = typecheck.CheckPackage(packagePath, map[string][]byte{filename: out}, imp); e != nil {
			if te, ok := e.(types.Error); ok {
				p := te.Fset.Position(te.Pos)
				mapped := &sourcemap.Error{Message: te.Msg, Generated: p}
				if seg := provenance.Lookup(p.Offset); seg != nil {
					mapped.Origin = seg.Origin
					mapped.Stack = seg.Stack
				}
				return Result{}, mapped
			}
			return Result{}, stageError("typecheck.Check", e)
		}
		erased := map[string][]byte{filename: out}
		meta, e := refs.ExportMetadata(packagePath, map[string][]byte{filename: referenceIR}, erased, imp)
		if e != nil {
			return Result{}, e
		}
		if len(meta.Functions) > 0 {
			out = append(erased[filename], []byte(fmt.Sprintf("\nconst %s = %q\n", refs.MetadataMarker, meta.Marker()))...)
			if e = typecheck.CheckPackage(packagePath, map[string][]byte{filename: out}, imp); e != nil {
				return Result{}, e
			}
			provenance = provenance.Compose(refResult.Source, out, "refs.exportMetadata")
		}
	}

	if single {
		out = unusedValidatorOwnerImports(filename, out, provenance)
		if err := checkDefaultCycles(packagePath, map[string][]byte{filename: out}, sharedImporter, defaultHelpers, map[string]*sourcemap.Map{filename: provenance}); err != nil {
			return Result{}, err
		}
	}
	sm := []map[string]any{}
	deriveFields := []map[string]any{}
	for _, m := range ordered {
		for _, f := range m.fields {
			p := fs.Position(f.typ.Pos())
			origin := g.provenance.SpanFor(p.Offset, fs.Position(f.typ.End()).Offset)
			deriveFields = append(deriveFields, map[string]any{"model": m.name, "field": f.name, "origin": origin})
		}
	}
	for _, m := range ordered {
		for _, kind := range []string{"Serialize", "Deserialize", "Debug", "Clone", "Default"} {
			if m.attrs.derive[kind] {
				name := kind + m.name
				offset := bytes.Index(out, []byte("func "+name+"("))
				sm = append(sm, map[string]any{"source": filename, "line": m.line, "helper": name, "generatedByteOffset": offset})
			}
		}
		if m.attrs.derive["Error"] {
			sm = append(sm, map[string]any{"source": filename, "line": m.line, "helper": m.name + ".Error", "generatedByteOffset": bytes.Index(out, []byte("func (v "+m.name+") Error("))})
		}
	}
	mapping, _ := json.MarshalIndent(map[string]any{"derives": sm, "deriveFields": deriveFields, "macroTokens": expanded.Origins, "macroTokenInputStage": "propagate.Prepare output", "references": refResult.References, "diagnostics": provenance, "sourceBytes": len(originalSource), "note": "diagnostics uses final Go byte offsets; generated/unmapped regions have no proven source; macroTokens uses macro expansion offsets"}, "", "  ")
	if single {
		if err := checkEnumEquality(packagePath, map[string][]byte{filename: out}, singleImporter, singleEqCfg.Enums, true); err != nil {
			return Result{}, err
		}
	}
	return Result{fieldProtocols: fieldProtocols, defaultRegistrations: defaultRegistrations, equalityDerives: equalityDerives, matchReferenceTemps: matchReferenceTemps, Diagnostics: provenance, Go: out, Expanded: out, SourceMap: append(mapping, '\n'), Enums: enumResult.Enums, References: refResult.References, conversions: conversions, debugTypes: debugTypes, defaultHelpers: defaultHelpers}, nil
}
func (g *generator) typeName(t ast.Expr) string {
	var b bytes.Buffer
	_ = format.Node(&b, g.fs, t)
	return b.String()
}
func (g *generator) err(t ast.Expr, msg string) error {
	if g.enumPosition != nil {
		return fmt.Errorf("%s [generated/unmapped; enum stage coordinates]: %s", *g.enumPosition, msg)
	}
	p := g.fs.Position(t.Pos())
	e := &sourcemap.Error{Message: msg, Generated: p, Origin: g.provenance.SpanFor(p.Offset, g.fs.Position(t.End()).Offset)}
	if s := g.provenance.Lookup(p.Offset); s != nil {
		e.Stack = s.Stack
	}
	return e
}

func (g *generator) emitModel(m model) error {
	if m.enum != nil {
		return g.emitEnum(m)
	}
	var ser, des strings.Builder
	count := 0
	for _, f := range m.fields {
		if !f.skip {
			count++
		}
	}
	fmt.Fprintf(&ser, "\nfunc Serialize%s(s %s.Serializer,v *%s) error { if v==nil {return %s.NewError(\"Type\",\"nil struct pointer\")};w,e:=s.Struct(%q,%d);if e!=nil{return e};", m.name, g.alias, m.name, g.alias, m.name, count)
	fmt.Fprintf(&des, "\nfunc Deserialize%s(d %s.Deserializer)(v %s,e error){", m.name, g.alias, m.name)
	for i, f := range m.fields {
		if !f.skip {
			fmt.Fprintf(&des, "var seen%d bool;", i)
		}
	}
	fmt.Fprintf(&des, "e=d.Struct(%q,[]string{", m.name)
	for _, f := range m.fields {
		if !f.skip {
			fmt.Fprintf(&des, "%q,", f.wire)
		}
	}
	fmt.Fprintf(&des, "},%s.VisitorFuncs{OnMap:func(a %s.MapAccess)error{for{var k string;ok,e:=a.NextKey(func(d %s.Deserializer)error{var e error;k,e=%s.ReadString(d);return e});if e!=nil{return e};if !ok{break};switch k{", g.alias, g.alias, g.alias, g.alias)
	for i, f := range m.fields {
		if f.skip {
			fmt.Fprintf(&des, "case %q:if e:=a.NextValue(func(d %s.Deserializer)error{return d.Skip()});e!=nil{return e};", f.wire, g.alias)
			continue
		}
		s, r, e := g.codec(f.typ, m.attrs.derive["Serialize"], m.attrs.derive["Deserialize"])
		if e != nil {
			return e
		}
		fmt.Fprintf(&ser, "if e:=w.Field(%q,func(s %s.Serializer)error{return %s(s,v.%s)});e!=nil{return e};", f.wire, g.alias, s, f.name)
		fmt.Fprintf(&des, "case %q:if seen%d{return %s.NewError(\"DuplicateField\",\"duplicate field \"+k)};seen%d=(0==0);if e:=a.NextValue(func(d %s.Deserializer)error{var e error;v.%s,e=%s(d);return e});e!=nil{return e};", f.wire, i, g.alias, i, g.alias, f.name, r)
	}
	if m.attrs.deny {
		fmt.Fprintf(&des, "default:return %s.AtField(%s.NewError(\"UnknownField\",\"unknown field\"),k);", g.alias, g.alias)
	} else {
		des.WriteString("default:if e:=a.NextValue(func(d " + g.alias + ".Deserializer)error{return d.Skip()});e!=nil{return e};")
	}
	des.WriteString("}};")
	for i, f := range m.fields {
		_, optional := g.option(f.typ)
		if !f.skip && !f.def && !optional {
			fmt.Fprintf(&des, "if !seen%d{return %s.AtField(%s.NewError(\"MissingField\",\"required field missing\"),%q)};", i, g.alias, g.alias, f.wire)
		}
	}
	// Finish reading and validate required fields before constructing this
	// struct's missing/skipped fields. Nested visitor side effects are not rolled
	// back if an enclosing decode later fails.
	if m.attrs.derive["Deserialize"] {
		for i, f := range m.fields {
			_, optional := g.option(f.typ)
			if !f.def && !f.skip {
				// Ordinary missing Option fields follow the serde protocol, not
				// Default resolution. nil is the builtin's canonical None.
				if optional {
					fmt.Fprintf(&des, "if !seen%d{v.%s=nil};", i, f.name)
				}
				continue
			}
			if err := g.validateExtra(f.typ, "Default"); err != nil {
				return fmt.Errorf("serde default/skip field %s: %w", f.name, err)
			}
			value, err := deriveextras.DefaultValue(f.typ, func(t ast.Expr) (string, bool) { return g.deriveProtocol("Default", t) })
			// Explicit default/skip uses the builtin Option Default (None).
			// Avoid re-resolving type arguments inside the visitor scope.
			if optional {
				value = "nil"
			}
			if err != nil {
				return g.err(f.typ, fmt.Sprintf("serde default/skip field %s: %v", f.name, err))
			}
			if f.skip {
				fmt.Fprintf(&des, "v.%s=%s;", f.name, value)
			} else {
				fmt.Fprintf(&des, "if !seen%d{v.%s=%s};", i, f.name, value)
			}
		}
	}
	des.WriteString("return nil}});return v,e}\n")
	ser.WriteString("return w.End()}\n")
	if m.attrs.derive["Serialize"] {
		g.helpers.WriteString(ser.String())
	}
	if m.attrs.derive["Deserialize"] {
		g.helpers.WriteString(des.String())
	}
	return nil
}

// option recognizes only metadata registered by the builtin enum lowerer.
// Aliases, imported generic names and arbitrary shape-compatible types do not
// acquire Option's missing-field behavior.
func (g *generator) option(t ast.Expr) (*enums.Decl, bool) {
	indexed, ok := t.(*ast.IndexExpr)
	if !ok {
		return nil, false
	}
	name, ok := indexed.X.(*ast.Ident)
	if !ok || name.Name != "Option" {
		return nil, false
	}
	d := g.enumDecls[g.typeName(t)]
	return d, d != nil && d.Family == "Option"
}

func (g *generator) codec(t ast.Expr, wantS, wantR bool) (string, string, error) {
	g.next++
	s := fmt.Sprintf("__goxide_write%d", g.next)
	r := fmt.Sprintf("__goxide_read%d", g.next)
	typ := g.typeName(t)
	a := g.alias
	var w, b string
	switch t := t.(type) {
	case *ast.Ident:
		write, hasWrite := g.importedField("Serialize", t)
		read, hasRead := g.importedField("Deserialize", t)
		if hasWrite || hasRead {
			if wantS && !hasWrite || wantR && !hasRead {
				return "", "", g.err(t, "nested type lacks required registered serde protocol: "+typ)
			}
			w = "return " + write + "(s,&v)"
			b = "return " + read + "(d)"
			break
		}
		switch t.Name {
		case "bool", "string":
			method := map[string]string{"bool": "Bool", "string": "String"}[t.Name]
			w = "return s." + method + "(v)"
			b = "return " + a + ".Read" + method + "(d)"
		case "int", "int8", "int16", "int32", "int64":
			w = "return s.Int64(int64(v))"
			b = "x,e:=" + a + ".ReadInt64(d);if e!=nil{return v,e};v=" + typ + "(x);if int64(v)!=x{return v," + a + ".NewError(\"Range\",\"integer overflow\")};return v,nil"
		case "uint", "uint8", "uint16", "uint32", "uint64":
			w = "return s.Uint64(uint64(v))"
			b = "x,e:=" + a + ".ReadUint64(d);if e!=nil{return v,e};v=" + typ + "(x);if uint64(v)!=x{return v," + a + ".NewError(\"Range\",\"integer overflow\")};return v,nil"
		case "byte":
			return g.codec(ast.NewIdent("uint8"), wantS, wantR)
		case "rune":
			return g.codec(ast.NewIdent("int32"), wantS, wantR)
		case "float32", "float64":
			w = "return s.Float64(float64(v))"
			b = "x,e:=" + a + ".ReadFloat64(d);if e!=nil{return v,e};"
			if t.Name == "float32" {
				b += "if x>3.4028234663852886e38||x< -3.4028234663852886e38||(x!=0&&float32(x)==0){return v," + a + ".NewError(\"Range\",\"float32 overflow\")};"
			}
			b += "return " + typ + "(x),nil"
		default:
			m, ok := g.models[t.Name]
			if !ok {
				if g.enumDecls[t.Name] != nil {
					return "", "", g.err(t, "nested enum lacks required derive: "+typ)
				}
				return "", "", g.err(t, "unsupported field type "+typ)
			}
			if wantS && !m.attrs.derive["Serialize"] || wantR && !m.attrs.derive["Deserialize"] {
				return "", "", g.err(t, "nested type lacks required derive: "+typ)
			}
			w = "return Serialize" + typ + "(s,&v)"
			b = "return Deserialize" + typ + "(d)"
		}
	case *ast.SelectorExpr:
		write, hasWrite := g.importedField("Serialize", t)
		read, hasRead := g.importedField("Deserialize", t)
		if wantS && !hasWrite || wantR && !hasRead {
			return "", "", g.err(t, "nested type lacks required registered serde protocol: "+typ)
		}
		w = "return " + write + "(s,&v)"
		b = "return " + read + "(d)"
	case *ast.IndexExpr:
		option, ok := g.option(t)
		if !ok {
			return "", "", g.err(t, "serde supports only registered builtin Option[T] instantiations")
		}
		cs, cr, e := g.codec(t.Index, wantS, wantR)
		if e != nil {
			return "", "", e
		}
		// Bind the concrete case at package scope: payload type names may be s/d/v,
		// which are also ordinary helper parameter names. The alias is compiler-owned
		// and is namespaced with the other helpers during multi-file lowering.
		caseType := s + "_option_case"
		if wantS {
			fmt.Fprintf(&g.helpers, "type %s = %s\n", caseType, option.Variants[0].CaseName)
		}
		w = "switch item:=v.(type){case nil:return s.None();case " + caseType + ":return s.Some(func(s " + a + ".Serializer)error{return " + cs + "(s,item.Value)});default:return " + a + ".NewError(" + a + ".Type,\"invalid enum Option\") }"
		// Some has no phantom argument: infer its type from the statically typed
		// decoded payload, avoiding local names capturing a repeated type argument.
		some := strings.Split(option.Variants[0].Constructor, "[")[0]
		b = "e=d.Option(" + a + ".VisitorFuncs{OnNone:func()error{return nil},OnSome:func(d " + a + ".Deserializer)error{x,e:=" + cr + "(d);if e!=nil{return e};v=" + some + "(x);return nil}});return v,e"
	case *ast.StarExpr:
		cs, cr, e := g.codec(t.X, wantS, wantR)
		if e != nil {
			return "", "", e
		}
		w = "if v==nil{return s.None()};return s.Some(func(s " + a + ".Serializer)error{return " + cs + "(s,*v)})"
		b = "e=d.Option(" + a + ".VisitorFuncs{OnNone:func()error{return nil},OnSome:func(d " + a + ".Deserializer)error{x,e:=" + cr + "(d);if e!=nil{return e};v=&x;return nil}});return v,e"
	case *ast.ArrayType:
		cs, cr, e := g.codec(t.Elt, wantS, wantR)
		if e != nil {
			return "", "", e
		}
		w = "q,e:=s.Seq(len(v));if e!=nil{return e};for i:=range v{if e:=q.Element(func(s " + a + ".Serializer)error{return " + cs + "(s,v[i])});e!=nil{return e}};return q.End()"
		init := ""
		assign := "v=append(v,x)"
		finish := ""
		if t.Len != nil {
			init = "if i>=len(v){return " + a + ".NewError(\"Type\",\"too many array elements\")};"
			assign = "v[i]=x"
			finish = "if i!=len(v){return " + a + ".NewError(\"Type\",\"wrong array length\")};"
		} else {
			// Resolve allocation types outside reader parameter/result scope.
			containerType := r + "_container"
			if wantR {
				fmt.Fprintf(&g.helpers, "type %s = %s\n", containerType, typ)
			}
			b = "v=make(" + containerType + ",0);"
		}
		b += "e=d.Seq(" + a + ".VisitorFuncs{OnSeq:func(q " + a + ".SeqAccess)error{i:=0;for{ok,e:=q.NextElement(func(d " + a + ".Deserializer)error{" + init + "x,e:=" + cr + "(d);if e!=nil{return e};" + assign + ";return nil});if e!=nil{return e};if !ok{break};i++};" + finish + "return nil}});return v,e"
	case *ast.MapType:
		k, ok := t.Key.(*ast.Ident)
		if !ok || k.Name != "string" {
			return "", "", g.err(t, "only map[string]T is supported")
		}
		cs, cr, e := g.codec(t.Value, wantS, wantR)
		if e != nil {
			return "", "", e
		}
		w = "q,e:=s.Map(len(v));if e!=nil{return e};for k,x:=range v{if e:=q.Entry(func(s " + a + ".Serializer)error{return s.String(k)},func(s " + a + ".Serializer)error{return " + cs + "(s,x)});e!=nil{return e}};return q.End()"
		containerType := r + "_container"
		if wantR {
			fmt.Fprintf(&g.helpers, "type %s = %s\n", containerType, typ)
		}
		b = "v=make(" + containerType + ");e=d.Map(" + a + ".VisitorFuncs{OnMap:func(q " + a + ".MapAccess)error{for{var k string;ok,e:=q.NextKey(func(d " + a + ".Deserializer)error{var e error;k,e=" + a + ".ReadString(d);return e});if e!=nil{return e};if !ok{break};if _,ok:=v[k];ok{return " + a + ".AtField(" + a + ".NewError(\"DuplicateField\",\"duplicate map key\"),k)};if e:=q.NextValue(func(d " + a + ".Deserializer)error{x,e:=" + cr + "(d);if e!=nil{return e};v[k]=x;return nil});e!=nil{return e}};return nil}});return v,e"
	default:
		return "", "", g.err(t, "unsupported field type "+typ)
	}
	if wantS {
		fmt.Fprintf(&g.helpers, "\nfunc %s(s %s.Serializer,v %s)error{%s}\n", s, a, typ, w)
	}
	if wantR {
		fmt.Fprintf(&g.helpers, "\nfunc %s(d %s.Deserializer)(v %s,e error){%s}\n", r, a, typ, b)
	}
	return s, r, nil
}

// splitAttrs respects commas inside quoted attribute strings.
func splitAttrs(s string) []string {
	var out []string
	start := 0
	quoted, escape := false, false
	for i, c := range s {
		if escape {
			escape = false
			continue
		}
		if quoted && c == '\\' {
			escape = true
			continue
		}
		if c == '"' {
			quoted = !quoted
		}
		if c == ',' && !quoted {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}
func protectedLines(source []byte) map[int]bool {
	fs := token.NewFileSet()
	f := fs.AddFile("", -1, len(source))
	var s scanner.Scanner
	s.Init(f, source, func(token.Position, string) {}, scanner.ScanComments)
	out := map[int]bool{}
	for {
		p, t, l := s.Scan()
		if t == token.EOF {
			break
		}
		if (t == token.STRING || t == token.COMMENT) && strings.Contains(l, "\n") {
			first := f.Position(p).Line
			for n := 0; n <= strings.Count(l, "\n"); n++ {
				out[first+n] = true
			}
		}
	}
	return out
}
func (g *generator) validateExtra(t ast.Expr, kind string) error {
	if kind == "Default" || kind == "Clone" || kind == "Debug" {
		if _, ok, err := g.sumProtocol(kind, t); ok || err != nil {
			return err
		}
	}
	if kind == "Clone" || kind == "Debug" {
		if _, ok := g.importedField(kind, t); ok {
			return nil
		}
	}
	if kind == "Default" {
		if _, ok := g.importedDefault(t); ok {
			return nil
		}
		if _, ok := t.(*ast.Ident); !ok {
			return g.err(t, "Default requires an explicit implementation; containers, references and Result are unsupported")
		}
	}
	switch x := t.(type) {
	case *ast.Ident:
		switch x.Name {
		case "bool", "string", "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "float32", "float64", "byte", "rune":
			return nil
		}
		m, ok := g.models[x.Name]
		if !ok || !m.attrs.derive[kind] {
			return g.err(t, "nested type requires "+kind+" derive or explicit implementation: "+x.Name)
		}
	case *ast.ArrayType:
		return g.validateExtra(x.Elt, kind)
	case *ast.MapType:
		if e := g.validateExtra(x.Key, kind); e != nil {
			return e
		}
		return g.validateExtra(x.Value, kind)
	}
	return nil
}

// emitEnum uses only enum-lowerer's nominal metadata, never inferred interfaces.
// Exact value-case dispatch rejects typed nil, pointer, and embedded foreign cases.
func (g *generator) emitEnum(m model) error {
	a := g.alias
	d := m.enum
	if len(d.Variants) > 0 && d.Variants[0].HasSerdeValue {
		return g.emitNumericEnum(m)
	}
	var w, r strings.Builder
	fmt.Fprintf(&w, "\nfunc Serialize%s(s %s.Serializer,v *%s)error{if v==nil{return %s.NewError(%s.Type,\"nil enum pointer\")};switch x:=(*v).(type){", m.name, a, m.name, a, a)
	fmt.Fprintf(&r, "\nfunc Deserialize%s(d %s.Deserializer)(v %s,e error){e=d.Enum(%q,[]string{", m.name, a, m.name, m.name)
	for _, v := range d.Variants {
		name := v.Name
		if v.HasSerdeRename {
			name = v.SerdeRename
		}
		fmt.Fprintf(&r, "%q,", name)
	}
	fmt.Fprintf(&r, "},%s.VisitorFuncs{OnEnum:func(access %s.EnumAccess)error{name,payload,e:=access.Variant();if e!=nil{return e};switch name{", a, a)
	for _, v := range d.Variants {
		name := v.Name
		if v.HasSerdeRename {
			name = v.SerdeRename
		}
		fmt.Fprintf(&w, "case %s:_=x;", v.CaseName)
		fmt.Fprintf(&r, "case %q:", name)
		if v.Payload == "" {
			fmt.Fprintf(&w, "return s.UnitVariant(%q,%q);", m.name, name)
			fmt.Fprintf(&r, "if e:=payload.Unit();e!=nil{return e};v=%s();return nil;", v.Constructor)
			continue
		}
		// Payload metadata AST positions use an independent FileSet. Report errors at
		// the original variant source position rather than misinterpreting that Pos.
		g.enumPosition = &v.Position
		ws, rd, e := g.codec(v.Type, m.attrs.derive["Serialize"], m.attrs.derive["Deserialize"])
		g.enumPosition = nil
		if e != nil {
			return fmt.Errorf("%s: enum payload %s: %w", v.Position, v.Name, e)
		}
		fmt.Fprintf(&w, "return s.NewtypeVariant(%q,%q,func(s %s.Serializer)error{return %s(s,x.Value)});", m.name, v.Name, a, ws)
		fmt.Fprintf(&r, "return payload.Newtype(func(d %s.Deserializer)error{x,e:=%s(d);if e!=nil{return e};v=%s(x);return nil});", a, rd, v.Constructor)
	}
	fmt.Fprintf(&w, "default:return %s.NewError(%s.Type,%q)}}\n", a, a, "invalid enum "+m.name)
	fmt.Fprintf(&r, "default:return %s.NewError(%s.UnknownVariant,\"unknown variant \"+name)}}});return v,e}\n", a, a)
	if m.attrs.derive["Serialize"] {
		g.helpers.WriteString(w.String())
	}
	if m.attrs.derive["Deserialize"] {
		g.helpers.WriteString(r.String())
	}
	return nil
}

// Legacy passes still report coordinates in their own transformed buffers.
// Preserve their errors, but never present those coordinates as original spans.
func stageError(stage string, err error) error {
	var located interface{ SourceDiagnostic() bool }
	if errors.As(err, &located) && located.SourceDiagnostic() {
		return fmt.Errorf("%s: %w", stage, err)
	}
	return fmt.Errorf("%s [generated/unmapped; intermediate coordinates]: %w", stage, err)
}
