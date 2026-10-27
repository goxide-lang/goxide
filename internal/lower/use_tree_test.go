package lower_test

import "testing"

func TestUseTreeExecution(t *testing.T) {
	sources := map[string]string{
		"thiserror/lib.gox": `package thiserror
pub type Error string
pub struct Result[T any] { pub Value T }
pub func Answer() int { return 42 }
`,
		"facade/lib.gox": `package facade
pub use associatedfixture::thiserror::{Error, Result};
`,
		"main.gox": `package main
use associatedfixture::thiserror as thiserror;
use thiserror::{Error, Result};
use associatedfixture::{facade::{self as facade, *}, thiserror::{Answer as answer}};
use {fmt::{Println, Sprintf as render}, bytes::{self, Buffer}};
#[cfg(any())] use missing::{Bad, Unknown};
func main() {
 var failure Error = "broken"
 value := Result[int]{Value:answer()}
 var exposed facade::Error = failure
 var buffer Buffer
 buffer.WriteString(render("%d", value.Value))
 Println(failure, value.Value, exposed, bytes::Equal(buffer.Bytes(), []byte("42")))
}
`,
	}
	runPrivateAssociatedFixture(t, sources, "broken 42 broken true\n")
}

func TestUseTreeMacroExecution(t *testing.T) {
	runMacroBindingSource(t, `package main
macro_rules! imports { ($print:ident, $render:ident) => { use fmt::{Println as $print, Sprintf as $render}; }; }
imports!(Println, render)
macro_rules! report { () => { Println(render("%d", 42)) }; }
func main() { report!() }
`, "42\n")
}
