// Command enginebench runs the engine test bench: it loads the tricky cases into the
// frozen source, syncs each of them with Benthos and with Athanor through the Husonym
// API, checks every destination against the expectation of the case and reports the gaps.
//
//	enginebench list                         the cases
//	enginebench run [-cases a,b] [-parallel n] [-update-baseline]
//	enginebench verify [-cases a,b]          check the destinations again, without running
//	enginebench perf [-scale n] [-rounds n]  measure both engines on a dataset at scale
package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/fishtre-compagnie/husonym/bench/cases"
	"github.com/fishtre-compagnie/husonym/bench/env"
	"github.com/fishtre-compagnie/husonym/bench/gen"
	"github.com/fishtre-compagnie/husonym/bench/orchestrate"
	"github.com/fishtre-compagnie/husonym/bench/report"
	"github.com/fishtre-compagnie/husonym/bench/schema"
	"github.com/fishtre-compagnie/husonym/bench/verify"
	"github.com/fishtre-compagnie/husonym/bench/workerctl"
)

var errRegressions = errors.New("cases did worse than the baseline")

const (
	// idleConnections is the default of database/sql, restored after the idle pool is emptied.
	idleConnections = 2
	// interruptedActivityGrace is what the activity of a terminated run is left to end in.
	interruptedActivityGrace = 3 * time.Second
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: enginebench list | run | verify | perf")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "list":
		err = list()
	case "run":
		err = run(os.Args[2:], true)
	case "verify":
		err = run(os.Args[2:], false)
	case "perf":
		err = perfMode(os.Args[2:])
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "enginebench:", err)
		os.Exit(1)
	}
}

func list() error {
	for _, c := range cases.All() {
		if err := c.Validate(); err != nil {
			return err
		}
		databases := "tous"
		if len(c.Dialects) > 0 {
			names := make([]string, len(c.Dialects))
			for i, d := range c.Dialects {
				names[i] = string(d)
			}
			databases = strings.Join(names, ",")
		}
		fmt.Fprintf(os.Stdout, "%s  %-10s %-32s %s\n", c.Priority, databases, c.ID, c.Title)
	}
	return nil
}

// bench is what every case needs to run.
type bench struct {
	env      *env.Env
	renderer schema.Renderer
	source   *sql.DB
	dests    map[env.Engine]*sql.DB
	// Set when jobs are run, nil when destinations are only verified again.
	client      *orchestrate.Client
	sourceConn  string
	destConns   map[env.Engine]string
	runTag      string
	runTimeout  time.Duration
	progressMux sync.Mutex
}

func run(args []string, execute bool) error {
	flags := flag.NewFlagSet("enginebench", flag.ExitOnError)
	only := flags.String("cases", "", "comma-separated case ids (default: every case)")
	parallel := flags.Int("parallel", 4, "cases run at the same time")
	outDir := flags.String("out", "bench/out", "directory receiving the reports")
	baselinePath := flags.String("baseline", "bench/baseline.json", "known gaps")
	runTimeout := flags.Duration("run-timeout", 3*time.Minute, "time given to one run before it is terminated")
	updateBaseline := flags.Bool("update-baseline", false, "record the verdicts of this run as the known gaps")
	if err := flags.Parse(args); err != nil {
		return err
	}
	ctx := context.Background()
	b, err := newBench(ctx, execute)
	if err != nil {
		return err
	}
	b.runTimeout = *runTimeout
	selected, err := selectCases(*only, b.env.Dialect)
	if err != nil {
		return err
	}
	if left := len(cases.All()) - len(selected); left > 0 && *only == "" {
		fmt.Fprintf(os.Stdout, "%d cas hors de %s\n\n", left, b.env.Dialect)
	}

	started := time.Now()
	// Sources are loaded one after the other: cases share the oracle tables, and
	// concurrent replacements of their expectations deadlock.
	sourceRows := make([]map[string]int, len(selected))
	if execute {
		if err := b.setSourceReadOnly(ctx, false); err != nil {
			return err
		}
		for i, c := range selected {
			if sourceRows[i], err = gen.LoadSource(ctx, b.source, b.renderer, c, b.env.Params); err != nil {
				return err
			}
		}
		// Every run reads a source that refuses writes, like a read-only replica: an engine
		// that needs to write to its source fails here, not at a customer's.
		if err := b.setSourceReadOnly(ctx, true); err != nil {
			return err
		}
		defer func() { _ = b.setSourceReadOnly(ctx, false) }()
	}
	reports := make([]*report.CaseReport, len(selected))
	errs := make([]error, len(selected))
	slots := make(chan struct{}, max(1, *parallel))
	var wg sync.WaitGroup
	runOne := func(i int, c *cases.Case) {
		reports[i], errs[i] = b.runCase(ctx, c, execute)
		if reports[i] != nil {
			reports[i].SourceRows = sourceRows[i]
		}
	}
	for i, c := range selected {
		if c.KillWorker != nil {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			runOne(i, c)
		}()
	}
	wg.Wait()
	// A case killing the worker runs alone: the runs of the others would go down with it.
	for i, c := range selected {
		if c.KillWorker != nil {
			runOne(i, c)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}

	commit := gitCommit(ctx)
	r := &report.Report{
		Date: started, Commit: commit, Dialect: b.env.Dialect, Params: b.env.Params, Cases: reports,
	}
	dir := filepath.Join(*outDir, started.Format("20060102-150405")+"-"+commit)
	if err := r.Write(dir); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "\nrapport : %s\n", filepath.Join(dir, "report.md"))

	baseline, err := report.ReadBaseline(*baselinePath)
	if err != nil {
		return err
	}
	if *updateBaseline {
		baseline.Merge(r)
		return baseline.Write(*baselinePath)
	}
	if regressions := baseline.Regressions(r); len(regressions) > 0 {
		fmt.Fprintln(os.Stdout, "\nhors baseline :")
		for _, line := range regressions {
			fmt.Fprintln(os.Stdout, "  -", line)
		}
		return errRegressions
	}
	return nil
}

