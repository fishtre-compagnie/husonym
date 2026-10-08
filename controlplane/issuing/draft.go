package issuing

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/internal/license"
)

// The names of the form fields a draft is read from and written to.
const (
	FieldLicenseID          = "license_id"
	FieldCustomerExternalID = "customer_external_id"
	FieldCustomerName       = "customer_name"
	FieldPlan               = "plan"
	// FieldFeatures is repeated, once per feature.
	FieldFeatures = "features"
	// FieldAllFeatures is "1" for a key that lists no feature and allows them all.
	FieldAllFeatures = "all_features"
	// FieldMaxSources is empty for no cap.
	FieldMaxSources = "max_sources"
	// FieldExpiresAt is a UTC day, YYYY-MM-DD: the license holds until the end of it.
	FieldExpiresAt = "expires_at"
	// FieldGraceDays is empty for the default of the product.
	FieldGraceDays = "grace_days"
	// FieldTelemetry is empty for a key that does not say, which the product reads as online.
	FieldTelemetry = "telemetry"
	FieldNote      = "note"
	FieldSucceeds  = "succeeds"
)

const (
	// TrialDays is how long after now a trial expires.
	TrialDays = 30

	// MaxGraceDays is the longest grace period a form can ask for. Far beyond it, the end of the
	// grace is an instant no timestamp holds.
	MaxGraceDays = 3650
	// MaxSourcesCap is the highest cap on sources a form can ask for.
	MaxSourcesCap = 100000
	// MaxExpiryYears is how far after now, in years, a form can set the expiry.
	MaxExpiryYears = 10
	// MaxCustomerNameLength, MaxPlanLength and MaxNoteLength are counted in characters.
	MaxCustomerNameLength = 200
	MaxPlanLength         = 64
	MaxNoteLength         = 1000

	dateLayout     = "2006-01-02"
	licenseIDBytes = 8
	// maxQuoted caps how much of a typed value a problem quotes.
	maxQuoted = 40
)

var (
	// ErrNotTheCustomerOfTheLicense is returned when a renewal is drafted for another customer than
	// the one of the license it renews.
	ErrNotTheCustomerOfTheLicense = errors.New("the license to renew is not a license of that customer")
	// ErrLimitsNotCarried is returned when the license to renew carries a limit a draft cannot
	// carry: renewing it here would issue a key that caps less.
	ErrLimitsNotCarried = errors.New("the license to renew carries a limit that a draft cannot carry")
	// ErrNothingToRenew is returned when a renewal is drafted without a license or without its
	// customer.
	ErrNothingToRenew = errors.New("there is no license to renew, or no customer to renew it for")
	// ErrLicenseIDNotRenewable is returned when the license to renew has an id that is not of the
	// form a draft names its predecessor by.
	ErrLicenseIDNotRenewable = errors.New("the license to renew has an id that is not 16 lowercase hexadecimal characters")
)

// Draft is a license about to be signed: what the operator asked for, before and after the
// confirmation page.
type Draft struct {
	// LicenseID is drawn when the draft is made, so that a confirmation submitted twice names the
	// same license.
	LicenseID          string
	CustomerExternalID string
	// CustomerName is what the key says it was issued to.
	CustomerName string
	Plan         string
	// Features is the list the key carries when AllFeatures is false; none of them allows no
	// optional feature.
	Features []string
	// AllFeatures means no feature list in the key, which allows every feature.
	AllFeatures bool
	// MaxSources is nil for no cap.
	MaxSources *int
	// ExpiresAt is the end of a UTC day, 23:59:59: a form carries the day only.
	ExpiresAt time.Time
	// GraceDays is nil for a key that does not say, which the product reads as its default.
	GraceDays *int
	// Telemetry is empty for a key that does not say, which the product reads as online.
	Telemetry string
	// Note is not part of the key: it is stored beside it.
	Note string
	// Succeeds is the id of the license this one renews, empty when it renews none. It is not part
	// of the key either.
	Succeeds string
}

// Line is a field of the key as the confirmation page shows it.
type Line struct {
	Label string
	Value string
}

