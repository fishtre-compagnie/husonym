// Package report records what a bench run found, as JSON for tools and as Markdown for
// people, and compares it with the baseline of known gaps.
package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fishtre-compagnie/husonym/bench/cases"
	"github.com/fishtre-compagnie/husonym/bench/env"
	"github.com/fishtre-compagnie/husonym/bench/schema"
	"github.com/fishtre-compagnie/husonym/bench/verify"
)

// Verdict is the outcome of one case on one engine.
type Verdict string

const (
	// VerdictOK: the run did what the case expects.
	VerdictOK Verdict = "ok"
	// VerdictGap: the run completed and the destination is off the expectation.
	VerdictGap Verdict = "gap"
	// VerdictRunFailed: the run failed while the case expects it to complete.
	VerdictRunFailed Verdict = "run_failed"
	// VerdictRunTimeout: the run was still going when the bench stopped waiting for it.
	VerdictRunTimeout Verdict = "run_timeout"
	// VerdictNotExercised: the bench settings cannot trigger what the case is about (page
	// size too small); the case was not run and the baseline keeps its last verdict.
	VerdictNotExercised Verdict = "not_exercised"
	// VerdictFailureExpected: the case expects the run to fail with a given message, and
	// it completed or failed with another one.
	VerdictFailureExpected Verdict = "failure_expected"
)

var verdictLabels = map[Verdict]string{
	VerdictOK:              "OK",
	VerdictGap:             "écart",
	VerdictRunFailed:       "échec du run",
	VerdictRunTimeout:      "run sans fin",
	VerdictNotExercised:    "non exercé",
	VerdictFailureExpected: "échec attendu absent",
}

// Outcome is what one engine did with one case.
type Outcome struct {
	Engine       env.Engine     `json:"engine"`
	Verdict      Verdict        `json:"verdict"`
	Gaps         int            `json:"gaps"`
	RunStatus    string         `json:"runStatus"`
	DurationMs   int64          `json:"durationMs"`
	Errors       []string       `json:"errors,omitempty"`
	Verification *verify.Result `json:"verification,omitempty"`
}

// CaseReport gathers the outcomes of one case.
type CaseReport struct {
	ID         string         `json:"id"`
	Priority   string         `json:"priority"`
	Title      string         `json:"title"`
	SourceRows map[string]int `json:"sourceRows"`
	Outcomes   []*Outcome     `json:"outcomes"`
}

// Report is one bench run, on one database.
type Report struct {
	Date    time.Time      `json:"date"`
	Commit  string         `json:"commit"`
	Dialect schema.Dialect `json:"dialect"`
	Params  cases.Params   `json:"params"`
	Cases   []*CaseReport  `json:"cases"`
}

// Write stores report.json and report.md in dir.
func (r *Report) Write(dir string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("report: %w", err)
	}
	encoded, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("report: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "report.json"), append(encoded, '\n'), 0o600); err != nil {
		return fmt.Errorf("report: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "report.md"), []byte(r.Markdown()), 0o600); err != nil {
		return fmt.Errorf("report: %w", err)
	}
	return nil
}

