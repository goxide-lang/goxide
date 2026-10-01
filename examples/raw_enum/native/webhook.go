package native

import "encoding/json"

// The wire format can evolve independently of the hgo consumer's known cases.
type Webhook struct {
	State   string
	Attempt uint8
}

func Decode(data []byte) (Webhook, error) {
	var value Webhook
	err := json.Unmarshal(data, &value)
	return value, err
}
