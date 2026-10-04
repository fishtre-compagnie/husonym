package rules

import (
	"strconv"

	"github.com/fishtre-compagnie/husonym/backend/pkg/piidetect"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/profile"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/report"
)

// A format is a finding from this share of the non-blank values up.
const minFormatShare = 0.5

// Detectors returns the format checks a profile counts the hits of: those of the API's
// content scan, in its order, from the most constrained to the least.
func Detectors() []profile.Detector {
	checks := piidetect.ValueDetectors()
	detectors := make([]profile.Detector, 0, len(checks))
	for _, check := range checks {
		detectors = append(detectors, profile.Detector{Name: check.Category, Match: check.Match})
	}
	return detectors
}

// The category of the report each format belongs to. A format that is not here makes no
// finding: a SIRET or a SIREN identifies a company, and is left to the model as evidence.
var formatCategories = map[string]report.Category{
	"email":        report.Contact,
	"phone_number": report.Contact,
	"iban":         report.Financial,
	"credit_card":  report.Financial,
	"nir":          report.NationalID,
	"ip_address":   report.Location,
	"gender":       report.Personal,
}

// byValueFormat reads the most frequent format of a profile, whose hits come the most
// frequent first and, between two as frequent, the most constrained first. That one
// format decides: when it makes no finding, a less constrained one that its values also
// pass does not make one in its place.
func byValueFormat(p *profile.Profile) (Finding, bool) {
	if p == nil || len(p.Hits) == 0 {
		return Finding{}, false
	}
	top := p.Hits[0]
	category, known := formatCategories[top.Name]
	if !known || top.Share < minFormatShare {
		return Finding{}, false
	}
	return Finding{
		Category: category,
		Evidence: "values:" + top.Name + " " + strconv.FormatFloat(top.Share, 'g', -1, 64),
	}, true
}
