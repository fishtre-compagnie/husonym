package perf

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fishtre-compagnie/husonym/bench/env"
)

// ActivitySummary gathers the activities of one type in a run.
type ActivitySummary struct {
	Type    string        `json:"type"`
	Count   int           `json:"count"`
	Total   time.Duration `json:"total"`
	Longest time.Duration `json:"longest"`
}

// Measure is one run of one engine.
type Measure struct {
	Round           int           `json:"round"`
	Engine          env.Engine    `json:"engine"`
	Status          string        `json:"status"`
	Duration        time.Duration `json:"duration"`
	PeakMemoryBytes int64         `json:"peakMemoryBytes"`
	// AddedMemoryBytes is what the run added to the memory the container already held.
	AddedMemoryBytes int64             `json:"addedMemoryBytes"`
	PageLimit        int               `json:"pageLimit"`
	Rows             map[string]int    `json:"rows"`
	Activities       []ActivitySummary `json:"activities"`
	Errors           []string          `json:"errors,omitempty"`
}

// WrittenRows is how many rows the run wrote, all tables together.
func (m *Measure) WrittenRows() int {
	total := 0
	for _, n := range m.Rows {
		total += n
	}
	return total
}

// RowsPerSecond is what the run wrote per second.
func (m *Measure) RowsPerSecond() float64 {
	if m.Duration <= 0 {
		return 0
	}
	return float64(m.WrittenRows()) / m.Duration.Seconds()
}

// Report is one pass of the perf mode.
type Report struct {
	Date   time.Time `json:"date"`
	Commit string    `json:"commit"`
	Scale  int       `json:"scale"`
	Rounds int       `json:"rounds"`
	// BatchCount is the batch size the destinations were given, zero for the default.
	BatchCount uint32         `json:"batchCount"`
	SourceRows map[string]int `json:"sourceRows"`
	Measures   []*Measure     `json:"measures"`
}

// Write stores report.json and report.md in dir.
func (r *Report) Write(dir string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("perf: %w", err)
	}
	encoded, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("perf: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "report.json"), append(encoded, '\n'), 0o600); err != nil {
		return fmt.Errorf("perf: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "report.md"), []byte(r.Markdown()), 0o600); err != nil {
		return fmt.Errorf("perf: %w", err)
	}
	return nil
}

// measuresOf returns the successful runs of one engine.
func (r *Report) measuresOf(engine env.Engine) []*Measure {
	var kept []*Measure
	for _, m := range r.Measures {
		if m.Engine == engine {
			kept = append(kept, m)
		}
	}
	return kept
}

// Markdown renders the report for people: what each engine took, then what it spent that
// time on.
func (r *Report) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Comparaison mesurée des moteurs\n\n")
	fmt.Fprintf(&b, "- Date : %s\n- Commit : `%s`\n- Échelle : %d (%d lignes en source)\n- Tours par moteur : %d\n",
		r.Date.Format("2006-01-02 15:04:05"), r.Commit, r.Scale, sum(r.SourceRows), r.Rounds)
	if r.BatchCount > 0 {
		fmt.Fprintf(&b, "- Lignes par lot en destination : %d\n", r.BatchCount)
	}
	b.WriteString("\n")
	b.WriteString("Chaque run est seul sur la machine, sur un worker redémarré et une destination vidée.\n\n")

	b.WriteString("## Synthèse\n\n")
	b.WriteString("| Moteur | Durée médiane | min | max | Lignes écrites | Lignes/s (médiane) | Mémoire du run | Runs |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|\n")
	for _, engine := range env.Engines {
		measures := r.measuresOf(engine)
		if len(measures) == 0 {
			continue
		}
		durations := make([]float64, 0, len(measures))
		rates := make([]float64, 0, len(measures))
		var peak int64
		for _, m := range measures {
			durations = append(durations, m.Duration.Seconds())
			rates = append(rates, m.RowsPerSecond())
			if m.AddedMemoryBytes > peak {
				peak = m.AddedMemoryBytes
			}
		}
		sort.Float64s(durations)
		sort.Float64s(rates)
		fmt.Fprintf(&b, "| %s | %.1fs | %.1fs | %.1fs | %d | %.0f | %s | %d |\n",
			engine, median(durations), durations[0], durations[len(durations)-1],
			measures[0].WrittenRows(), median(rates), formatBytes(peak), len(measures))
	}

	b.WriteString("\n## Où passe le temps\n\n")
	for _, engine := range env.Engines {
		measures := r.measuresOf(engine)
		if len(measures) == 0 {
			continue
		}
		fmt.Fprintf(&b, "### %s\n\n| Activité | Nombre | Cumul | La plus longue |\n|---|---|---|---|\n", engine)
		for _, activity := range measures[0].Activities {
			fmt.Fprintf(&b, "| %s | %d | %.1fs | %.1fs |\n",
				activity.Type, activity.Count, activity.Total.Seconds(), activity.Longest.Seconds())
		}
		b.WriteString("\n")
	}

	b.WriteString("## Lignes écrites par table\n\n| Table | Source |")
	for _, engine := range env.Engines {
		fmt.Fprintf(&b, " %s |", engine)
	}
	b.WriteString("\n|---|---|")
	b.WriteString(strings.Repeat("---|", len(env.Engines)))
	b.WriteString("\n")
	for _, table := range sortedKeys(r.SourceRows) {
		fmt.Fprintf(&b, "| `%s` | %d |", table, r.SourceRows[table])
		for _, engine := range env.Engines {
			written := "—"
			if measures := r.measuresOf(engine); len(measures) > 0 {
				written = fmt.Sprintf("%d", measures[0].Rows[table])
			}
			fmt.Fprintf(&b, " %s |", written)
		}
		b.WriteString("\n")
	}

	var failed []string
	for _, m := range r.Measures {
		if m.Status != "JOB_RUN_STATUS_COMPLETE" {
			failed = append(failed, fmt.Sprintf("tour %d, %s : %s %s", m.Round, m.Engine, m.Status, strings.Join(m.Errors, " | ")))
		}
	}
	if len(failed) > 0 {
		b.WriteString("\n## Runs hors attendu\n\n")
		for _, line := range failed {
			fmt.Fprintf(&b, "- %s\n", line)
		}
	}
	return b.String()
}

func median(sorted []float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	middle := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[middle]
	}
	return (sorted[middle-1] + sorted[middle]) / 2
}

func sum(counts map[string]int) int {
	total := 0
	for _, n := range counts {
		total += n
	}
	return total
}

func sortedKeys(counts map[string]int) []string {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func formatBytes(bytes int64) string {
	switch {
	case bytes <= 0:
		return "—"
	case bytes >= 1<<30:
		return fmt.Sprintf("%.1f Gio", float64(bytes)/float64(1<<30))
	default:
		return fmt.Sprintf("%.0f Mio", float64(bytes)/float64(1<<20))
	}
}
