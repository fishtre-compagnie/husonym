package transformers

import (
	"crypto/rand"
	"errors"
	"fmt"
	"sync"

	"github.com/fishtre-compagnie/husonym/worker/pkg/phoneformat"
	"github.com/redpanda-data/benthos/v4/public/bloblang"
)

// errNoConsistencyKey is what a mapping gets when the deployment has no key to derive the
// permutation from. It stops the run of a job that maps a column this way, on its first value,
// and no other job: nothing else in the stream asks for the function.
var errNoConsistencyKey = errors.New(
	"transform_phone_number_preserve_format: ATHANOR_CONSISTENCY_KEY is not set, and the " +
		"permutation is derived from it — set it on the worker, or turn preserve_format off",
)

// RegisterTransformPhoneNumberPreserveFormat adds TransformPhoneNumber with preserve_format to
// the bloblang environment of one stream, on the pseudonymizer of the run's consistency scope.
//
// It is registered per stream and not once per process, the way transform_pii_text is: the key
// belongs to the run's scope, not to the worker that happens to run the table. A key drawn per
// process would give two pseudonyms for the same number as soon as two tables of a job land on
// two workers, and a new one after every restart — where the whole point of the permutation is
// that the same number always comes out the same, so that a foreign key still points at its
// parent and a unique column stays unique.
//
// It is registered apart from transform_phone_number, whose builder is generated: the generated
// options draw their randomness for every value, where the permutation needs one key for all of
// them.
//
// A nil pseudonymizer means the deployment cannot derive one.
func RegisterTransformPhoneNumberPreserveFormat(
	env *bloblang.Environment,
	pseudonymizer *phoneformat.Pseudonymizer,
) error {
	spec := bloblang.NewPluginSpec().
		Description("Keeps the prefix, separators and length of a phone number typed as a string, and permutes its subscriber digits.").
		Category("string").
		Param(bloblang.NewAnyParam("value").Optional())

	return env.RegisterFunctionV2(
		"transform_phone_number_preserve_format",
		spec,
		func(args *bloblang.ParsedParams) (bloblang.Function, error) {
			if pseudonymizer == nil {
				return nil, errNoConsistencyKey
			}
			value, err := args.GetOptionalString("value")
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
}

// processPhoneFormatPseudonymizer is the pseudonymizer of callers that have no consistency scope
// to derive a key from: the anonymization API, and TransformPiiText replacing a phone number it
// found in free text. Its key is drawn once per process, so the same text gives the same number
// for as long as the process lives, and nothing is promised beyond it — a column mapped to
// TransformPhoneNumber goes through the run's own key instead (see the function above and, on
// the Athanor side, native.NewPhoneFormatPreserver).
var processPhoneFormatPseudonymizer = sync.OnceValues(NewPhoneFormatPseudonymizer)

// ProcessPhoneFormatPseudonymizer returns it, drawing its key on first use.
func ProcessPhoneFormatPseudonymizer() (*phoneformat.Pseudonymizer, error) {
	return processPhoneFormatPseudonymizer()
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
