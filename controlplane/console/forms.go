package console

import (
	"net/url"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/controlplane/issuing"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/google/uuid"
)

// What the forms, the pages that answer them and the journal receive.

const (
	// journalLines is how many lines the page of the journal shows, the newest.
	journalLines = 200

	// The fields of the form of a customer.
	fieldExternalID = "external_id"
	fieldName       = "name"
	fieldNote       = "note"
	// fieldCustomer carries, beside the fields of a draft, the id of the customer the draft is for:
	// what the draft says of the customer is checked against the store through it.
	fieldCustomer = "customer"
	// queryTrial asks, with the value "1", for the form of a license prefilled as a trial.
	queryTrial = "trial"

	// maxExternalIDLength is counted in characters, as the lengths of a draft are.
	maxExternalIDLength = 200
)

type customerFormView struct {
	Heading string
	// Action is where the form is sent.
	Action string
	// Customer is the customer edited; it has no Href on the form of a new one.
	Customer link
	// New tells the customer is to be created: its external id is typed. It is only shown otherwise.
	New        bool
	ExternalID string
	Name       string
	Note       string
	Problems   []string

	MaxExternalIDLength int
	MaxNameLength       int
	MaxNoteLength       int
}

// choice is one of the values a field offers.
type choice struct {
	Value  string
	Label  string
	Chosen bool
}

type licenseFormView struct {
	Heading  string
	Customer link
	// CustomerID is the id of the customer in the store; the two next are what the key will carry.
	CustomerID         string
	CustomerExternalID string
	CustomerName       string
	// Renews is the license the new one succeeds, nil when it succeeds none.
	Renews   *link
	Problems []string

	LicenseID string
	Succeeds  string
	Plan      string
	// Plans is offered in place of a typed plan when the product declares its plans.
	Plans       []choice
	AllFeatures bool
	Features    []choice
	MaxSources  string
	ExpiresAt   string
	GraceDays   string
	Telemetry   []choice
	Note        string
	// Options holds the bounds the fields say.
	Options issuing.FormOptions
}

type licenseConfirmView struct {
	Customer link
	// Lines are the fields of the key about to be signed.
	Lines []issuing.Line
	// Note and Renews are stored beside the key: it has no field for them.
	Note   string
	Renews *link
	// Hidden is the draft, as the fields the form carries.
	Hidden []fact
}

type licenseKeyView struct {
	Heading string
	License link
	Key     string
}

type journalView struct {
	Lines []journalRow
	// Truncated tells the page holds as many lines as it may, so that older ones may exist.
	Truncated bool
	Cap       int
}

type journalRow struct {
	At       string
	Operator string
	// Action is what was done, as a phrase.
	Action string
	// Customer and License have no Text when the act was on none.
	Customer link
	License  link
	Detail   string
}

// typed is a value of a form as the store and the key take it: without the space around it.
func typed(form url.Values, name string) string {
	return strings.TrimSpace(form.Get(name))
}

// writable says whether a value typed in a form is text the console records: valid UTF-8 without a
// control character. A text of several lines may hold its line breaks and tabs; one of a single
// line is held to what a draft holds the texts of a key to, so that a customer recorded here can
// be issued a license: no format character, no line or paragraph separator either.
func writable(value string, severalLines bool) bool {
	if !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if severalLines {
			if r != '\n' && r != '\r' && r != '\t' && unicode.IsControl(r) {
				return false
			}
			continue
		}
		if unicode.In(r, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp) {
			return false
		}
	}
	return true
}

// customerProblems says, in sentences for the page, what of a customer typed cannot be recorded.
// A new customer is judged on its external id too.
func customerProblems(form url.Values, isNew bool) []string {
	options := issuing.Options()
	var problems []string
	check := func(subject, value string, maxLength int, required, severalLines bool) {
		switch {
		case required && value == "":
			problems = append(problems, subject+" is required.")
		case !writable(value, severalLines):
			problems = append(problems, subject+" holds a control or formatting character, or text that is not valid UTF-8.")
		case utf8.RuneCountInString(value) > maxLength:
			problems = append(problems, subject+" is too long.")
		}
	}
	if isNew {
		check("The external id", typed(form, fieldExternalID), maxExternalIDLength, true, false)
	}
	check("The name", typed(form, fieldName), options.MaxCustomerNameLength, true, false)
	check("The note", typed(form, fieldNote), options.MaxNoteLength, false, true)
	return problems
}

func newCustomerFormView(customer *cpstore.CustomerDetail, form url.Values, problems []string) *customerFormView {
	options := issuing.Options()
	view := &customerFormView{
		Heading:             "New customer",
		Action:              "/customers",
		New:                 customer == nil,
		ExternalID:          form.Get(fieldExternalID),
		Name:                form.Get(fieldName),
		Note:                form.Get(fieldNote),
		Problems:            problems,
		MaxExternalIDLength: maxExternalIDLength,
		MaxNameLength:       options.MaxCustomerNameLength,
		MaxNoteLength:       options.MaxNoteLength,
	}
	if customer != nil {
		view.Customer = customerLink(customer.ID, customer.Name)
		view.Heading = "Edit the customer"
		view.Action = view.Customer.Href
		view.ExternalID = customer.ExternalID
	}
	return view
}

