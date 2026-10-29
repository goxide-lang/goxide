package main

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"trial.example/app/logkit"
	cfg "trial.example/model/config"
)

func TestOrder(t *testing.T) {
	cfg.DefaultCalls, logkit.Calls = 0, 0
	got, err := Run(`{"Name":"42","Mode":{"Batch":3}}`, true)
	if err != nil || got != (Receipt{42, 3, -1, 8080}) || cfg.DefaultCalls != 1 || logkit.Calls != 1 {
		t.Fatal(got, err, cfg.DefaultCalls, logkit.Calls)
	}
	got, err = Run(`{"Name":"7","Retry":null}`, false)
	if err != nil || got.Units != 1 || got.Retry != -1 || logkit.Calls != 1 {
		t.Fatal(got, err)
	}
	got, err = Run(`{"Name":"7","Retry":2,"Settings":{"Port":90}}`, false)
	if err != nil || got.Retry != 2 || got.Port != 90 || cfg.DefaultCalls != 2 {
		t.Fatal(got, err, cfg.DefaultCalls)
	}
}
func TestErrorChainAndClosedVariants(t *testing.T) {
	logkit.Calls = 0
	_, err := Run(`{"Name":"42","Mode":{"Batch":9}}`, true)
	if !errors.Is(err, OverCapacity) {
		t.Fatal(err)
	}
	_, err = Run(`{"Name":"bad"}`, true)
	var number *strconv.NumError
	if !errors.As(err, &number) {
		t.Fatal(err)
	}
	for _, input := range []string{`{`, `{"Name":"1","Mode":{"Unknown":0}}`, `{"Name":"1","Unknown":0}`} {
		if _, err := Run(input, true); err == nil {
			t.Fatal("accepted invalid input", input)
		}
	}
	if logkit.Calls != 0 {
		t.Fatal("failure logged success")
	}
}
func TestCrossModuleProtocols(t *testing.T) {
	none := (SnapshotOps{}).Initial()
	nilSome := Snapshot{Data: cfg.Some(nil)}
	emptySome := Snapshot{Data: cfg.Some([]int{})}
	if !Same(none, none) || Same(none, nilSome) || Same(nilSome, emptySome) {
		t.Fatal("states collapsed")
	}
	for _, tc := range []struct {
		v    Snapshot
		want string
	}{{none, "Option::None"}, {nilSome, "Option::Some(nil)"}, {emptySome, "Option::Some([])"}} {
		clone := tc.v.Copy()
		if !Same(tc.v, clone) || !strings.Contains(clone.Inspect(), tc.want) {
			t.Fatal(clone.Inspect(), tc.want)
		}
	}
	original := Snapshot{Data: cfg.Some([]int{1})}
	copy := original.Copy()
	cfg.Items(copy.Data)[0] = 9
	if cfg.Items(original.Data)[0] != 1 || Same(original, copy) {
		t.Fatal("clone shares payload")
	}
}
