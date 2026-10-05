package piitext

import (
	"fmt"
	"slices"
	"unicode/utf8"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
)

// ConfigError is the error of a configuration that cannot be applied. Error returns Reason.
type ConfigError struct {
	Reason string
}

func (e *ConfigError) Error() string {
	return e.Reason
}

// Validate tells whether a configuration can be applied: nil, or a *ConfigError.
//
// A transform operator may run any Husonym transformer but this one, and a masking character
// is one character or none.
func Validate(config *mgmtv1alpha1.TransformPiiText) error {
	if err := validateAnonymizer(config.GetDefaultAnonymizer(), "default"); err != nil {
		return err
	}
	// In the order of the entity types, so that the same configuration is refused the same way.
	entities := make([]string, 0, len(config.GetEntityAnonymizers()))
	for entity := range config.GetEntityAnonymizers() {
		entities = append(entities, entity)
	}
	slices.Sort(entities)
	for _, entity := range entities {
		if err := validateAnonymizer(config.GetEntityAnonymizers()[entity], fmt.Sprintf("entity (%s)", entity)); err != nil {
			return err
		}
	}
	return nil
}

func validateAnonymizer(anonymizer *mgmtv1alpha1.PiiAnonymizer, where string) error {
	if anonymizer.GetTransform().GetConfig().GetTransformPiiTextConfig() != nil {
		return &ConfigError{Reason: fmt.Sprintf(
			"found nested TransformPiiText config in %s anonymizer. TransformPiiText may not be used deeply nested within itself.",
			where,
		)}
	}
	if utf8.RuneCountInString(anonymizer.GetMask().GetMaskingChar()) > 1 {
		return &ConfigError{Reason: fmt.Sprintf(
			"the masking character of the %s anonymizer of TransformPiiText must be a single character",
			where,
		)}
	}
	return nil
}