// selectCases returns the cases to run on a database: the named ones, or every case the
// database is concerned by. A case named on the command line that belongs to another
// database is an error, not a silent skip.
func selectCases(only string, dialect schema.Dialect) ([]*cases.Case, error) {
	all := cases.All()
	if only == "" {
		var selected []*cases.Case
		for _, c := range all {
			if c.RunsOn(dialect) {
				selected = append(selected, c)
			}
		}
		return selected, nil
	}
	byID := map[string]*cases.Case{}
	for _, c := range all {
		byID[c.ID] = c
	}
	var selected []*cases.Case
	for _, id := range strings.Split(only, ",") {
		c, ok := byID[strings.TrimSpace(id)]
		if !ok {
			return nil, fmt.Errorf("unknown case %q", id)
		}
		if !c.RunsOn(dialect) {
			return nil, fmt.Errorf("case %q is not exercised on %s", c.ID, dialect)
		}
		selected = append(selected, c)
	}
	return selected, nil
}

func newBench(ctx context.Context, execute bool) (*bench, error) {
	environment, err := env.FromEnvironment()
	if err != nil {
		return nil, err
	}
	renderer, err := schema.RendererFor(environment.Dialect)
	if err != nil {
		return nil, err
	}
	b := &bench{
		env:       environment,
		renderer:  renderer,
		dests:     map[env.Engine]*sql.DB{},
		destConns: map[env.Engine]string{},
		runTag:    time.Now().Format("20060102-150405"),
	}
	if b.source, err = openServer(ctx, "source", &environment.Source); err != nil {
		return nil, err
	}
	for _, engine := range env.Engines {
		server := environment.Destinations[engine]
		if b.dests[engine], err = openServer(ctx, "destination "+string(engine), &server); err != nil {
			return nil, err
		}
	}
	if !execute {
		return b, nil
	}
	if b.client, err = orchestrate.NewClient(ctx, environment.APIURL); err != nil {
		return nil, err
	}
	if b.sourceConn, err = b.client.EnsureConnection(ctx, "source", &environment.Source); err != nil {
		return nil, err
	}
	for _, engine := range env.Engines {
		server := environment.Destinations[engine]
		if b.destConns[engine], err = b.client.EnsureConnection(ctx, "dest-"+string(engine), &server); err != nil {
			return nil, err
		}
	}
	return b, nil
}

// setSourceReadOnly turns the source into a server refusing the writes of the runs, or
// back: every run then reads a source it cannot write to, like a replica.
func (b *bench) setSourceReadOnly(ctx context.Context, readOnly bool) error {
	// One connection for the whole switch: a statement of it may be what lets the next
	// one through, and the pool would hand them to different sessions.
	conn, err := b.source.Conn(ctx)
	if err != nil {
		return fmt.Errorf("source read-only switch: %w", err)
	}
	defer conn.Close()
	for _, stmt := range b.renderer.ReadOnlyStatements(b.env.Source.Database, readOnly) {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("source read-only switch: %w\n%s", err, stmt)
		}
	}
	// A session reads the setting when it opens, so the ones idle in the pool still hold
	// the old one. Emptying the idle pool leaves only sessions opened after the switch.
	b.source.SetMaxIdleConns(0)
	b.source.SetMaxIdleConns(idleConnections)
	return nil
}

