// Package recommendations implements the deterministic category -> transformer
// mapping and validity filter used to build AI-assisted anonymization config
// suggestions (plans/assistant-ia-config-anonymisation.md §4.2).
package recommendations

import (
	"regexp"
	"strings"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/gotypeutil"
	piidetect_table_activities "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/ee/piidetect/workflows/table/activities"
)

// Category re-exports the PII categories produced by the piidetect regex/LLM
// detectors so that callers don't need to import the worker package directly.
type Category = piidetect_table_activities.PiiCategory

const (
	CategoryNationalId     = piidetect_table_activities.PiiCategoryNationalId
	CategoryContact        = piidetect_table_activities.PiiCategoryContact
	CategoryFinancial      = piidetect_table_activities.PiiCategoryFinancial
	CategoryPersonal       = piidetect_table_activities.PiiCategoryPersonal
	CategoryLocation       = piidetect_table_activities.PiiCategoryLocation
	CategoryAuthentication = piidetect_table_activities.PiiCategoryAuth
)

// Recommendation is the mapping table's raw output for a column, before the
// server-side validity filter (validity.go) is applied.
type Recommendation struct {
	// Config is the suggested transformer configuration.
	Config *mgmtv1alpha1.TransformerConfig
	// Rationale is a short, human readable (English) explanation of why this
	// transformer was suggested. It is surfaced to the caller as part of the
	// evidence/detail shown to the reviewing user.
	Rationale string
	// IsGenericFallback is true when no name/shape-specific rule matched and
	// the mapping fell back to a category-wide generic transformer (e.g.
	// TransformString) rather than a semantically specific one (e.g.
	// TransformEmail). Such columns are candidates for an AI-generated
	// user-defined transformer proposal (plans/assistant-ia-config-anonymisation.md
	// §4.4), since the catalog match is weak.
	IsGenericFallback bool
}

// name hint patterns, matched case-insensitively against the column name.
var (
	emailNameRe   = regexp.MustCompile(`(^|_)e?mail(_|$)`)
	phoneNameRe   = regexp.MustCompile(`(^|_)(phone|tel|telephone|mobile|fax)(_|$)`)
	firstNameRe   = regexp.MustCompile(`(^|_)(first_?name|given_?name|fname|prenom|vorname|nombre|imie)(_|$)`)
	lastNameRe    = regexp.MustCompile(`(^|_)(last_?name|surname|family_?name|lname|nom|nachname|apellido|nazwisko)(_|$)`)
	fullNameRe    = regexp.MustCompile(`(^|_)(full_?name|display_?name|fullname)(_|$)`)
	cityNameRe    = regexp.MustCompile(`(^|_)(city|town|ville|stadt|ciudad|citta|miasto)(_|$)`)
	stateNameRe   = regexp.MustCompile(`(^|_)(state|province|region|departement|bundesland)(_|$)`)
	countryNameRe = regexp.MustCompile(`(^|_)(country|pays|land|paese|kraj)(_|$)`)
	zipNameRe     = regexp.MustCompile(`(^|_)(zip(_?code)?|postal_?code|postcode|cp|plz|cap)(_|$)`)
	streetNameRe  = regexp.MustCompile(`(^|_)(street|address|adresse|anschrift|direccion|indirizzo)(_|$)`)
)

// Recommend derives a raw transformer recommendation for a single column,
// based on its detected PII category, its name and its (best-effort) data
// type. The result must still be passed through FilterForValidity before
// being surfaced to a caller, since it does not take schema constraints
// (data type compatibility, generated/identity columns) into account.
func Recommend(
	category Category,
	columnName string,
	dataType mgmtv1alpha1.TransformerDataType,
) Recommendation {
	name := strings.ToLower(strings.TrimSpace(columnName))

	switch category {
	case CategoryContact:
		return recommendContact(name, dataType)
	case CategoryPersonal:
		return recommendPersonal(name, dataType)
	case CategoryLocation:
		return recommendLocation(name, dataType)
	case CategoryNationalId, CategoryFinancial, CategoryAuthentication:
		return recommendIdentifier(category, dataType)
	default:
		return passthroughRecommendation("column was not classified as PII")
	}
}