// FormOptions is what a form offers, as the product declares it.
type FormOptions struct {
	// Features are the declared features, in their declared order.
	Features []string
	// TelemetryModes are the modes a key can ask for.
	TelemetryModes []string
	// Plans are the declared plan names. The product declares none: a plan is a label nothing is
	// decided from.
	Plans []string
	// PlanIsFreeText tells that the plan is typed and not chosen among Plans.
	PlanIsFreeText bool
	// DefaultGraceDays is the grace period of a key that does not say.
	DefaultGraceDays int
	// MaxGraceDays, MaxSourcesCap and MaxExpiryYears are the bounds ParseDraft holds a form to.
	MaxGraceDays   int
	MaxSourcesCap  int
	MaxExpiryYears int
	// MaxCustomerNameLength, MaxPlanLength and MaxNoteLength are counted in characters.
	MaxCustomerNameLength int
	MaxPlanLength         int
	MaxNoteLength         int
}

// Options gives what a form offers. Every name comes from the product's own declarations.
func Options() FormOptions {
	declared := license.AllFeatures()
	features := make([]string, 0, len(declared))
	for _, feature := range declared {
		features = append(features, string(feature))
	}
	return FormOptions{
		Features: features,
		TelemetryModes: []string{
			string(license.TelemetryOnline),
			string(license.TelemetryOfflineReport),
			string(license.TelemetryNone),
		},
		PlanIsFreeText:        true,
		DefaultGraceDays:      license.DefaultGraceDays,
		MaxGraceDays:          MaxGraceDays,
		MaxSourcesCap:         MaxSourcesCap,
		MaxExpiryYears:        MaxExpiryYears,
		MaxCustomerNameLength: MaxCustomerNameLength,
		MaxPlanLength:         MaxPlanLength,
		MaxNoteLength:         MaxNoteLength,
	}
}

// ParseDraft reads a draft from the fields of a form. The problems are sentences for the page;
// with any of them there is no draft. A missing license id is drawn. It does not replace the
// checks of the signing: it tells the operator before.
func ParseDraft(form url.Values, now time.Time) (draft *Draft, problems []string) {
	problem := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}
	field := func(name string) string { return strings.TrimSpace(form.Get(name)) }

	draft = &Draft{
		LicenseID:          field(FieldLicenseID),
		CustomerExternalID: field(FieldCustomerExternalID),
		CustomerName:       field(FieldCustomerName),
		Plan:               field(FieldPlan),
		Telemetry:          field(FieldTelemetry),
		Note:               field(FieldNote),
		Succeeds:           field(FieldSucceeds),
	}

	if draft.LicenseID == "" {
		draft.LicenseID = newLicenseID()
	}

	// What a form can say that a draft cannot is told here; every rule of a draft is in problems,
	// which the signing applies again.
	switch field(FieldAllFeatures) {
	case "":
		draft.Features = []string{}
	case "1":
		draft.AllFeatures = true
	default:
		problem("The choice of all the features is unreadable.")
	}
	// The names listed are kept even beside all the features, for problems to tell of both.
	draft.Features = append(draft.Features, form[FieldFeatures]...)

	draft.MaxSources = wholeNumber(field(FieldMaxSources), problem, sourcesProblems)
	draft.GraceDays = wholeNumber(field(FieldGraceDays), problem, graceProblems)

	// A date that cannot be read is told once, as unreadable and not as missing too.
	expiryTold := false
	if day := field(FieldExpiresAt); day != "" {
		parsed, err := time.Parse(dateLayout, day)
		if err != nil {
			problem("The expiry date must be written YYYY-MM-DD.")
			expiryTold = true
		} else {
			draft.ExpiresAt = endOfDay(parsed)
		}
	}

	problems = append(problems, draft.problems(now, expiryTold)...)
	if len(problems) > 0 {
		return nil, problems
	}
	return draft, nil
}

// numberProblems are the sentences for a number that cannot be read, is below zero, or is over
// its bound.
type numberProblems struct {
	highest                       int
	unreadable, negative, tooHigh string
}

var (
	sourcesProblems = numberProblems{
		highest:    MaxSourcesCap,
		unreadable: "The maximum number of sources must be a whole number.",
		negative:   "The maximum number of sources cannot be negative.",
		tooHigh:    fmt.Sprintf("The maximum number of sources cannot be more than %d.", MaxSourcesCap),
	}
	graceProblems = numberProblems{
		highest:    MaxGraceDays,
		unreadable: "The grace period must be a whole number of days.",
		negative:   "The grace period cannot be negative.",
		tooHigh:    fmt.Sprintf("The grace period cannot be more than %d days.", MaxGraceDays),
	}
)

