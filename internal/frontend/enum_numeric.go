package frontend

import (
	"fmt"
	"strings"
)

// Numeric wire values select closed nominal constructors. They never become
// the source enum's representation, and no unknown value is stored in a case.
func (g *generator) emitNumericEnum(m model) error {
	a := g.alias
	var w, r strings.Builder
	fmt.Fprintf(&w, "\nfunc Serialize%s(s %s.Serializer,v *%s)error{if v==nil{return %s.NewError(%s.Type,\"nil enum pointer\")};switch %s(*v){", m.name, a, m.name, a, a, m.enum.Tag)
	fmt.Fprintf(&r, "\nfunc Deserialize%s(d %s.Deserializer)(v %s,e error){ n,e:=%s.ReadInt64(d);if e!=nil{return v,e};switch n{", m.name, a, m.name, a)
	for _, v := range m.enum.Variants {
		fmt.Fprintf(&w, "case %d:return s.Int64(%s);", v.Tag, v.SerdeValue)
		fmt.Fprintf(&r, "case %s:return %s(),nil;", v.SerdeValue, v.Constructor)
	}
	fmt.Fprintf(&w, "default:return %s.NewError(%s.Type,%q)}}\n", a, a, "invalid enum "+m.name)
	fmt.Fprintf(&r, "default:return v,%s.NewError(%s.UnknownVariant,%q)}}\n", a, a, "unknown numeric variant for "+m.name)
	if m.attrs.derive["Serialize"] {
		g.helpers.WriteString(w.String())
	}
	if m.attrs.derive["Deserialize"] {
		g.helpers.WriteString(r.String())
	}
	return nil
}
