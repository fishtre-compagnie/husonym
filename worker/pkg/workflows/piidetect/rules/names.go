package rules

import (
	"github.com/fishtre-compagnie/husonym/backend/pkg/piidetect"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/report"
)

// The categories of the API's detection by name, under the category of the report each
// belongs to.
var nameCategories = map[string]report.Category{
	"email":             report.Contact,
	"phone_number":      report.Contact,
	"username":          report.Personal,
	"person_first_name": report.Personal,
	"person_last_name":  report.Personal,
	"person_full_name":  report.Personal,
	"gender":            report.Personal,
	"birth_date":        report.Personal,
	"age":               report.Personal,
	"ethnicity":         report.Personal,
	"ip_address":        report.Location,
	"street_address":    report.Location,
	"city":              report.Location,
	"state":             report.Location,
	"postal_code":       report.Location,
	"country":           report.Location,
	"ssn":               report.NationalID,
	"national_id":       report.NationalID,
	"credit_card":       report.Financial,
	"iban":              report.Financial,
	"bank_account":      report.Financial,
	"salary":            report.Financial,
	"secret":            report.Authentication,
}

// byName asks the API's detection by name, which reads the words of a name in eight
// languages and sets aside the names of things, of references, and the names that
// qualify a datum without being one. The scan of a connection and this job answer from
// the same rules.
func byName(name, dataType string) (report.Category, bool) {
	classification, ok := piidetect.Classify(name, dataType)
	if !ok || !classification.Sensitive {
		return "", false
	}
	category, ok := nameCategories[classification.Category]
	return category, ok
}