// outOfBounds gives the sentence for a number outside zero to highest; nothing for one inside, or
// for a number not written.
func (p numberProblems) outOfBounds(n *int) string {
	switch {
	case n == nil:
		return ""
	case *n < 0:
		return p.negative
	case *n > p.highest:
		return p.tooHigh
	default:
		return ""
	}
}

// wholeNumber reads a number; empty is nil, which means not written. Its bounds are for the rules
// of the draft to tell, but for a number no int holds, which is told here.
func wholeNumber(typed string, problem func(string, ...any), sentences numberProblems) *int {
	if typed == "" {
		return nil
	}
	n, err := strconv.Atoi(typed)
	switch {
	case errors.Is(err, strconv.ErrRange) && strings.HasPrefix(typed, "-"):
		problem("%s", sentences.negative)
	case errors.Is(err, strconv.ErrRange):
		problem("%s", sentences.tooHigh)
	case err != nil:
		problem("%s", sentences.unreadable)
	default:
		return &n
	}
	return nil
}

// problems gives what keeps the draft from being signed, as sentences for the page: every rule of
// a draft is here and nowhere else. ParseDraft tells them to the operator and Signer.Issue refuses
// on them, so a draft built by hand is held to what a form is held to. expiryTold leaves out the
// missing expiry, for a form whose date was already told as unreadable.
func (d *Draft) problems(now time.Time, expiryTold bool) []string {
	var problems []string
	problem := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}
	// shown is a text the key carries and a page shows: it must be the same to the eye as in the
	// bytes that are signed.
	shown := func(subject, text string, maxLength int) {
		switch {
		case !carriable(text, false):
			problem("%s holds a control or formatting character, or text that is not valid UTF-8.", subject)
		case text != strings.TrimSpace(text):
			problem("%s cannot begin or end with a space.", subject)
		case maxLength > 0 && utf8.RuneCountInString(text) > maxLength:
			problem("%s must be at most %d characters.", subject, maxLength)
		}
	}

	// Without an id of its own the draft would be given a new one at each signing, and a
	// confirmation submitted twice would issue two licenses.
	if !validLicenseID(d.LicenseID) {
		problem("The license id must be 16 lowercase hexadecimal characters.")
	}
	if d.CustomerExternalID == "" {
		problem("The customer is required.")
	}
	shown("The customer id", d.CustomerExternalID, 0)
	if d.CustomerName == "" {
		problem("The name of the customer is required.")
	}
	shown("The name of the customer", d.CustomerName, MaxCustomerNameLength)
	shown("The plan", d.Plan, MaxPlanLength)
	// The note is not in the key and may be of several lines.
	switch {
	case !carriable(d.Note, true):
		problem("The note holds a control or formatting character, or text that is not valid UTF-8.")
	case utf8.RuneCountInString(d.Note) > MaxNoteLength:
		problem("The note must be at most %d characters.", MaxNoteLength)
	}

	if d.AllFeatures && len(d.Features) > 0 {
		problem("Choose either all the features or a list of features, not both.")
	}
	// The wildcard is refused with the unknown names: every feature is said by AllFeatures, which
	// writes no list.
	seen := map[string]bool{}
	for _, name := range d.Features {
		switch _, declared := license.ParseFeature(name); {
		case !declared:
			problem("The feature %s is not a declared feature.", quoted(name))
		case seen[name]:
			problem("The feature %s is listed twice.", quoted(name))
		}
		seen[name] = true
	}

	for _, sentence := range []string{sourcesProblems.outOfBounds(d.MaxSources), graceProblems.outOfBounds(d.GraceDays)} {
		if sentence != "" {
			problem("%s", sentence)
		}
	}

	switch {
	case d.ExpiresAt.IsZero():
		if !expiryTold {
			problem("The expiry date is required.")
		}
	case !d.ExpiresAt.After(now):
		problem("The expiry date is in the past.")
	case d.ExpiresAt.After(endOfDay(now.UTC().AddDate(MaxExpiryYears, 0, 0))):
		problem("The expiry date cannot be more than %d years from now.", MaxExpiryYears)
	}

	if modes := Options().TelemetryModes; d.Telemetry != "" && !slices.Contains(modes, d.Telemetry) {
		problem("The telemetry mode %s is not one of %s.", quoted(d.Telemetry), strings.Join(modes, ", "))
	}
	if d.Succeeds != "" && !validLicenseID(d.Succeeds) {
		problem("The license to renew must be named by an id of 16 lowercase hexadecimal characters.")
	}
	return problems
}

