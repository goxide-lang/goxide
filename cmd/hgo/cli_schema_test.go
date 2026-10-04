package main

import (
	"github.com/spf13/pflag"
	"testing"
)

// Future short names must obey the same lexical contract without registering
// speculative production flags just to exercise combinations.
func TestCLIShortSchema(t *testing.T) {
	for _, tc := range []struct {
		args []string
		ok   bool
	}{
		{[]string{"-ab"}, true}, {[]string{"-aboFILE"}, true},
		{[]string{"-abo", "-9"}, true}, {[]string{"-abo", "--"}, false},
		{[]string{"-aaa"}, false}, {[]string{"-aba"}, false},
		{[]string{"-offline"}, false}, {[]string{"-offline=false"}, false},
		{[]string{"-o", "-offline"}, true}, {[]string{"-oz"}, true},
		{[]string{"-az"}, false}, {[]string{"-ab=true"}, false},
		{[]string{"--", "-aaa"}, true}, {[]string{"input", "-aaa"}, true},
	} {
		f := pflag.NewFlagSet("synthetic", pflag.ContinueOnError)
		f.BoolP("alpha", "a", false, "")
		f.BoolP("beta", "b", false, "")
		f.StringP("o", "o", "", "")
		f.Bool("offline", false, "")
		if err := checkOptionTokens(f, tc.args); (err == nil) != tc.ok {
			t.Errorf("%q: error %v; want valid=%v", tc.args, err, tc.ok)
		}
	}
}
