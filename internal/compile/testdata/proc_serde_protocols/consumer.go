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
	fmt.Println("ordinary serde protocols passed")
}