// carriable reports whether text can be carried as it is: valid UTF-8 without a control character.
// Of several lines, it may hold the line breaks and tabs such a text has. Of one line, it is a
// text a page shows from a key, and it holds no format character either — the bidirectional
// overrides, the zero-width ones — nor a line or paragraph separator: what is signed must be what
// is seen.
func carriable(text string, severalLines bool) bool {
	if !utf8.ValidString(text) {
		return false
	}
	for _, r := range text {
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

// quoted quotes the beginning of a typed value, for a problem: short, and with nothing in it that
// breaks out of a line. The page escapes it.
func quoted(typed string) string {
	runes := []rune(typed)
	if len(runes) > maxQuoted {
		return strconv.Quote(string(runes[:maxQuoted]) + "…")
	}
	return strconv.Quote(typed)
}

// TrialDraft drafts a trial for customer: every feature, no cap, and an expiry at the end of the
// day TrialDays after now. The name and the id of the customer are trimmed as ParseDraft trims
// them, so that the page that shows this draft and the key signed after it say the same. Without a
// customer there is no draft: it returns nil.
func TrialDraft(customer *cpstore.CustomerDetail, now time.Time) *Draft {
	if customer == nil {
		return nil
	}
	return &Draft{
		LicenseID:          newLicenseID(),
		CustomerExternalID: strings.TrimSpace(customer.ExternalID),
		CustomerName:       strings.TrimSpace(customer.Name),
		AllFeatures:        true,
		ExpiresAt:          endOfDay(now.UTC().AddDate(0, 0, TrialDays)),
	}
}

// RenewalDraft drafts the license that succeeds previous: the same content under a new id,
// expiring at the end of the day one year after the later of now and the previous expiry. The name
// is the one customer has today, and the telemetry is what the key of previous says, not what the
// product reads from it. Texts are trimmed as ParseDraft trims them. It refuses, with an error: a
// missing license or customer (ErrNothingToRenew), a license of another customer, a license whose
// id a draft cannot name as its predecessor, and one that carries a limit a draft has no field
// for.
func RenewalDraft(previous *cpstore.LicenseDetail, customer *cpstore.CustomerDetail, now time.Time) (*Draft, error) {
	if previous == nil || customer == nil {
		return nil, ErrNothingToRenew
	}
	if previous.CustomerID != customer.ID {
		return nil, ErrNotTheCustomerOfTheLicense
	}
	if !validLicenseID(previous.ID) {
		return nil, ErrLicenseIDNotRenewable
	}
	draft := &Draft{
		LicenseID:          newLicenseID(),
		CustomerExternalID: strings.TrimSpace(customer.ExternalID),
		CustomerName:       strings.TrimSpace(customer.Name),
		Plan:               strings.TrimSpace(previous.Plan),
		Telemetry:          strings.TrimSpace(previous.StoredTelemetry),
		Succeeds:           previous.ID,
	}
	// A key that lists the wildcard allows what a key without a list allows.
	if previous.Features == nil || slices.Contains(previous.Features, license.FeatureWildcard) {
		draft.AllFeatures = true
	} else {
		draft.Features = append([]string{}, previous.Features...)
	}
	if limits := previous.Limits; limits != nil {
		if limits.MaxJobs != nil || limits.MaxConnections != nil || len(limits.AllowedConnectionTypes) > 0 {
			return nil, ErrLimitsNotCarried
		}
		draft.MaxSources = copyInt(limits.MaxSources)
	}
	draft.GraceDays = copyInt(previous.GraceDays)

	from := now
	if previous.ExpiresAt.After(now) {
		from = previous.ExpiresAt
	}
	draft.ExpiresAt = endOfDay(from.UTC().AddDate(1, 0, 0))
	return draft, nil
}

// Form writes the draft as the fields ParseDraft reads, to carry it through the confirmation page.
// A form carries the day of the expiry only: an expiry that is not the end of a UTC day comes back
// from ParseDraft as the end of the UTC day it falls on.
func (d *Draft) Form() url.Values {
	form := url.Values{}
	form.Set(FieldLicenseID, d.LicenseID)
	form.Set(FieldCustomerExternalID, d.CustomerExternalID)
	form.Set(FieldCustomerName, d.CustomerName)
	form.Set(FieldPlan, d.Plan)
	if d.AllFeatures {
		form.Set(FieldAllFeatures, "1")
	}
	for _, name := range d.Features {
		form.Add(FieldFeatures, name)
	}
	if d.MaxSources != nil {
		form.Set(FieldMaxSources, strconv.Itoa(*d.MaxSources))
	}
	if !d.ExpiresAt.IsZero() {
		form.Set(FieldExpiresAt, d.ExpiresAt.UTC().Format(dateLayout))
	}
	if d.GraceDays != nil {
		form.Set(FieldGraceDays, strconv.Itoa(*d.GraceDays))
	}
	form.Set(FieldTelemetry, d.Telemetry)
	form.Set(FieldNote, d.Note)
	form.Set(FieldSucceeds, d.Succeeds)
	return form
}

// Lines gives each field of the key the draft would sign, in a fixed order, as the product would
// read it.
func (d *Draft) Lines() []Line {
	features := "all"
	switch {
	case d.AllFeatures:
	case len(d.Features) == 0:
		features = "none"
	default:
		features = strings.Join(d.Features, ", ")
	}
	sources := "no cap"
	if d.MaxSources != nil {
		sources = strconv.Itoa(*d.MaxSources)
	}
	grace := fmt.Sprintf("%d days (default)", license.DefaultGraceDays)
	switch {
	case d.GraceDays == nil:
	case *d.GraceDays == 0:
		grace = "none"
	default:
		grace = fmt.Sprintf("%d days", *d.GraceDays)
	}
	telemetry := d.Telemetry
	if telemetry == "" {
		telemetry = string(license.TelemetryOnline) + " (not written)"
	}
	plan := d.Plan
	if plan == "" {
		plan = "none"
	}
	return []Line{
		{Label: "License id", Value: d.LicenseID},
		{Label: "Issued to", Value: d.CustomerName},
		{Label: "Customer id", Value: d.CustomerExternalID},
		{Label: "Plan", Value: plan},
		{Label: "Features", Value: features},
		{Label: "Maximum sources", Value: sources},
		{Label: "Expires", Value: d.ExpiresAt.UTC().Format("2006-01-02 15:04:05 MST")},
		{Label: "Grace period", Value: grace},
		{Label: "Telemetry", Value: telemetry},
	}
}

// request is the draft as the issuing code of the product takes it. The note and the license
// succeeded are not in it: a key has no field for them.
func (d *Draft) request(now time.Time) *license.IssueRequest {
	req := &license.IssueRequest{
		Id:         d.LicenseID,
		IssuedTo:   d.CustomerName,
		CustomerId: d.CustomerExternalID,
		IssuedAt:   now,
		ExpiresAt:  d.ExpiresAt,
		GraceDays:  copyInt(d.GraceDays),
		Plan:       d.Plan,
		Telemetry:  d.Telemetry,
	}
	if d.MaxSources != nil {
		req.Limits = &license.Limits{MaxSources: copyInt(d.MaxSources)}
	}
	// Left nil for all the features: the key then carries no list. Otherwise the list is never
	// nil, and an empty one allows no optional feature.
	if !d.AllFeatures {
		req.Features = append([]string{}, d.Features...)
	}
	return req
}

func copyInt(n *int) *int {
	if n == nil {
		return nil
	}
	value := *n
	return &value
}

// endOfDay is the last second of the UTC day of t.
func endOfDay(t time.Time) time.Time {
	year, month, day := t.UTC().Date()
	return time.Date(year, month, day, 23, 59, 59, 0, time.UTC)
}

// newLicenseID draws a license id, of the form the issuing code of the product draws.
func newLicenseID() string {
	b := make([]byte, licenseIDBytes)
	// crypto/rand.Read never returns an error: it fills b or stops the program.
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// validLicenseID reports whether id has the form of a drawn id: 16 lowercase hexadecimal characters.
func validLicenseID(id string) bool {
	if len(id) != 2*licenseIDBytes {
		return false
	}
	for _, c := range id {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