// restrictedDestination creates, on the destination of an engine, the account holding
// only the privileges the case grants, and returns the connection the job writes with.
func (b *bench) restrictedDestination(ctx context.Context, c *cases.Case, engine env.Engine) (string, error) {
	// One account per case: cases run in parallel on the same destination server.
	digest := sha256.Sum256([]byte(c.ID))
	user, password := "bench_r_"+hex.EncodeToString(digest[:4]), "bench-restricted"
	db := b.dests[engine]
	quotedDB := b.renderer.QuoteIdent(c.Schema())
	stmts := b.renderer.AccountStatements(user, password)
	for _, grant := range c.DestinationGrants[b.env.Dialect] {
		grant = strings.ReplaceAll(grant, "{db}", quotedDB)
		stmts = append(stmts, strings.ReplaceAll(grant, "{user}", b.renderer.Account(user)))
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return "", fmt.Errorf("%s: restricted destination account: %w\n%s", c.ID, err, stmt)
		}
	}
	server := b.env.Destinations[engine]
	server.User, server.Password = user, password
	server.Database = b.renderer.ConnectionDatabase(server.Database, c.Schema())
	// The connection name carries a digest of the account, which tells the cases apart.
	return b.client.EnsureConnection(ctx, "dest-"+string(engine)+"-restricted", &server)
}

func openServer(ctx context.Context, role string, server *env.Server) (*sql.DB, error) {
	db, err := server.Open()
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("%s (%s) unreachable, is bench/compose.bench.yml up? %w", role, server.Addr, err)
	}
	return db, nil
}

// runCase syncs one loaded case with each engine in turn, then verifies every
// destination. With execute false it only verifies what a previous run left.
func (b *bench) runCase(ctx context.Context, c *cases.Case, execute bool) (*report.CaseReport, error) {
	caseReport := &report.CaseReport{ID: c.ID, Priority: c.Priority.String(), Title: c.Title}
	for _, engine := range env.Engines {
		outcome := &report.Outcome{Engine: engine}
		if execute {
			if err := b.execute(ctx, c, engine, outcome); err != nil {
				return nil, err
			}
		} else {
			outcome.RunStatus = "non relancé"
		}
		if outcome.Verdict == "" {
			verification, err := verify.Case(ctx, b.source, b.dests[engine], b.renderer, c)
			if err != nil {
				return nil, err
			}
			outcome.Verification = verification
			outcome.Gaps = verification.Gaps()
			outcome.Verdict = report.VerdictOK
			if outcome.Gaps > 0 {
				outcome.Verdict = report.VerdictGap
			}
		}
		// A run failing as expected must leave the triggers as they were all the same.
		if len(outcome.TriggerChanges) > 0 {
			outcome.Gaps += len(outcome.TriggerChanges)
			if outcome.Verdict == report.VerdictOK {
				outcome.Verdict = report.VerdictGap
			}
		}
		caseReport.Outcomes = append(caseReport.Outcomes, outcome)
		b.progress(c, outcome)
	}
	return caseReport, nil
}

