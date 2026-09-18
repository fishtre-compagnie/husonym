package report

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"

	"github.com/fishtre-compagnie/husonym/bench/env"
	"github.com/fishtre-compagnie/husonym/bench/schema"
)

// Baseline lists the known gaps: the verdict of every case on every engine the last
// time the baseline was recorded. Fixing an engine empties it; the bench fails when a
// case does worse than its baseline, and a case missing from it must be OK.
//
// Verdicts are recorded per database first: the same case says different things on
// MySQL and on PostgreSQL, and one must never be compared with, or overwrite, the other.
type Baseline map[schema.Dialect]map[string]map[env.Engine]Verdict

// ReadBaseline loads the baseline, empty when the file does not exist yet.
func ReadBaseline(path string) (Baseline, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Baseline{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("report: %w", err)
	}
	var baseline Baseline
	if err := json.Unmarshal(data, &baseline); err != nil {
		return nil, fmt.Errorf("report: %s: %w", path, err)
	}
	return baseline, nil
}

// Merge records the verdicts of a report, keeping the cases the report did not run and
// the databases it does not concern.
func (b Baseline) Merge(r *Report) {
	if b[r.Dialect] == nil {
		b[r.Dialect] = map[string]map[env.Engine]Verdict{}
	}
	known := b[r.Dialect]
	for _, c := range r.Cases {
		if known[c.ID] == nil {
			known[c.ID] = map[env.Engine]Verdict{}
		}
		for _, o := range c.Outcomes {
			if o.Verdict != VerdictNotExercised {
				known[c.ID][o.Engine] = o.Verdict
			}
		}
	}
}

// Write stores the baseline.
func (b Baseline) Write(path string) error {
	encoded, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("report: %w", err)
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0o600); err != nil {
		return fmt.Errorf("report: %w", err)
	}
	return nil
}

// Regressions lists the outcomes that are not OK while the baseline says OK, or says
// nothing. A known gap that changes nature (a gap becoming a failed run) is not a
// regression of the baseline, but it shows in the report.
func (b Baseline) Regressions(r *Report) []string {
	var regressions []string
	known := b[r.Dialect]
	for _, c := range r.Cases {
		for _, o := range c.Outcomes {
			if o.Verdict == VerdictOK || o.Verdict == VerdictNotExercised {
				continue
			}
			recordedVerdict, recorded := known[c.ID][o.Engine]
			if !recorded || recordedVerdict == VerdictOK {
				regressions = append(regressions, fmt.Sprintf("%s sur %s : %s", c.ID, o.Engine, verdictLabels[o.Verdict]))
			}
		}
	}
	sort.Strings(regressions)
	return regressions
}
