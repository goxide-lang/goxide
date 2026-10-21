package expand

import (
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/token"
)

// Configure selects complete source items before import or declaration discovery.
// Parsing must already have succeeded, including the syntax of disabled items.
// used records configuration attributes even when they remove their item.
func Configure(file *ast.File, configuration config.Config) (used bool, err error) {
	s := selection{file: file, config: func(*ast.File) config.Config { return configuration }, used: &used}
	var names []string
	for name, enabled := range configuration.Features.Enabled {
		if enabled {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if !featureName.MatchString(name) {
			return used, s.fail(file, fmt.Sprintf("invalid feature name %q", name))
		}
	}
	for _, attr := range file.Attrs {
		if configName(attr) != "" {
			used = true
			return used, s.fail(attr, "configuration attributes require a complete top-level item")
		}
	}
	items := make([]ast.Item, 0, len(file.Items))
	for _, item := range file.Items {
		keep, err := s.item(item)
		if err != nil {
			return used, err
		}
		if keep {
			items = append(items, item)
		} else {
			dropItemComments(file, item)
		}
	}
	file.Items = items
	var id ast.NodeID
	ast.Walk(file, func(n ast.Node) bool { id++; n.(interface{ SetID(ast.NodeID) }).SetID(id); return true })
	return used, nil
}

func dropItemComments(file *ast.File, item ast.Item) {
	spans := []ast.Span{item.Span()}
	if doc := reflect.ValueOf(item).Elem().FieldByName("Doc"); !doc.IsNil() {
		spans = append(spans, doc.Interface().(*ast.CommentGroup).Span())
	}
	inside := func(span ast.Span) bool {
		for _, removed := range spans {
			if span.Start >= removed.Start && span.End <= removed.End {
				return true
			}
		}
		return false
	}
	var comments []*ast.CommentGroup
	for _, group := range file.Comments {
		var retained []*ast.Comment
		for _, comment := range group.List {
			if !inside(comment.Span()) {
				retained = append(retained, comment)
			}
		}
		if len(retained) != 0 {
			group.List = retained
			group.Loc = ast.Span{Start: retained[0].Span().Start, End: retained[len(retained)-1].Span().End}
			comments = append(comments, group)
		}
	}
	file.Comments = comments
	var directives []*ast.Directive
	for _, directive := range file.Directives {
		if !inside(directive.Span()) {
			directives = append(directives, directive)
		}
	}
	file.Directives = directives
}

type selection struct {
	file   *ast.File
	config func(*ast.File) config.Config
	engine *engine
	used   *bool
}

func (s selection) origin(n ast.Node) Origin {
	if s.engine != nil {
		if o, ok := s.engine.r.Origins[n]; ok {
			return o
		}
	}
	return Origin{s.file, n.Span()}
}

func (s selection) location(n ast.Node) Origin {
	if s.engine != nil {
		if location := s.engine.location(n); location != (Origin{}) {
			return location
		}
	}
	return s.origin(n)
}

func (s selection) fail(n ast.Node, message string) error {
	o := s.origin(n)
	call := o
	if s.engine != nil && nodeExpn(n) != 0 {
		call = s.engine.r.Expansions[nodeExpn(n)].Call
	}
	return &Diagnostic{Code: "cfg", Message: message, Location: s.location(n), Definition: o, Call: call, Expn: nodeExpn(n)}
}

func configName(a *ast.Attr) string {
	if a.Path != nil && len(a.Path.Segments) == 1 {
		name := a.Path.Segments[0].Name.Name
		if name == "cfg" || name == "cfg_attr" {
			return name
		}
	}
	return ""
}

func (s selection) item(item ast.Item) (bool, error) {
	field := reflect.ValueOf(item).Elem().FieldByName("Attrs")
	attrs := field.Interface().([]*ast.Attr)
	selected, keep, err := s.attrs(attrs)
	if err != nil || !keep {
		return keep, err
	}
	field.Set(reflect.ValueOf(selected))
	switch item.(type) {
	case *ast.MacroRulesDecl:
		// These nodes disappear during expansion, so unsupported ordinary
		// attributes must be diagnosed here instead of silently disappearing.
		if len(selected) != 0 {
			diagnostic := s.fail(selected[0], "unsupported attribute or invalid attribute position on macro item").(*Diagnostic)
			diagnostic.Code = "attribute"
			return false, diagnostic
		}
	}
	// Only the complete top-level item is selectable. A surviving item's fields,
	// members, statements and individual grouped declarations are not.
	err = s.children(item)
	return true, err
}

func (s selection) children(n ast.Node) error {
	var err error
	ast.Walk(n, func(child ast.Node) bool {
		if err != nil {
			return false
		}
		switch child := child.(type) {
		case *ast.TokenTree:
			return false
		case *ast.Attr:
			if configName(child) != "" {
				*s.used = true
				err = s.fail(child, "configuration attributes require a complete top-level item")
			}
			return false
		}
		return true
	})
	return err
}

func (s selection) attrs(attrs []*ast.Attr) ([]*ast.Attr, bool, error) {
	var out []*ast.Attr
	keep := true
	for _, attr := range attrs {
		name := configName(attr)
		if name == "" {
			out = append(out, attr)
			continue
		}
		*s.used = true
		if attr.Inner || attr.Args == nil || attr.Args.Delim != ast.Paren {
			return nil, false, s.fail(attr, name+" requires parenthesized arguments")
		}
		args, err := configArguments(attr.Args.Tokens)
		if err != nil {
			return nil, false, s.fail(attr, name+": "+err.Error())
		}
		if name == "cfg" && len(args) != 1 {
			return nil, false, s.fail(attr, "cfg requires exactly one predicate")
		}
		if name == "cfg_attr" && len(args) < 2 {
			return nil, false, s.fail(attr, "cfg_attr requires a predicate and at least one attribute")
		}
		// Validate the payload's attribute-list shape even when it is unselected.
		// Arguments of those attributes are deliberately interpreted only below.
		var payload []*ast.Attr
		if name == "cfg_attr" {
			for _, tokens := range args[1:] {
				a, err := s.attribute(tokens, attr)
				if err != nil {
					return nil, false, err
				}
				payload = append(payload, a)
			}
		}
		value, err := s.predicate(args[0], s.origin(attr).File)
		if err != nil {
			return nil, false, s.fail(attr, err.Error())
		}
		if name == "cfg" {
			keep = keep && value
		} else if value {
			nested, retained, err := s.attrs(payload)
			if err != nil {
				return nil, false, err
			}
			out = append(out, nested...)
			keep = keep && retained
		}
	}
	return out, keep, nil
}

func configArguments(tokens []ast.TokenOrTree) ([][]ast.TokenOrTree, error) {
	var args [][]ast.TokenOrTree
	var current []ast.TokenOrTree
	for _, part := range tokens {
		if part.Token != nil && part.Token.Kind == token.SEMICOLON && part.Token.Implicit {
			continue
		}
		if part.Token != nil && part.Token.Kind == token.COMMA {
			if len(current) == 0 {
				return nil, fmt.Errorf("empty argument")
			}
			args = append(args, current)
			current = nil
		} else {
			current = append(current, part)
		}
	}
	if len(current) != 0 {
		args = append(args, current)
	}
	return args, nil
}

var featureName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

func configPredicate(tokens []ast.TokenOrTree, configuration config.Config, targetUsed func()) (bool, error) {
	if len(tokens) == 0 || tokens[0].Token == nil || tokens[0].Token.Kind != token.IDENT {
		return false, fmt.Errorf("expected a configuration predicate")
	}
	name := tokens[0].Token.Text
	switch name {
	case "feature", "all", "any", "not", "env", "target_os", "target_arch", "target_platform", "target_endian", "target_pointer_width":
	default:
		if strings.HasPrefix(name, "target_") || name == "test" || name == "debug_assertions" || name == "cgo" {
			return false, fmt.Errorf("configuration predicate %q is not supported", name)
		}
		return false, fmt.Errorf("unknown configuration predicate %q", name)
	}
	if len(tokens) != 2 || tokens[1].Tree == nil || tokens[1].Tree.Delim != ast.Paren {
		return false, fmt.Errorf("configuration predicate %s requires parenthesized arguments", name)
	}
	args, err := configArguments(tokens[1].Tree.Tokens)
	if err != nil {
		return false, err
	}
	if name == "feature" {
		if len(args) != 1 || len(args[0]) != 1 || args[0][0].Token == nil || args[0][0].Token.Kind != token.STRING {
			return false, fmt.Errorf("feature requires exactly one string literal")
		}
		value, err := strconv.Unquote(args[0][0].Token.Text)
		if err != nil || !featureName.MatchString(value) {
			return false, fmt.Errorf("invalid feature name %q", value)
		}
		if configuration.Features.Declared != nil && !configuration.Features.Declared[value] {
			return false, fmt.Errorf("unknown feature %q", value)
		}
		return configuration.Features.Enabled[value], nil
	}
	if name == "env" || strings.HasPrefix(name, "target_") {
		if name == "env" && (len(args) < 1 || len(args) > 2) {
			return false, fmt.Errorf("env requires one or two string literals")
		}
		if name != "env" && len(args) != 1 {
			return false, fmt.Errorf("%s requires exactly one string literal", name)
		}
		var values []string
		for _, arg := range args {
			if len(arg) != 1 || arg[0].Token == nil || arg[0].Token.Kind != token.STRING {
				return false, fmt.Errorf("%s requires string literal arguments", name)
			}
			value, err := strconv.Unquote(arg[0].Token.Text)
			if err != nil {
				return false, fmt.Errorf("invalid %s string: %w", name, err)
			}
			values = append(values, value)
		}
		if name == "env" {
			if values[0] == "" || strings.ContainsAny(values[0], "=\x00") {
				return false, fmt.Errorf("env requires a nonempty environment key without '=' or NUL")
			}
			if configuration.Environment == nil {
				return false, fmt.Errorf("env requires a prepared environment")
			}
			value, present := configuration.Environment.LookupEnv(values[0])
			configuration.Reads.Record(configuration.Environment.EnvKey(values[0]), present, value)
			return present && (len(values) == 1 || value == values[1]), nil
		}
		allowed := config.Values(name)
		known := false
		for _, value := range allowed {
			known = known || value == values[0]
		}
		if !known {
			return false, fmt.Errorf("unknown %s value %q; allowed values: %s", name, values[0], strings.Join(allowed, ", "))
		}
		if configuration.Environment == nil {
			return false, fmt.Errorf("%s requires a prepared environment", name)
		}
		targetUsed()
		target := configuration.Environment.Target
		var value string
		switch name {
		case "target_os":
			value = target.OS
		case "target_arch":
			value = target.Arch
		case "target_platform":
			value = target.Platform
		case "target_endian":
			value = target.Endian
		case "target_pointer_width":
			value = strconv.Itoa(target.PointerWidth)
		}
		return value == values[0], nil
	}
	if name == "not" && len(args) != 1 {
		return false, fmt.Errorf("not requires exactly one predicate")
	}
	value := name == "all"
	for _, arg := range args {
		child, err := configPredicate(arg, configuration, targetUsed)
		if err != nil {
			return false, err
		}
		switch name {
		case "all":
			value = value && child
		case "any":
			value = value || child
		case "not":
			value = !child
		}
	}
	return value, nil
}

func (s selection) attribute(tokens []ast.TokenOrTree, parent *ast.Attr) (*ast.Attr, error) {
	a := &ast.Attr{Base: ast.Base{Expn: parent.Expn}, Path: &ast.Path{}}
	for i := 0; ; {
		if i >= len(tokens) || tokens[i].Token == nil || (tokens[i].Token.Kind != token.IDENT && tokens[i].Token.Kind != token.DEFAULT) {
			return nil, s.fail(parent, "cfg_attr requires an attribute name in its attribute list")
		}
		tok := tokens[i].Token
		o := Origin{s.origin(parent).File, ast.Span{Start: tok.Start, End: tok.End}}
		location := o
		ctx := ast.SyntaxCtx(0)
		if s.engine != nil {
			if meta, ok := s.engine.r.TokenContexts[tok]; ok {
				o, ctx = meta.Origin, meta.Ctx
				location = meta.Location
				if location == (Origin{}) {
					location = o
				}
			}
		}
		id := &ast.Ident{Base: ast.Base{Loc: o.Span, Expn: parent.Expn}, Name: tok.Text, Raw: tok.Raw, Ctx: ctx}
		seg := &ast.PathSegment{Base: id.Base, Name: id}
		a.Path.Segments = append(a.Path.Segments, seg)
		if i == 0 {
			a.Base.Loc, a.Path.Base = o.Span, id.Base
			if s.engine != nil {
				s.engine.r.Origins[a], s.engine.r.Origins[a.Path] = o, o
				s.engine.r.Locations[a], s.engine.r.Locations[a.Path] = location, location
			}
		}
		if s.engine != nil {
			s.engine.r.Origins[id], s.engine.r.Origins[seg] = o, o
			s.engine.r.Locations[id], s.engine.r.Locations[seg] = location, location
			if meta, ok := s.engine.r.TokenContexts[tok]; ok {
				s.engine.r.Scopes[id] = meta.Scope
			}
		}
		i++
		if i < len(tokens) && tokens[i].Token != nil && tokens[i].Token.Kind == token.DCOLON {
			i++
			continue
		}
		rest := tokens[i:]
		if len(rest) == 1 && rest[0].Tree != nil {
			a.Args = rest[0].Tree
		} else if len(rest) == 2 && rest[0].Token != nil && rest[0].Token.Kind == token.ASSIGN && rest[1].Token != nil && rest[1].Token.Kind.IsLiteral() {
			a.Args = &ast.TokenTree{Base: a.Base, Delim: ast.NoDelim, Tokens: rest}
			if s.engine != nil {
				s.engine.r.Locations[a.Args] = s.engine.r.Locations[a]
			}
		} else if len(rest) != 0 {
			return nil, s.fail(parent, "malformed attribute in cfg_attr attribute list")
		}
		return a, nil
	}
}

func (e *engine) selection(file *ast.File) selection {
	return selection{file: file, engine: e, config: func(file *ast.File) config.Config { return e.configs[file] }, used: &e.r.UsesConfiguration}
}

func (s selection) predicate(tokens []ast.TokenOrTree, owner *ast.File) (bool, error) {
	configuration := s.config(owner)
	destination := s.config(s.file)
	configuration.Environment, configuration.Reads = destination.Environment, destination.Reads
	return configPredicate(tokens, configuration, func() {
		configuration.Reads.RecordTarget(s.file)
		if s.engine != nil {
			s.engine.r.TargetFiles[s.file] = true
		}
	})
}

func (e *engine) cfg(n ast.Node, args *ast.TokenTree, kind fragmentKind, file *ast.File, parent ast.ExpnID) (ast.Node, bool, error) {
	e.r.UsesConfiguration = true
	s := e.selection(file)
	if kind != exprKind {
		return nil, true, s.fail(n, "cfg! requires an expression position")
	}
	if args == nil || args.Delim != ast.Paren {
		return nil, true, s.fail(n, "cfg! requires parenthesized arguments")
	}
	parts, err := configArguments(args.Tokens)
	if err != nil {
		return nil, true, s.fail(n, err.Error())
	}
	if len(parts) != 1 {
		return nil, true, s.fail(n, "cfg! requires exactly one predicate")
	}
	o := s.origin(n)
	value, err := s.predicate(parts[0], o.File)
	if err != nil {
		return nil, true, s.fail(n, err.Error())
	}
	if err := e.expansionBudget(o, o, parent); err != nil {
		return nil, true, err
	}
	out := &ast.BoolLit{Base: ast.Base{Loc: n.Span(), Expn: parent}, Value: value}
	e.r.Origins[out] = o
	e.r.Locations[out] = s.location(n)
	return out, true, nil
}