// execute runs the case on one engine and settles the verdict when the run alone decides
// it; it leaves the verdict empty when the destination must be verified.
func (b *bench) execute(ctx context.Context, c *cases.Case, engine env.Engine, outcome *report.Outcome) error {
	if err := gen.PrepareDestination(ctx, b.dests[engine], b.renderer, c); err != nil {
		return err
	}
	triggersBefore, err := verify.Triggers(ctx, b.dests[engine], b.renderer, c.Schema())
	if err != nil {
		return err
	}
	destConn := b.destConns[engine]
	if len(c.DestinationGrants[b.env.Dialect]) > 0 {
		if destConn, err = b.restrictedDestination(ctx, c, engine); err != nil {
			return err
		}
	}
	jobID, err := b.client.CreateJob(ctx, b.env.Dialect, c, engine, b.sourceConn, destConn, b.runTag)
	if err != nil {
		return err
	}
	if c.InterruptedRunFirst {
		first, err := b.client.RunUntil(ctx, jobID, b.runTimeout, func(ctx context.Context) (bool, error) {
			now, err := verify.Triggers(ctx, b.dests[engine], b.renderer, c.Schema())
			return len(verify.TriggerChanges(triggersBefore, now)) > 0, err
		})
		if err != nil {
			return err
		}
		if !first.Interrupted {
			outcome.Verdict = report.VerdictNotExercised
			outcome.RunStatus = "le premier run a fini sans être surpris triggers retirés"
			return nil
		}
		// Terminating a run does not stop the activity it was running: it goes on until
		// it ends, on its own. Its page is small and written in well under this.
		time.Sleep(interruptedActivityGrace)
	}
	var watch func(context.Context) (bool, error)
	killed := false
	if c.KillWorker != nil {
		blocker, err := b.blockRow(ctx, c, engine)
		if err != nil {
			return err
		}
		defer func() { _ = blocker.Rollback() }()
		watch = func(ctx context.Context) (bool, error) {
			if killed {
				return false, nil
			}
			var waiting int
			if err := b.dests[engine].QueryRowContext(ctx, b.renderer.LockWaitQuery()).Scan(&waiting); err != nil {
				return false, err
			}
			if waiting == 0 {
				return false, nil
			}
			killed = true
			if err := workerctl.Kill(ctx); err != nil {
				return false, err
			}
			return false, blocker.Rollback()
		}
	}
	result, err := b.client.RunUntil(ctx, jobID, b.runTimeout, watch)
	if err != nil {
		return err
	}
	if c.KillWorker != nil && !killed {
		outcome.Verdict = report.VerdictNotExercised
		outcome.RunStatus = "le run n'a jamais attendu la ligne de blocage"
		return nil
	}
	if result.Succeeded() && !result.TimedOut {
		planned := slices.IndexFunc(c.Tables, func(t *schema.Table) bool { return !c.IsExcluded(t.Name) })
		pageLimit, err := b.client.PlanPageLimit(ctx, result.RunID, c.Schema(), c.Tables[planned].Name)
		if err != nil {
			return err
		}
		if pageLimit != b.env.Params.PageLimit {
			return fmt.Errorf("the worker pages every %d rows while the bench expects %d: "+
				"start it with `make bench/up`, or set BENCH_PAGE_LIMIT", pageLimit, b.env.Params.PageLimit)
		}
	}
	triggersAfter, err := verify.Triggers(ctx, b.dests[engine], b.renderer, c.Schema())
	if err != nil {
		return err
	}
	outcome.TriggerChanges = verify.TriggerChanges(triggersBefore, triggersAfter)
	outcome.RunStatus = strings.TrimPrefix(result.Status.String(), "JOB_RUN_STATUS_")
	outcome.DurationMs = result.Duration.Milliseconds()
	outcome.Errors = result.Errors

	switch {
	case result.TimedOut:
		outcome.Verdict = report.VerdictRunTimeout
	case c.ExpectedRunError(b.env.Dialect, string(engine)) != "":
		outcome.Verdict = report.VerdictFailureExpected
		if !result.Succeeded() && strings.Contains(strings.Join(result.Errors, "\n"), c.ExpectedRunError(b.env.Dialect, string(engine))) {
			outcome.Verdict = report.VerdictOK
		}
	case !result.Succeeded():
		outcome.Verdict = report.VerdictRunFailed
	}
	return nil
}

// blockRow inserts the blocking row of a case into the destination of an engine, in a
// transaction left open for the caller to roll back.
func (b *bench) blockRow(ctx context.Context, c *cases.Case, engine env.Engine) (*sql.Tx, error) {
	t := c.Table(c.KillWorker.Table)
	if t == nil {
		return nil, fmt.Errorf("%s: blocking row for unknown table %q", c.ID, c.KillWorker.Table)
	}
	columns, marks := make([]string, len(t.Columns)), make([]string, len(t.Columns))
	for i := range t.Columns {
		columns[i] = b.renderer.QuoteIdent(t.Columns[i].Name)
		marks[i] = b.renderer.Placeholder(i + 1)
	}
	tx, err := b.dests[engine].BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: blocking row: %w", c.ID, err)
	}
	//nolint:gosec // identifiers come from the case definitions and are quoted
	query := fmt.Sprintf("INSERT INTO %s.%s (%s)%s VALUES (%s)",
		b.renderer.QuoteIdent(c.Schema()), b.renderer.QuoteIdent(t.Name), strings.Join(columns, ", "),
		b.renderer.InsertOverride(), strings.Join(marks, ", "))
	if _, err := tx.ExecContext(ctx, query, c.KillWorker.BlockingRow...); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("%s: blocking row: %w", c.ID, err)
	}
	return tx, nil
}

func (b *bench) progress(c *cases.Case, o *report.Outcome) {
	b.progressMux.Lock()
	defer b.progressMux.Unlock()
	fmt.Fprintf(os.Stdout, "%s  %-32s %-8s %-22s écarts=%d  %d ms\n",
		c.Priority, c.ID, o.Engine, o.Verdict, o.Gaps, o.DurationMs)
}

// gitCommit names the code under test, marked when the work tree has uncommitted changes.
func gitCommit(ctx context.Context) string {
	out, err := exec.CommandContext(ctx, "git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	commit := strings.TrimSpace(string(out))
	status, err := exec.CommandContext(ctx, "git", "status", "--porcelain").Output()
	if err == nil && len(status) > 0 {
		commit += "-dirty"
	}
	return commit
}
