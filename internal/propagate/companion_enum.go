package propagate

import (
	"fmt"
	"go/ast"

	"github.com/hxsf/goxide/internal/enums"
)

func (l *lowering) bindCompanionEnum(d enums.Decl, filename string) (enums.Decl, error) {
	key := "companion-enum|" + filename + "|" + d.Name
	if prior, ok := l.boundFamilies[key]; ok {
		return prior, nil
	}
	source, sourceErr := l.enumDeclarationFile(filename)
	if sourceErr != nil {
		return d, sourceErr
	}
	bind := func(value string, typeOnly bool) (string, error) {
		if value == "" {
			return value, nil
		}
		return l.bindCanonicalEnumExpression(value, nil, source, typeOnly)
	}
	var err error
	d.LookupName = ""
	for _, helper := range []*string{&d.Validator, &d.Tag} {
		*helper, err = bind(*helper, false)
		if err != nil {
			return d, err
		}
	}
	d.Variants = append([]enums.Variant(nil), d.Variants...)
	for i := range d.Variants {
		v := &d.Variants[i]
		for _, helper := range []*string{&v.Constructor, &v.Value, &v.Project} {
			*helper, err = bind(*helper, false)
			if err != nil {
				return d, err
			}
		}
		if v.Payload != "" {
			v.PayloadAlias, err = bind(v.PayloadAlias, true)
			if err != nil {
				return d, err
			}
		}
	}
	if l.boundFamilies == nil {
		l.boundFamilies = map[string]enums.Decl{}
	}
	l.boundFamilies[key] = d
	if l.cfg.InferredFamilies != nil {
		*l.cfg.InferredFamilies = append(*l.cfg.InferredFamilies, d)
	}
	return d, nil
}

func (l *lowering) enumDeclarationFile(filename string) (*ast.File, error) {
	for _, f := range append([]*ast.File{l.file}, l.companions...) {
		if l.fs.PositionFor(f.Pos(), false).Filename == filename {
			return f, nil
		}
	}
	return nil, fmt.Errorf("enum declaration file is unavailable: %s", filename)
}
