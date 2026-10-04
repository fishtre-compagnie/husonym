package rules

import (
	"regexp"
	"strings"

	"github.com/fishtre-compagnie/husonym/backend/pkg/piidetect"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/report"
)

// The names recognized as a whole, whatever their case. Between two words an underscore,
// a dash or nothing is accepted. The first form that fits decides.
var nameForms = []struct {
	category report.Category
	form     *regexp.Regexp
}{
	{report.NationalID, wholeName(`ssn|social_?security.*|tax_?id|passport(_?number)?|driver_?licen[sc]e`)},
	{report.Contact, wholeName(`email(_?address)?|(phone|telephone|mobile)_?number`)},
	{report.Personal, wholeName(`birth_?date|(first|last|full)_?name|dob|date_?of_?birth|age`)},
	{report.Financial, wholeName(
		`(credit_?card|cc)(_?num(ber)?)?|account_?num(ber)?|bank_?account(_?num(ber)?)?|(current_?)?salary`,
	)},
	{report.Authentication, wholeName(`password|passwd|auth_?token|secret`)},
	{report.Location, wholeName(`(mailing_?)?address|(zip|postal)_?code|ip_?address`)},
}

// wholeName compiles a form in which "_" stands for a separator.
func wholeName(form string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)^(?:` + strings.ReplaceAll(form, "_", `[_-]`) + `)$`)
}

func byNameForm(name string) (report.Category, bool) {
	for _, known := range nameForms {
		if known.form.MatchString(name) {
			return known.category, true
		}
	}
	return "", false
}

// The categories of the API's detection by name, under the category of the report each
// belongs to.
var tokenCategories = map[string]report.Category{
	"email":             report.Contact,
	"phone_number":      report.Contact,
	"username":          report.Personal,
	"person_first_name": report.Personal,
	"person_last_name":  report.Personal,
	"person_full_name":  report.Personal,
	"gender":            report.Personal,
	"birth_date":        report.Personal,
	"ip_address":        report.Location,
	"street_address":    report.Location,
	"city":              report.Location,
	"state":             report.Location,
	"postal_code":       report.Location,
	"country":           report.Location,
	"ssn":               report.NationalID,
	"national_id":       report.NationalID,
	"credit_card":       report.Financial,
	"secret":            report.Authentication,
}

// byNameTokens asks the API's detection by name, which reads the words of a name in
// eight languages and sets aside the names of things, of references, and the names that
// qualify a datum without being one.
func byNameTokens(name, dataType string) (report.Category, bool) {
	classification, ok := piidetect.Classify(name, dataType)
	if !ok || !classification.Sensitive {
		return "", false
	}
	category, ok := tokenCategories[classification.Category]
	return category, ok
}