// customerForm is the fields of the form of a customer, filled with what the store holds of it.
func customerForm(customer *cpstore.CustomerDetail) url.Values {
	return url.Values{fieldName: {customer.Name}, fieldNote: {customer.Note}}
}

// renewed is the link to the license a draft succeeds, nil when it succeeds none.
func renewed(succeeds string) *link {
	if succeeds == "" {
		return nil
	}
	previous := licenseLink(succeeds)
	return &previous
}

// newLicenseFormView is the form of a license for customer, its fields filled from form: a draft
// just made, or what the operator sent when it has problems.
func newLicenseFormView(customer *cpstore.CustomerDetail, form url.Values, problems []string) *licenseFormView {
	options := issuing.Options()
	view := &licenseFormView{
		Heading:            "New license",
		Customer:           customerLink(customer.ID, customer.Name),
		CustomerID:         customer.ID.String(),
		CustomerExternalID: customer.ExternalID,
		CustomerName:       customer.Name,
		Renews:             renewed(typed(form, issuing.FieldSucceeds)),
		Problems:           problems,
		LicenseID:          form.Get(issuing.FieldLicenseID),
		Succeeds:           form.Get(issuing.FieldSucceeds),
		Plan:               form.Get(issuing.FieldPlan),
		AllFeatures:        form.Get(issuing.FieldAllFeatures) != "",
		MaxSources:         form.Get(issuing.FieldMaxSources),
		ExpiresAt:          form.Get(issuing.FieldExpiresAt),
		GraceDays:          form.Get(issuing.FieldGraceDays),
		Note:               form.Get(issuing.FieldNote),
		Options:            options,
	}
	if view.Renews != nil {
		view.Heading = "Renew a license"
	}
	if !options.PlanIsFreeText {
		view.Plans = []choice{{Label: "none", Chosen: view.Plan == ""}}
		for _, plan := range options.Plans {
			view.Plans = append(view.Plans, choice{Value: plan, Label: plan, Chosen: plan == view.Plan})
		}
	}
	for _, feature := range options.Features {
		view.Features = append(view.Features, choice{
			Value: feature, Label: feature, Chosen: slices.Contains(form[issuing.FieldFeatures], feature),
		})
	}
	telemetry := form.Get(issuing.FieldTelemetry)
	view.Telemetry = []choice{{
		Label:  "not written (the product reads it as " + string(license.TelemetryOnline) + ")",
		Chosen: telemetry == "",
	}}
	for _, mode := range options.TelemetryModes {
		view.Telemetry = append(view.Telemetry, choice{Value: mode, Label: mode, Chosen: mode == telemetry})
	}
	return view
}

func newLicenseConfirmView(customer *cpstore.CustomerDetail, draft *issuing.Draft) *licenseConfirmView {
	view := &licenseConfirmView{
		Customer: customerLink(customer.ID, customer.Name),
		Lines:    draft.Lines(),
		Note:     orAbsent(draft.Note),
		Renews:   renewed(draft.Succeeds),
		Hidden:   []fact{{Name: fieldCustomer, Value: customer.ID.String()}},
	}
	form := draft.Form()
	names := make([]string, 0, len(form))
	for name := range form {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, value := range form[name] {
			view.Hidden = append(view.Hidden, fact{Name: name, Value: value})
		}
	}
	return view
}

// actionPhrases says each act of the journal as a phrase.
var actionPhrases = map[cpstore.OperatorActionKind]string{
	cpstore.ActionCustomerCreated: "recorded the customer",
	cpstore.ActionCustomerUpdated: "changed the customer",
	cpstore.ActionLicenseIssued:   "issued the license",
	cpstore.ActionLicenseRenewed:  "issued the license, as a renewal",
	cpstore.ActionLicenseKeyShown: "was shown the key of the license again",
}

func newJournalView(actions []cpstore.OperatorAction) *journalView {
	view := &journalView{
		Lines:     make([]journalRow, 0, len(actions)),
		Truncated: len(actions) >= journalLines,
		Cap:       journalLines,
	}
	for i := range actions {
		action := &actions[i]
		row := journalRow{
			At:       instant(action.At),
			Operator: action.Operator,
			Action:   actionPhrases[action.Action],
			Detail:   detailText(action.Detail),
		}
		if row.Action == "" {
			row.Action = string(action.Action)
		}
		if action.CustomerID != uuid.Nil {
			row.Customer = customerLink(action.CustomerID, action.CustomerName)
			if row.Customer.Text == "" {
				row.Customer.Text = action.CustomerID.String()
			}
		}
		if action.LicenseID != "" {
			row.License = licenseLink(action.LicenseID)
		}
		view.Lines = append(view.Lines, row)
	}
	return view
}

// detailText writes the detail of a line of the journal as one short text, its names in order. A
// name without a value, as the plan of a license that has none, is left out.
func detailText(detail map[string]string) string {
	names := make([]string, 0, len(detail))
	for name, value := range detail {
		if value != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, strings.ReplaceAll(name, "_", " ")+": "+detail[name])
	}
	return strings.Join(parts, "; ")
}
