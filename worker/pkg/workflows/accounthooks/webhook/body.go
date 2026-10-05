// Package webhook delivers one lifecycle event of a job run to one receiver over HTTP.
//
// What a receiver gets is a contract: a POST of the body built by bodyOf, with the content
// type, the user agent and the signature headers set by Sender.Send. The package knows
// neither Temporal nor the API: it is given a URL, a secret and an event.
package webhook

import (
	"encoding/json"

	"github.com/fishtre-compagnie/husonym/internal/runevents"
)

// document is the body of a webhook. The names and the order of its fields are what
// receivers parse and what they verify the signature against.
type document struct {
	EventName string           `json:"event_name"`
	EventData *runevents.Event `json:"event_data"`
}

// bodyOf returns the bytes sent to a receiver for the event: the same event always gives
// the same bytes.
func bodyOf(event *runevents.Event) ([]byte, error) {
	return json.Marshal(document{EventName: event.Kind().String(), EventData: event})
}
