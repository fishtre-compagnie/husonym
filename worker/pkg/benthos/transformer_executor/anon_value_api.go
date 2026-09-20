package transformer_executor

import (
	"encoding/json"
	"fmt"

	javascript_functions "github.com/fishtre-compagnie/husonym/internal/javascript/functions"
	"github.com/redpanda-data/benthos/v4/public/service"
)

// AnonValueApi exposes a single message to the JavaScript functions of a runner
// (benthos.v0_msg_as_structured, husonym.patchStructuredMessage…) outside of a Benthos stream.
type AnonValueApi struct {
	message *service.Message
}

var _ javascript_functions.ValueApi = (*AnonValueApi)(nil)

// NewAnonValueApi returns a value API with no message; set one with SetMessage.
func NewAnonValueApi() *AnonValueApi {
	return &AnonValueApi{}
}

func (b *AnonValueApi) SetMessage(message *service.Message) {
	b.message = message
}

func (b *AnonValueApi) Message() *service.Message {
	return b.message
}

func (b *AnonValueApi) SetBytes(bytes []byte) {
	b.message.SetBytes(bytes)
}

func (b *AnonValueApi) AsBytes() ([]byte, error) {
	return b.message.AsBytes()
}

func (b *AnonValueApi) SetStructured(value any) {
	b.message.SetStructured(value)
}

func (b *AnonValueApi) AsStructured() (any, error) {
	return b.message.AsStructured()
}

func (b *AnonValueApi) MetaGet(key string) (any, bool) {
	return b.message.MetaGet(key)
}

func (b *AnonValueApi) MetaSetMut(key string, value any) {
	b.message.MetaSetMut(key, value)
}

func (b *AnonValueApi) GetPropertyPathValue(propertyPath string) (any, error) {
	return propertyPathValue(b.message, propertyPath)
}

// propertyPathValue returns the value a top-level key holds in a structured message.
func propertyPathValue(message *service.Message, propertyPath string) (any, error) {
	if message == nil {
		return nil, fmt.Errorf("message is nil")
	}
	structuredValue, err := message.AsStructured()
	if err != nil {
		return nil, fmt.Errorf("failed to get structured message: %w", err)
	}
	structuredValueMap, ok := structuredValue.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("structured value is not a map[string]any")
	}
	return structuredValueMap[propertyPath], nil
}

func NewMessage(input map[string]any) (*service.Message, error) {
	bits, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal input map: %w", err)
	}
	return service.NewMessage(bits), nil
}
