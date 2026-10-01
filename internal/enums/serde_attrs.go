package enums

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var wireInteger = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)

func parseSerdeVariantAttr(body string, v *Variant) error {
	field, value, ok := strings.Cut(strings.TrimSpace(body[len("serde("):len(body)-1]), "=")
	if !ok {
		return fmt.Errorf("variant serde attribute requires rename=string or value=int64 literal")
	}
	field, value = strings.TrimSpace(field), strings.TrimSpace(value)
	switch field {
	case "rename":
		if v.HasSerdeRename || v.HasSerdeValue {
			return fmt.Errorf("duplicate or mixed variant serde mapping")
		}
		if len(value) < 2 || value[0] != '"' {
			return fmt.Errorf("serde rename requires a quoted string")
		}
		name, e := strconv.Unquote(value)
		if e != nil || !utf8.ValidString(name) {
			return fmt.Errorf("serde rename requires a valid UTF-8 string literal")
		}
		v.SerdeRename, v.HasSerdeRename = name, true
	case "value":
		if v.HasSerdeRename || v.HasSerdeValue {
			return fmt.Errorf("duplicate or mixed variant serde mapping")
		}
		if !wireInteger.MatchString(value) {
			return fmt.Errorf("serde value requires a signed int64 decimal literal")
		}
		n, e := strconv.ParseInt(value, 10, 64)
		if e != nil {
			return fmt.Errorf("serde value exceeds int64 range")
		}
		v.SerdeValue, v.HasSerdeValue = strconv.FormatInt(n, 10), true
	default:
		return fmt.Errorf("unsupported enum variant serde attribute %s", field)
	}
	return nil
}

// ValidateSerdeMapping checks an explicit codec, not an enum representation.
// Enum identity, constructors, exhaustiveness and Go zero values do not change.
func ValidateSerdeMapping(d Decl) error {
	mapped, numeric := false, false
	for _, v := range d.Variants {
		mapped = mapped || v.HasSerdeRename || v.HasSerdeValue
		numeric = numeric || v.HasSerdeValue
	}
	if !mapped {
		return nil
	}
	seen := map[string]bool{}
	for _, v := range d.Variants {
		if v.Type != nil {
			return fmt.Errorf("%s: enum serde mapping currently requires all variants to have no payload", v.Position)
		}
		wire := v.Name
		if numeric {
			if !v.HasSerdeValue || v.HasSerdeRename {
				return fmt.Errorf("%s: numeric serde mapping requires explicit value on every variant; string/numeric mappings cannot mix", v.Position)
			}
			wire = v.SerdeValue
		} else if v.HasSerdeRename {
			wire = v.SerdeRename
		}
		if seen[wire] {
			return fmt.Errorf("%s: duplicate enum serde mapping %q", v.Position, wire)
		}
		seen[wire] = true
	}
	return nil
}