// Markdown renders the report for people: a summary, then the detail of every case that
// is not OK on every engine.
func (r *Report) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Banc d'essai des moteurs\n\n")
	fmt.Fprintf(&b, "- Date : %s\n- Commit : `%s`\n- SGBD : %s\n- Taille de page : %d · échelle : %d\n\n",
		r.Date.Format("2006-01-02 15:04:05"), r.Commit, r.Dialect, r.Params.PageLimit, r.Params.Scale)

	b.WriteString("## Synthèse\n\n| Priorité | Moteur | OK | Écart | Échec du run | Run sans fin | Échec attendu absent | Non exercé |\n" +
		"|---|---|---|---|---|---|---|---|\n")
	type cell struct{ priority, engine string }
	counts := map[cell]map[Verdict]int{}
	for _, c := range r.Cases {
		for _, o := range c.Outcomes {
			key := cell{c.Priority, string(o.Engine)}
			if counts[key] == nil {
				counts[key] = map[Verdict]int{}
			}
			counts[key][o.Verdict]++
		}
	}
	keys := make([]cell, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].priority != keys[j].priority {
			return keys[i].priority < keys[j].priority
		}
		return keys[i].engine < keys[j].engine
	})
	for _, key := range keys {
		n := counts[key]
		fmt.Fprintf(&b, "| %s | %s | %d | %d | %d | %d | %d | %d |\n", key.priority, key.engine,
			n[VerdictOK], n[VerdictGap], n[VerdictRunFailed], n[VerdictRunTimeout], n[VerdictFailureExpected],
			n[VerdictNotExercised])
	}

	b.WriteString("\n## Cas\n\n| Cas | Priorité |")
	for _, engine := range env.Engines {
		fmt.Fprintf(&b, " %s |", engine)
	}
	b.WriteString("\n|---|---|")
	b.WriteString(strings.Repeat("---|", len(env.Engines)))
	b.WriteString("\n")
	for _, c := range r.Cases {
		fmt.Fprintf(&b, "| `%s` | %s |", c.ID, c.Priority)
		for _, engine := range env.Engines {
			b.WriteString(" " + summarize(c.outcome(engine)) + " |")
		}
		b.WriteString("\n")
	}

	b.WriteString("\n## Détail des cas hors attendu\n")
	for _, c := range r.Cases {
		if c.allOK() {
			continue
		}
		fmt.Fprintf(&b, "\n### `%s` (%s)\n\n%s\n\n", c.ID, c.Priority, c.Title)
		for _, o := range c.Outcomes {
			if o.Verdict == VerdictOK || o.Verdict == VerdictNotExercised {
				continue
			}
			fmt.Fprintf(&b, "**%s** : %s, run %s en %d ms\n\n", o.Engine, verdictLabels[o.Verdict], o.RunStatus, o.DurationMs)
			for _, message := range o.Errors {
				fmt.Fprintf(&b, "- erreur : `%s`\n", strings.ReplaceAll(message, "`", "'"))
			}
			if o.Verification != nil {
				writeVerification(&b, o.Verification)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

func writeVerification(b *strings.Builder, v *verify.Result) {
	for _, t := range v.Tables {
		if t.Gaps() == 0 {
			continue
		}
		fmt.Fprintf(b, "- `%s` : %d lignes attendues, %d écrites ; %d manquantes, %d hors subset, %d sans origine, %d en double\n",
			t.Table, t.ExpectedRows, t.DestinationRows, t.Missing, t.Leaked, t.Unexpected, t.Duplicated)
		rules := make([]string, 0, len(t.RuleViolations))
		for rule := range t.RuleViolations {
			rules = append(rules, rule)
		}
		sort.Strings(rules)
		for _, rule := range rules {
			fmt.Fprintf(b, "  - règle `%s` : %d valeurs\n", rule, t.RuleViolations[rule])
		}
		for _, sample := range t.Samples {
			//nolint:misspell // message produit, rédigé en français
			fmt.Fprintf(b, "  - exemple : %s\n", sample)
		}
	}
	for _, o := range v.Orphans {
		fmt.Fprintf(b, "- `%s` : %d références orphelines sur `%s`\n", o.Table, o.Orphans, o.ForeignKey)
	}
}

func summarize(o *Outcome) string {
	if o == nil {
		return "—"
	}
	if o.Verdict == VerdictGap {
		return fmt.Sprintf("%s (%d)", verdictLabels[o.Verdict], o.Gaps)
	}
	return verdictLabels[o.Verdict]
}

func (c *CaseReport) outcome(engine env.Engine) *Outcome {
	for _, o := range c.Outcomes {
		if o.Engine == engine {
			return o
		}
	}
	return nil
}

func (c *CaseReport) allOK() bool {
	for _, o := range c.Outcomes {
		if o.Verdict != VerdictOK && o.Verdict != VerdictNotExercised {
			return false
		}
	}
	return true
}
