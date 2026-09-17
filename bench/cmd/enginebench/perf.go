package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/fishtre-compagnie/husonym/bench/perf"
)

// perfMode runs the measured comparison: one dataset at scale, each engine in turn, alone
// on the machine. It is the only mode whose durations mean anything — the correctness mode
// runs its cases side by side, and measures the contention as much as the engines.
func perfMode(args []string) error {
	flags := flag.NewFlagSet("enginebench perf", flag.ExitOnError)
	scale := flags.Int("scale", perf.DefaultScale, "multiplies the rows of the dataset (100 ≈ 3 million rows)")
	rounds := flags.Int("rounds", 5, "how many times each engine runs")
	outDir := flags.String("out", "bench/out", "directory receiving the reports")
	runTimeout := flags.Duration("run-timeout", 30*time.Minute, "time given to one run before it is terminated")
	skipLoad := flags.Bool("skip-load", false, "reuse the source loaded by a previous pass")
	if err := flags.Parse(args); err != nil {
		return err
	}

	ctx := context.Background()
	b, err := newBench(ctx, true)
	if err != nil {
		return err
	}

	runner := &perf.Runner{
		Env:        b.env,
		Renderer:   b.renderer,
		Source:     b.source,
		Dests:      b.dests,
		Client:     b.client,
		SourceConn: b.sourceConn,
		DestConns:  b.destConns,
		Scale:      *scale,
		Rounds:     *rounds,
		RunTimeout: *runTimeout,
		SkipLoad:   *skipLoad,
		Log:        func(format string, args ...any) { fmt.Fprintf(os.Stdout, format+"\n", args...) },
	}
	report, err := runner.Run(ctx)
	if err != nil {
		return err
	}
	report.Commit = gitCommit(ctx)
	dir := filepath.Join(*outDir, "perf-"+report.Date.Format("20060102-150405")+"-"+report.Commit)
	if err := report.Write(dir); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "\nrapport : %s\n", filepath.Join(dir, "report.md"))
	return nil
}
