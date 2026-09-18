package perf

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/fishtre-compagnie/husonym/bench/cases"
	"github.com/fishtre-compagnie/husonym/bench/env"
	"github.com/fishtre-compagnie/husonym/bench/gen"
	"github.com/fishtre-compagnie/husonym/bench/orchestrate"
	"github.com/fishtre-compagnie/husonym/bench/schema"
	"github.com/fishtre-compagnie/husonym/bench/workerctl"
)

// Runner measures the two engines on the dataset.
type Runner struct {
	Env      *env.Env
	Renderer schema.Renderer
	Source   *sql.DB
	Dests    map[env.Engine]*sql.DB
	Client   *orchestrate.Client
	// SourceConn and DestConns are the connections of the job.
	SourceConn string
	DestConns  map[env.Engine]string
	// Scale multiplies the rows of the dataset, Rounds is how many times each engine runs.
	Scale, Rounds int
	RunTimeout    time.Duration
	// SkipLoad reuses the source of a previous pass instead of loading it again.
	SkipLoad bool
	// BatchCount is how many rows a destination writes at once; zero keeps the default.
	BatchCount uint32
	Log        func(format string, args ...any)
}

// Run loads the source, then runs the dataset with each engine in turn, as many rounds as
// asked. Engines alternate so that a machine slowly warming up or cooling down does not
// favor the one that always runs first.
func (r *Runner) Run(ctx context.Context) (*Report, error) {
	dataset := Dataset()
	dataset.Job.BatchCount = r.BatchCount
	report := &Report{
		Date:       time.Now(),
		Scale:      r.Scale,
		Rounds:     r.Rounds,
		BatchCount: r.BatchCount,
	}

	if !r.SkipLoad {
		r.logf("chargement de la source : %d lignes", TotalRows(r.Scale))
		started := time.Now()
		counts, err := Load(ctx, r.Source, r.Renderer, dataset, r.Scale)
		if err != nil {
			return nil, err
		}
		report.SourceRows = counts
		r.logf("source chargée en %s", time.Since(started).Round(time.Second))
	} else {
		counts, err := r.tableCounts(ctx, r.Source, dataset)
		if err != nil {
			return nil, err
		}
		report.SourceRows = counts
	}

	jobs := map[env.Engine]string{}
	tag := time.Now().Format("20060102-150405")
	for _, engine := range env.Engines {
		id, err := r.Client.CreateJob(ctx, r.Renderer.Dialect(), dataset, engine, r.SourceConn, r.DestConns[engine], tag)
		if err != nil {
			return nil, err
		}
		jobs[engine] = id
	}

	for round := 1; round <= r.Rounds; round++ {
		engines := append([]env.Engine{}, env.Engines...)
		if round%2 == 0 {
			engines[0], engines[1] = engines[1], engines[0]
		}
		for _, engine := range engines {
			measure, err := r.measure(ctx, dataset, engine, jobs[engine], round)
			if err != nil {
				return nil, err
			}
			report.Measures = append(report.Measures, measure)
			r.logf("tour %d  %-8s %6.1fs  %8.0f lignes/s  mémoire du run %s",
				round, engine, measure.Duration.Seconds(), measure.RowsPerSecond(), formatBytes(measure.AddedMemoryBytes))
		}
	}
	return report, nil
}

// measure runs the dataset once with one engine, on a destination emptied beforehand and a
// worker just restarted.
func (r *Runner) measure(
	ctx context.Context,
	dataset *cases.Case,
	engine env.Engine,
	jobID string,
	round int,
) (*Measure, error) {
	if err := gen.PrepareDestination(ctx, r.Dests[engine], r.Renderer, dataset); err != nil {
		return nil, err
	}
	// A run starts on a worker that has just been restarted: a pool of connections already
	// open, or a heap already grown, would measure the run before rather than this one.
	if err := workerctl.Restart(ctx); err != nil {
		return nil, err
	}

	memory := WatchMemory(ctx)
	result, err := r.Client.Run(ctx, jobID, r.RunTimeout)
	peak, added := memory.Stop()
	if err != nil {
		return nil, err
	}
	measure := &Measure{
		Round:    round,
		Engine:   engine,
		Status:   result.Status.String(),
		Duration: result.Duration,
		Errors:   result.Errors,
	}
	if result.TimedOut {
		measure.Duration = r.RunTimeout
	}
	measure.PeakMemoryBytes, measure.AddedMemoryBytes = peak, added

	activities, err := r.Client.Activities(ctx, result.RunID)
	if err != nil {
		return nil, err
	}
	measure.Activities = summarize(activities)

	if measure.Rows, err = r.tableCounts(ctx, r.Dests[engine], dataset); err != nil {
		return nil, err
	}
	if pageLimit, err := r.Client.PlanPageLimit(ctx, result.RunID, dataset.Schema(), CommandeTable); err == nil {
		measure.PageLimit = pageLimit
	}
	return measure, nil
}

// tableCounts counts the rows of every table of the dataset in one database.
func (r *Runner) tableCounts(ctx context.Context, db *sql.DB, dataset *cases.Case) (map[string]int, error) {
	counts := map[string]int{}
	for _, t := range dataset.Tables {
		//nolint:gosec // identifiers come from the dataset and are quoted
		query := fmt.Sprintf("SELECT COUNT(*) FROM %s.%s",
			r.Renderer.QuoteIdent(dataset.Schema()), r.Renderer.QuoteIdent(t.Name))
		var n int
		if err := db.QueryRowContext(ctx, query).Scan(&n); err != nil {
			return nil, fmt.Errorf("perf: count of %s: %w", t.Name, err)
		}
		counts[t.Name] = n
	}
	return counts, nil
}

// summarize groups the activities of a run by type: how many, and how long together.
func summarize(activities []orchestrate.Activity) []ActivitySummary {
	order := []string{}
	byType := map[string]*ActivitySummary{}
	for _, activity := range activities {
		summary, ok := byType[activity.Type]
		if !ok {
			summary = &ActivitySummary{Type: activity.Type}
			byType[activity.Type] = summary
			order = append(order, activity.Type)
		}
		summary.Count++
		summary.Total += activity.Duration
		if activity.Duration > summary.Longest {
			summary.Longest = activity.Duration
		}
	}
	summaries := make([]ActivitySummary, 0, len(order))
	for _, name := range order {
		summaries = append(summaries, *byType[name])
	}
	return summaries
}

func (r *Runner) logf(format string, args ...any) {
	if r.Log != nil {
		r.Log(format, args...)
	}
}