func recommendContact(name string, dataType mgmtv1alpha1.TransformerDataType) Recommendation {
	switch {
	case emailNameRe.MatchString(name):
		return Recommendation{
			Config:    transformEmailConfig(),
			Rationale: "column name matches an email pattern; category is contact PII",
		}
	case phoneNameRe.MatchString(name):
		return Recommendation{
			Config:    transformPhoneNumberConfig(),
			Rationale: "column name matches a phone number pattern; category is contact PII",
		}
	case dataType == mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_STRING:
		return Recommendation{
			Config:            transformStringConfig(),
			Rationale:         "column is contact PII with no specific name pattern matched; falling back to a generic string transform",
			IsGenericFallback: true,
		}
	default:
		return passthroughRecommendation("column is contact PII but no compatible transformer could be derived from its name or data type")
	}
}

func recommendPersonal(name string, dataType mgmtv1alpha1.TransformerDataType) Recommendation {
	switch {
	case firstNameRe.MatchString(name):
		return Recommendation{
			Config:    generateFirstNameConfig(),
			Rationale: "column name matches a first name pattern; category is personal PII",
		}
	case lastNameRe.MatchString(name):
		return Recommendation{
			Config:    generateLastNameConfig(),
			Rationale: "column name matches a last name pattern; category is personal PII",
		}
	case fullNameRe.MatchString(name):
		return Recommendation{
			Config:    generateFullNameConfig(),
			Rationale: "column name matches a full name pattern; category is personal PII",
		}
	case cityNameRe.MatchString(name):
		return Recommendation{
			Config:    generateCityConfig(),
			Rationale: "column name matches a city pattern; category is personal PII",
		}
	case dataType == mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_STRING:
		return Recommendation{
			Config:            transformStringConfig(),
			Rationale:         "column is personal PII free text with no specific name pattern matched; falling back to a generic string transform",
			IsGenericFallback: true,
		}
	default:
		return passthroughRecommendation("column is personal PII but no compatible transformer could be derived from its name or data type")
	}
}

func recommendLocation(name string, dataType mgmtv1alpha1.TransformerDataType) Recommendation {
	switch {
	case cityNameRe.MatchString(name):
		return Recommendation{
			Config:    generateCityConfig(),
			Rationale: "column name matches a city pattern; category is location PII",
		}
	case stateNameRe.MatchString(name):
		return Recommendation{
			Config:    generateStateConfig(),
			Rationale: "column name matches a state/region pattern; category is location PII",
		}
	case countryNameRe.MatchString(name):
		return Recommendation{
			Config:    generateCountryConfig(),
			Rationale: "column name matches a country pattern; category is location PII",
		}
	case zipNameRe.MatchString(name):
		return Recommendation{
			Config:    generateZipcodeConfig(),
			Rationale: "column name matches a postal code pattern; category is location PII",
		}
	case streetNameRe.MatchString(name):
		return Recommendation{
			Config:    generateStreetAddressConfig(),
			Rationale: "column name matches a street/address pattern; category is location PII",
		}
	case dataType == mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_STRING:
		return Recommendation{
			Config:            transformStringConfig(),
			Rationale:         "column is location PII with no specific name pattern matched; falling back to a generic string transform",
			IsGenericFallback: true,
		}
	default:
		return passthroughRecommendation("column is location PII but no compatible transformer could be derived from its name or data type")
	}
}

