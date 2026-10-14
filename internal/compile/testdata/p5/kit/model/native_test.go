package model_test

import (
	"testing"

	"example.org/p5/kit"
	"example.org/p5/kit/model"
)

func TestNativeConsumer(t *testing.T) {
	var value kit.Choice = model.Create()
	if model.Read[model.Choice](value) != 3 || kit.Label != "shared" {
		t.Fatal("native consumer cannot use the generated API")
	}
}
