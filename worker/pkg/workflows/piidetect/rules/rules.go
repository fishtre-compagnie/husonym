// Package rules finds personal data in a column without a model: from the name of the
// column and, when its values were sampled, from the formats most of them have.
package rules

import (
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/profile"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/report"
)

// Version names the rules. It enters the fingerprint of a table, so that tables scanned
// under other rules are scanned again; it changes with what Find answers.
const Version = "4"

// Evidence of a finding that rests on the name of the column.
const evidenceName = "name"

type Finding struct {
	Category report.Category
	// Evidence says what the finding rests on: "name", or "values:<format> <share>".
	Evidence string
}

// Find says whether a column holds personal data, from its name and type and, when it has
// one, from its profile. ok is false when nothing says so: that is not evidence of the
// contrary.
//
// The name answers first. A name that says personal data is not withdrawn because the
// values do not confirm it: a format check that stays silent proves nothing.
func Find(name, dataType string, p *profile.Profile) (f Finding, ok bool) {
	if category, ok := byName(name, dataType); ok {
		return Finding{Category: category, Evidence: evidenceName}, true
	}
	return byValueFormat(p)
}
