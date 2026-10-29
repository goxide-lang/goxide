package main

import (
	"errors"
	"fmt"

	model "example.org/serde-model"
	"github.com/goxide-lang/serde"
	json "github.com/goxide-lang/serde/json"
)

type unknown struct{ model.Event }

func require(ok bool, label string) {
	if !ok {
		panic(label)
	}
}
func main() {
	input := []byte("{\"packet\":{\"Values\":[1,2],\"Pair\":[3,4]},\"Event\":{\"Counts\":[5,6,7]},\"Amount\":8}")
	value, err := json.Unmarshal(input, model.DeserializeEnvelope)
	require(err == nil, "decode: "+fmt.Sprint(err))
	require(value.Packet.Values[1] == 2 && value.Packet.Pair[1] == 4 && value.Amount == 8, "generic fields and aliases")
	encoded, err := json.Marshal(&value, model.SerializeEnvelope)
	require(err == nil && string(encoded) == "{\"packet\":{\"Values\":[1,2],\"Pair\":[3,4]},\"Event\":{\"Counts\":[5,6,7]},\"Optional\":null,\"Amount\":8}", "encode: "+string(encoded)+" "+fmt.Sprint(err))
	for _, bad := range []struct{ input, kind string }{
		{"{}", serde.MissingField},
		{"{\"packet\":{},\"packet\":{}}", serde.MissingField},
		{"{\"packet\":{\"Values\":[],\"Pair\":[1]},\"Event\":\"Idle\",\"Amount\":1}", serde.Range},
		{"{\"packet\":{\"Values\":[],\"Pair\":[1,2]},\"Event\":\"Idle\",\"Amount\":1,\"Amount\":2}", "Duplicate"},
		{"{\"packet\":{\"Values\":[],\"Pair\":[1,2]},\"Event\":\"Idle\",\"Amount\":1,\"Optional\":\"bad\"}", serde.Type},
	} {
		_, err := json.Unmarshal([]byte(bad.input), model.DeserializeEnvelope)
		var detail *serde.Error
		require(errors.As(err, &detail) && detail.Kind == bad.kind, "error kind: "+fmt.Sprint(err))
	}
	for _, invalid := range []model.Event{nil, (*model.Idle)(nil), (*model.Counts)(nil), unknown{}} {
		_, err := json.Marshal(&invalid, model.SerializeEvent)
		var detail *serde.Error
		require(errors.As(err, &detail) && detail.Kind == serde.Type, "invalid enum: "+fmt.Sprint(err))
	}
	_, err = json.Marshal((*model.Envelope)(nil), model.SerializeEnvelope)
	require(err != nil, "nil struct pointer")
	checkDefaults()
	fmt.Println("ordinary serde protocols passed")
}

func checkDefaults() {
	for _, input := range []string{`{}`, `{"Required":null,"First":"bad"}`, `{"Required":null,"Last":"bad"}`} {
		model.ResetDefaults()
		_, err := json.Unmarshal([]byte(input), model.DeserializeDefaults)
		require(err != nil && model.DefaultTrace() == "", "defaults ran before field validation: "+fmt.Sprint(err))
	}
	model.ResetDefaults()
	value, err := json.Unmarshal([]byte(`{"Required":null}`), model.DeserializeDefaults)
	require(err == nil && model.DefaultTrace() == "FSL" && value.First == 1 && value.Ignored.Index == 2 && value.Last == 3 && value.Required == nil, "business default order: "+fmt.Sprint(err))
	model.ResetDefaults()
	value, err = json.Unmarshal([]byte(`{"First":10,"Last":30,"Required":9,"Ignored":[{"unread":true}]}`), model.DeserializeDefaults)
	require(err == nil && model.DefaultTrace() == "S" && value.First == 10 && value.Last == 30 && value.Ignored.Index == 1 && value.Required != nil && *value.Required == 9, "present/skip defaults: "+fmt.Sprint(err))
	encoded, err := json.Marshal(&model.SerializeOnly{Value: 17, Hidden: func() { panic("skip") }}, model.SerializeSerializeOnly)
	require(err == nil && string(encoded) == `{"Value":17}`, "Serialize required Deserialize or Default: "+fmt.Sprint(err))
	read, err := json.Unmarshal([]byte(`{"Value":19}`), model.DeserializeDeserializeOnly)
	require(err == nil && read.Value == 19, "Deserialize required Serialize: "+fmt.Sprint(err))
	_, err = json.Unmarshal([]byte(`{}`), model.DeserializePointerRecord)
	var detail *serde.Error
	require(errors.As(err, &detail) && detail.Kind == serde.MissingField, "pointer accepted missing: "+fmt.Sprint(err))
	for _, tc := range []struct {
		input, output string
		nilPointer    bool
	}{
		{`{"Pointer":null}`, `{"Pointer":null,"Optional":null}`, true},
		{`{"Pointer":23}`, `{"Pointer":23,"Optional":null}`, false},
	} {
		pointer, err := json.Unmarshal([]byte(tc.input), model.DeserializePointerRecord)
		require(err == nil, "pointer decode: "+fmt.Sprint(err))
		require((pointer.Pointer == nil) == tc.nilPointer, "pointer null state")
		encoded, err := json.Marshal(&pointer, model.SerializePointerRecord)
		if !tc.nilPointer {
			require(*pointer.Pointer == 23, "pointer payload")
		}
		require(err == nil && string(encoded) == tc.output, "pointer round trip: "+string(encoded)+" "+fmt.Sprint(err))
	}
}
