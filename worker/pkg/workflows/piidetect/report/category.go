package report

// Category is a kind of personal data. Its names are stored in the reports and shown by
// their readers.
type Category string

const (
	NationalID     Category = "national_id"
	Contact        Category = "contact"
	Financial      Category = "financial"
	Personal       Category = "personal"
	Location       Category = "location"
	Authentication Category = "authentication"
)

// Categories are the six categories, in the order they are documented.
var Categories = []Category{NationalID, Contact, Financial, Personal, Location, Authentication}

// Valid tells whether c is one of the six categories.
func (c Category) Valid() bool {
	switch c {
	case NationalID, Contact, Financial, Personal, Location, Authentication:
		return true
	}
	return false
}
