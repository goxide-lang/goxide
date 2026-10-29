package lower_test

import (
	"testing"

	"github.com/goxide-lang/goxide/internal/check"
)

func TestReceiverShorthandExecution(t *testing.T) {
	runMacroBindingSource(t, `package main
use bytes;
use fmt;
use io;
trait Snapshot { func snapshot(self) int }
trait Format { func format(&self, w io::Writer) error }
trait Change { func add(self &mut Self, delta int) }
struct Counter { value int }
impl Snapshot for Counter {
 func snapshot(self Self) int { self.value++; return self.value }
}
impl Format for Counter {
 func format(&self, w io::Writer) error {
  _, err := fmt::Fprintf(w, "%d", self.value)
  return err
 }
}
impl Change for Counter {
 func add(&mut self, delta int) { self.value += delta }
}
macro_rules! member {
 (read, $receiver:ident) => { func read(&$receiver) int { return self.value } };
 (copy, $receiver:ident) => { func copy($receiver) Self { return self } };
 (grow, $receiver:ident) => { func grow(&mut $receiver, delta int) { self.value += delta } };
}
impl Counter {
 member!(read, self)
 member!(copy, self)
 member!(grow, self)
}
func main() {
 counter := Counter{value:3}
 copied := counter.copy()
 snapshot := counter.snapshot()
 counter.add(2)
 grow := counter.grow
 grow(3)
 var buffer bytes::Buffer
 err := counter.format(&buffer)
 fmt::Println(snapshot, copied.read(), counter.read(), buffer.String(), err == nil)
}
`, "4 3 8 8 true\n")
}

func TestReceiverShorthandKeepsPermissions(t *testing.T) {
	for _, test := range []struct{ name, source, code string }{
		{"readonly", `impl Data {func write(&self){/*bad*/self.N/*end*/++}}`, check.ReferencePermission},
		{"mismatched-trait", `trait Read{func read(&self)};impl Read for Data{/*bad*/func read(&mut self){}/*end*/}`, check.TraitInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := "package model\ntype Data struct{N int}\n" + test.source
			_, _, diagnostics := checkP4DiagnosticSource(t, source)
			assertP4SourceDiagnostic(t, source, diagnostics, test.code, "")
		})
	}
}
