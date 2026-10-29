//go:build ignore

// Extract the public SDK surface from one verified module directory.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/build"
	"go/doc"
	"go/format"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
)

const modulePath = "github.com/goxide-lang/syntax"

type declaration struct {
	Name      string        `json:"name"`
	Kind      string        `json:"kind"`
	Signature string        `json:"signature"`
	Doc       string        `json:"doc"`
	File      string        `json:"file"`
	Line      int           `json:"line"`
	Promoted  bool          `json:"promoted,omitempty"`
	Methods   []declaration `json:"methods,omitempty"`
}

type example struct {
	Name   string `json:"name"`
	Doc    string `json:"doc"`
	Code   string `json:"code"`
	Output string `json:"output"`
	File   string `json:"file"`
	Line   int    `json:"line"`
}

type reference struct {
	Package      string        `json:"package"`
	Doc          string        `json:"doc"`
	Declarations []declaration `json:"declarations"`
	Examples     []example     `json:"examples"`
}

type sourcePackage struct {
	files []*ast.File
	tests []*ast.File
	info  *types.Info
	types *types.Package
}

type sourceImporter struct {
	directory string
	fset      *token.FileSet
	standard  types.Importer
	packages  map[string]*sourcePackage
}

func (s *sourceImporter) Import(path string) (*types.Package, error) {
	if !strings.HasPrefix(path, modulePath+"/") {
		return s.standard.Import(path)
	}
	if p := s.packages[path]; p != nil {
		return p.types, nil
	}
	directory := filepath.Join(s.directory, strings.TrimPrefix(path, modulePath+"/"))
	listed, err := build.Default.ImportDir(directory, 0)
	if err != nil {
		return nil, err
	}
	p := &sourcePackage{info: &types.Info{Defs: map[*ast.Ident]types.Object{}}}
	parse := func(names []string, output *[]*ast.File) error {
		for _, name := range names {
			file, err := parser.ParseFile(s.fset, filepath.Join(directory, name), nil, parser.ParseComments)
			if err != nil {
				return err
			}
			*output = append(*output, file)
		}
		return nil
	}
	if err := parse(listed.GoFiles, &p.files); err != nil {
		return nil, err
	}
	if err := parse(append(listed.TestGoFiles, listed.XTestGoFiles...), &p.tests); err != nil {
		return nil, err
	}
	configuration := types.Config{Importer: s}
	p.types, err = configuration.Check(path, s.fset, p.files, p.info)
	if err != nil {
		return nil, err
	}
	s.packages[path] = p
	return p.types, nil
}

func descriptions(p *sourcePackage) map[types.Object]string {
	result := map[types.Object]string{}
	for _, file := range p.files {
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				result[p.info.Defs[d.Name]] = d.Doc.Text()
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					text := d.Doc.Text()
					switch n := spec.(type) {
					case *ast.TypeSpec:
						if n.Doc != nil {
							text = n.Doc.Text()
						}
						result[p.info.Defs[n.Name]] = text + n.Comment.Text()
					case *ast.ValueSpec:
						if n.Doc != nil {
							text = n.Doc.Text()
						}
						for _, name := range n.Names {
							result[p.info.Defs[name]] = text + n.Comment.Text()
						}
					}
				}
			}
		}
	}
	return result
}

func (s *sourceImporter) format(node any) (string, error) {
	var output bytes.Buffer
	err := format.Node(&output, s.fset, node)
	return output.String(), err
}

func (s *sourceImporter) location(pos token.Pos) (string, int) {
	position := s.fset.Position(pos)
	name, _ := filepath.Rel(s.directory, position.Filename)
	return filepath.ToSlash(name), position.Line
}

func (s *sourceImporter) reference(path string) (reference, error) {
	if _, err := s.Import(path); err != nil {
		return reference{}, err
	}
	p := s.packages[path]
	descriptions := descriptions(p)
	// go/doc filters private fields; go/types supplies the complete promoted method set.
	public, err := doc.NewFromFiles(s.fset, p.files, path, doc.PreserveAST)
	if err != nil {
		return reference{}, err
	}
	publicTypes := map[string]*ast.TypeSpec{}
	for _, typ := range public.Types {
		for _, spec := range typ.Decl.Specs {
			node := spec.(*ast.TypeSpec)
			publicTypes[node.Name.Name] = node
		}
	}
	qualifier := func(pkg *types.Package) string {
		if pkg == p.types {
			return ""
		}
		return pkg.Name()
	}
	entry := func(object types.Object) declaration {
		file, line := s.location(object.Pos())
		return declaration{Name: object.Name(), Signature: types.ObjectString(object, qualifier), Doc: descriptions[object], File: file, Line: line}
	}
	result := reference{Package: path, Doc: public.Doc}
	for _, name := range p.types.Scope().Names() {
		object := p.types.Scope().Lookup(name)
		if !object.Exported() {
			continue
		}
		d := entry(object)
		switch object := object.(type) {
		case *types.Const:
			d.Kind = "const"
		case *types.Var:
			d.Kind = "var"
		case *types.Func:
			d.Kind = "func"
		case *types.TypeName:
			d.Kind = "type"
			node := publicTypes[name]
			if node == nil {
				return result, fmt.Errorf("public type %s is missing from documentation", name)
			}
			copy := *node
			copy.Doc, copy.Comment = nil, nil
			d.Signature, err = s.format(&ast.GenDecl{Tok: token.TYPE, Specs: []ast.Spec{&copy}})
			if err != nil {
				return result, err
			}
			valueMethods := types.NewMethodSet(object.Type())
			pointerMethods := types.NewMethodSet(types.NewPointer(object.Type()))
			// Interface types have an empty pointer method set.
			methods := pointerMethods
			if methods.Len() == 0 {
				methods = valueMethods
			}
			for index := 0; index < methods.Len(); index++ {
				selected := methods.At(index)
				method := selected.Obj()
				if !method.Exported() {
					continue
				}
				if value := valueMethods.Lookup(method.Pkg(), method.Name()); value != nil {
					selected = value
				}
				m := entry(method)
				m.Kind, m.Promoted = "method", len(selected.Index()) > 1
				m.Signature = "func (" + types.TypeString(selected.Recv(), qualifier) + ") " + method.Name() + strings.TrimPrefix(types.TypeString(selected.Type(), qualifier), "func")
				d.Methods = append(d.Methods, m)
			}
		default:
			return result, fmt.Errorf("undocumented exported object %s", name)
		}
		result.Declarations = append(result.Declarations, d)
	}
	for _, source := range doc.Examples(p.tests...) {
		node := source.Code
		if source.Play != nil {
			node = source.Play
		}
		code, err := s.format(node)
		if err != nil {
			return result, err
		}
		file, line := s.location(source.Code.Pos())
		result.Examples = append(result.Examples, example{Name: "Example" + source.Name, Doc: source.Doc, Code: code, Output: source.Output, File: file, Line: line})
	}
	return result, nil
}

func main() {
	if err := generate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: sdk_reference module-directory")
	}
	sources := sourceImporter{directory: os.Args[1], fset: token.NewFileSet(), standard: importer.Default(), packages: map[string]*sourcePackage{}}
	var result []reference
	for _, name := range []string{"token", "proc_macro", "syn"} {
		page, err := sources.reference(modulePath + "/" + name)
		if err != nil {
			return err
		}
		result = append(result, page)
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}