// recommendIdentifier handles the three "strong format" categories
// (national_id, financial, authentication): the goal is always to fully
// destroy the value rather than to preserve any semantic content, the
// choice of transformer is driven purely by the underlying data type.
func recommendIdentifier(category Category, dataType mgmtv1alpha1.TransformerDataType) Recommendation {
	rationale := "column is " + string(category) + " PII; recommending a strong identifier replacement"
	switch dataType {
	case mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_UUID:
		return Recommendation{Config: transformUuidConfig(), Rationale: rationale}
	case mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_STRING:
		return Recommendation{Config: generateUuidConfig(), Rationale: rationale}
	default:
		// No system transformer destroys a numeric/temporal/opaque identifier
		// without changing its type; the validity filter will fall back to
		// Passthrough when this does not fit the column's actual data type.
		// This is also the strongest signal that the column's format (a
		// structured business reference, a national format absent from the
		// catalog, ...) isn't covered by any system transformer.
		return Recommendation{Config: transformStringConfig(), Rationale: rationale, IsGenericFallback: true}
	}
}

func passthroughRecommendation(rationale string) Recommendation {
	return Recommendation{Config: passthroughConfig(), Rationale: rationale}
}

func transformEmailConfig() *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_TransformEmailConfig{
			TransformEmailConfig: &mgmtv1alpha1.TransformEmail{
				PreserveDomain: gotypeutil.ToPtr(false),
				PreserveLength: gotypeutil.ToPtr(false),
			},
		},
	}
}

func transformPhoneNumberConfig() *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_TransformPhoneNumberConfig{
			TransformPhoneNumberConfig: &mgmtv1alpha1.TransformPhoneNumber{
				PreserveLength: gotypeutil.ToPtr(false),
			},
		},
	}
}

func generateFirstNameConfig() *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_GenerateFirstNameConfig{
			GenerateFirstNameConfig: &mgmtv1alpha1.GenerateFirstName{},
		},
	}
}

func generateLastNameConfig() *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_GenerateLastNameConfig{
			GenerateLastNameConfig: &mgmtv1alpha1.GenerateLastName{},
		},
	}
}

func generateFullNameConfig() *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_GenerateFullNameConfig{
			GenerateFullNameConfig: &mgmtv1alpha1.GenerateFullName{},
		},
	}
}

func generateCityConfig() *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_GenerateCityConfig{
			GenerateCityConfig: &mgmtv1alpha1.GenerateCity{},
		},
	}
}

func generateStateConfig() *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_GenerateStateConfig{
			GenerateStateConfig: &mgmtv1alpha1.GenerateState{
				GenerateFullName: gotypeutil.ToPtr(false),
			},
		},
	}
}

func generateCountryConfig() *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_GenerateCountryConfig{
			GenerateCountryConfig: &mgmtv1alpha1.GenerateCountry{
				GenerateFullName: gotypeutil.ToPtr(false),
			},
		},
	}
}

func generateZipcodeConfig() *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_GenerateZipcodeConfig{
			GenerateZipcodeConfig: &mgmtv1alpha1.GenerateZipcode{},
		},
	}
}

func generateStreetAddressConfig() *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_GenerateStreetAddressConfig{
			GenerateStreetAddressConfig: &mgmtv1alpha1.GenerateStreetAddress{},
		},
	}
}

func generateUuidConfig() *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_GenerateUuidConfig{
			GenerateUuidConfig: &mgmtv1alpha1.GenerateUuid{
				IncludeHyphens: gotypeutil.ToPtr(true),
			},
		},
	}
}

func transformUuidConfig() *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_TransformUuidConfig{
			TransformUuidConfig: &mgmtv1alpha1.TransformUuid{},
		},
	}
}

func transformStringConfig() *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_TransformStringConfig{
			TransformStringConfig: &mgmtv1alpha1.TransformString{
				PreserveLength: gotypeutil.ToPtr(false),
			},
		},
	}
}

func passthroughConfig() *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{
			PassthroughConfig: &mgmtv1alpha1.Passthrough{},
		},
	}
}

func generateDefaultConfig() *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_GenerateDefaultConfig{
			GenerateDefaultConfig: &mgmtv1alpha1.GenerateDefault{},
		},
	}
}
