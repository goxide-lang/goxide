package lower_test

import "testing"

func TestTraitBindingPrivateOwnerAndPrimitiveFrames(t *testing.T) {
	runPrivateAssociatedFixture(t, map[string]string{
		"api/lib.gox": `package api
pub trait Catch { func Catch(self Self, caught *bool) }
`,
		"model/lib.gox": `package model
use associatedfixture::api;
struct secret {}
impl api::Catch for secret {
 func Catch(self Self, caught *bool) { *caught=recover()!=nil }
}
pub func Make() secret {return secret{}}
`,
		"main.gox": `package main
use fmt;
use associatedfixture::api;
use associatedfixture::model;
trait PrimitiveCatch {func Catch(self Self,caught *bool)}
impl PrimitiveCatch for int {func Catch(self Self,caught *bool){*caught=recover()!=nil}}
func bind[T api::Catch](value T)func(*bool){return value.Catch}
func forward[T api::Catch](value T)func(*bool){return bind(value)}
func generic[T api::Catch](value T,caught *bool){defer api::Catch::Catch(value,caught);panic("bound")}
func primitive(caught *bool){defer int(1).Catch(caught);panic("primitive")}
func primitiveSaved(caught *bool){value:=int(1);saved:=value.Catch;defer saved(caught);panic("primitive saved")}
func direct(caught *bool){value:=model::Make();defer value.Catch::<api::Catch>(caught);panic("private owner")}
func saved(caught *bool){value:=model::Make();f:=value.Catch::<api::Catch>;defer f(caught);panic("private owner saved")}
func bound(caught *bool){generic(model::Make(),caught)}
func boundSaved(caught *bool){f:=forward(model::Make());defer f(caught);panic("private owner bound")}
func observe(run func(*bool))bool{caught:=false;func(){defer func(){_=recover()}();run(&caught)}();return caught}
func main(){fmt::Println(observe(primitive),observe(primitiveSaved),observe(direct),observe(saved),observe(bound),observe(boundSaved))}
`}, "true true true true true true\n")
}
