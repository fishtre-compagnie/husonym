package transformers

import (
	"crypto/rand"
	"fmt"
	"sync"

	"github.com/fishtre-compagnie/husonym/worker/pkg/phoneformat"
	"github.com/redpanda-data/benthos/v4/public/bloblang"
)

// TransformPhoneNumber with preserve_format, under Benthos. It is registered apart from
// transform_phone_number, whose builder is generated: the generated options draw their
// randomness for every value, where the permutation needs one key for all of them.
//
// Bloblang calls the constructor of a function again for every value when an argument
// is dynamic (value:this.<column>), so the key cannot live in the constructor: it is
// drawn once per process.
var processPhoneFormatPseudonymizer = sync.OnceValues(NewPhoneFormatPseudonymizer)

func init() {
	spec := bloblang.NewPluginSpec().
		Description("Keeps the prefix, separators and length of a phone number typed as a string, and permutes its subscriber digits.").
		Category("string").
		Param(bloblang.NewAnyParam("value").Optional())

	err := bloblang.RegisterFunctionV2(
		"transform_phone_number_preserve_format",
		spec,
		func(args *bloblang.ParsedParams) (bloblang.Function, error) {
			value, err := args.GetOptionalString("value")
			if err != nil {
				return nil, err
			}
			pseudonymizer, err := processPhoneFormatPseudonymizer()
			if err != nil {
				return nil, err
			}
			return func() (any, error) {
				if value == nil {
					return nil, nil
				}
				res, err := pseudonymizer.Pseudonymize(*value)
				if err != nil {
					return nil, fmt.Errorf("unable to run transform_phone_number_preserve_format: %w", err)
				}
				return res, nil
			}, nil
		},
	)
	if err != nil {
		panic(err)
	}
}

// NewPhoneFormatPseudonymizer draws a key: the outputs of one pseudonymizer are
// consistent with each other, and with no other one.
func NewPhoneFormatPseudonymizer() (*phoneformat.Pseudonymizer, error) {
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		return nil, fmt.Errorf("drawing the key of the phone permutation: %w", err)
	}
	return phoneformat.New(key), nil
}

// BuildPhoneNumberPreserveFormatBloblang is the bloblang call for the column at valuePath.
func BuildPhoneNumberPreserveFormatBloblang(valuePath string) string {
	return fmt.Sprintf("transform_phone_number_preserve_format(value:this.%s)", valuePath)
}
