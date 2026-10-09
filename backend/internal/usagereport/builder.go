package usagereport

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	"github.com/fishtre-compagnie/husonym/backend/internal/usagestore"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
)

// ErrNoLicenseInForce says that no report is made: the instance has no license in force.
var ErrNoLicenseInForce = errors.New("no license is in force: the usage report is not prepared")

// optionalReadTimeout bounds a reading the report does without, so that an orchestrator that
// does not answer delays the report and never holds it.
const optionalReadTimeout = 10 * time.Second

// Sealed is a usage report with its seal: the one of a day, as it is kept, or the one for a
// period, as it is handed over.
type Sealed struct {
	// Document is the exact JSON the seal is of, byte for byte.
	Document []byte
	// Seal proves that whoever made the document holds the license key.
	Seal string
	// KeyFingerprint designates the key the document is sealed with, without revealing it.
	KeyFingerprint string
}

// Counters is what the usage store counted on a day, or over the days from one to the day before
// another, and the reports of the day it keeps. *usagestore.Store is one.
type Counters interface {
	InstanceId(ctx context.Context) (string, error)
	RunsOfDay(ctx context.Context, day time.Time) (*usagestore.DayRuns, error)
	SourceVersionsOfDay(ctx context.Context, day time.Time) ([]usagestore.SourceEngineRuns, error)
	RefusalsOfDay(ctx context.Context, day time.Time) ([]usagestore.GateCount, error)
	// ErrorsOfDay counts the runs RunsOfDay counts for the day that did not complete.
	ErrorsOfDay(ctx context.Context, day time.Time) ([]usagestore.ErrorCount, error)

	RunsBetween(ctx context.Context, from, before time.Time) (*usagestore.DayRuns, error)
	RefusalsBetween(ctx context.Context, from, before time.Time) ([]usagestore.GateCount, error)
	ReportsBetween(ctx context.Context, from, before time.Time) ([]usagestore.StoredReport, error)
}

// InventorySource gives what the instance holds. *InventoryReader is one.
type InventorySource interface {
	Read(ctx context.Context, now time.Time) (*Inventory, error)
}

// Instance gives what no store keeps. *InstanceReader is one. The report does without the last
// three: one that fails leaves its field out.
type Instance interface {
	SourcesCount(ctx context.Context) (int, error)
	PostgresMajor(ctx context.Context) (int, error)
	// TemporalVersion is the version as the server spells it: the builder decides what it keeps.
	TemporalVersion(ctx context.Context) (string, error)
	Workers(ctx context.Context) (int, error)
}

// KeySource gives the value of the license key the instance holds, as it was installed, or an
// empty string without one. *licensestore.Store is one.
type KeySource interface {
	Current(ctx context.Context) (string, error)
}

// Builder assembles the usage report of the instance for a day, checks it against its schema
// and seals it.
type Builder struct {
	counters  Counters
	inventory InventorySource
	instance  Instance
	license   license.EEInterface
	key       *InstanceKey
	facts     Facts
}

// NewBuilder takes the license of the process, which says whether a report is made at all, and
// the key of the instance with the ring that verifies it, which everything the report says of
// the license is read from.
func NewBuilder(
	counters Counters,
	inventory InventorySource,
	instance Instance,
	lic license.EEInterface,
	keys KeySource,
	ring license.Keyring,
	facts Facts, //nolint:gocritic // hugeParam: given once, at startup
) *Builder {
	return &Builder{
		counters: counters, inventory: inventory, instance: instance,
		license: lic, key: NewInstanceKey(keys, ring), facts: facts,
	}
}

// Build returns the sealed report of the day, or ErrNoLicenseInForce. day is the UTC day the
// counters are of; now is the moment the report is prepared, which the state of the instance
// and of its license are read at.
//
// A document its schema refuses is an error: nothing is sealed. What the instance cannot tell
// of where it runs is left out and logged, and the report is made without it; but a context
// that ended makes every such reading fail, and that report is not sealed.
func (b *Builder) Build(ctx context.Context, day, now time.Time) (*Sealed, error) {
	// Asked first, and of what the process holds: without a license nothing is read at all.
	if !b.license.IsValid() {
		return nil, ErrNoLicenseInForce
	}
	keyValue, identification, err := b.identify(ctx, now)
	if err != nil {
		return nil, err
	}
	identification.InstanceID, err = b.counters.InstanceId(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to read the id of the instance: %w", err)
	}
	sources, err := b.instance.SourcesCount(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to count the sources of the instance: %w", err)
	}

	report := &telemetry.Report{
		SchemaVersion:  telemetry.SchemaVersion,
		Day:            day.UTC().Format(time.DateOnly),
		GeneratedAt:    now.UTC().Format(time.RFC3339),
		Identification: *identification,
		Version:        telemetry.Version{Husonym: telemetry.HusonymVersion(b.facts.Version)},
		Sources:        telemetry.Sources{Count: sources},
	}
	if b.facts.Diagnostics {
		report.Diagnostics, err = b.diagnostics(ctx, day, now)
		if err != nil {
			return nil, err
		}
	}

	// The optional readings are skipped when they fail, and they all fail once the context is
	// done: what was assembled then is not the report that was asked for.
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("the usage report was not made in time: %w", err)
	}

	document, err := report.Marshal()
	if err != nil {
		return nil, fmt.Errorf("unable to write the usage report: %w", err)
	}
	if err := telemetry.Validate(document); err != nil {
		return nil, err
	}
	seal, err := telemetry.Seal(keyValue, document)
	if err != nil {
		return nil, err
	}
	return &Sealed{Document: document, Seal: seal, KeyFingerprint: identification.KeyFingerprint}, nil
}

// identify reads the key of the instance and says what the report tells of it. The fingerprint,
// the id, the state and the days left all come from that one value, which is the one the report
// is then sealed with: they never tell of two keys. Nothing here cites the key.
func (b *Builder) identify(ctx context.Context, now time.Time) (string, *telemetry.Identification, error) {
	keyValue, key, err := b.key.inForce(ctx, now)
	if err != nil {
		return "", nil, err
	}
	return keyValue, &telemetry.Identification{
		KeyFingerprint: telemetry.KeyFingerprint(keyValue),
		LicenseID:      telemetry.LicenseId(key.Id),
		LicenseState:   telemetry.LicenseState(string(key.StateAt(now))),
		DaysToExpiry:   daysBetween(now, key.ExpiresAt),
	}, nil
}

// daysBetween is how many whole days lie between two moments, counted down: a day and a half
// is one day, and it is negative as soon as the second moment is past.
func daysBetween(from, to time.Time) int {
	return int(math.Floor(to.Sub(from).Hours() / 24))
}

// diagnostics is the part of the report the operator can switch off.
func (b *Builder) diagnostics(ctx context.Context, day, now time.Time) (*telemetry.Diagnostics, error) {
	inventory, err := b.inventory.Read(ctx, now)
	if err != nil {
		return nil, fmt.Errorf("unable to read what the instance holds: %w", err)
	}
	runs, err := b.counters.RunsOfDay(ctx, day)
	if err != nil {
		return nil, fmt.Errorf("unable to read the runs of the day: %w", err)
	}
	versions, err := b.counters.SourceVersionsOfDay(ctx, day)
	if err != nil {
		return nil, fmt.Errorf("unable to read the source versions of the day: %w", err)
	}
	refusals, err := b.counters.RefusalsOfDay(ctx, day)
	if err != nil {
		return nil, fmt.Errorf("unable to read the refusals of the day: %w", err)
	}
	// Read as the runs are: the errors are the ones of the runs the same report counts, and a
	// report that could not read them is not made, as for any other count of the day.
	failures, err := b.counters.ErrorsOfDay(ctx, day)
	if err != nil {
		return nil, fmt.Errorf("unable to read the errors of the day: %w", err)
	}

	configuration := telemetry.Configuration{
		AuthEnabled:          b.facts.AuthEnabled,
		AccountOIDCProviders: int(inventory.AccountOidcProviders),
		Presidio:             b.facts.Presidio,
		RunLogs:              telemetry.RunLogs(b.facts.RunLogs),
	}
	if b.facts.AuthEnabled {
		configuration.AuthProvider = telemetry.AuthProvider(b.facts.AuthProvider)
	}

	logger := logger_interceptor.GetLoggerFromContextOrDefault(ctx)
	engines, unshaped := sourceEngines(versions, inventory.SourceTypeOfJob)
	if unshaped {
		logger.WarnContext(ctx, "runs on a source whose version is not one are left out of the usage report")
	}
	refused, unknown := refusalCounts(refusals)
	if unknown {
		logger.WarnContext(ctx, "refusals of a gate the usage report does not know are left out of it")
	}

	return &telemetry.Diagnostics{
		Installation:  b.installation(ctx),
		Configuration: configuration,
		Connections:   inventory.Connections,
		SourceEngines: engines,
		Jobs:          inventory.Jobs,
		Transformers:  inventory.Transformers,
		ColumnTypes:   inventory.ColumnTypes,
		Features:      inventory.Features,
		Refusals:      refused,
		Runs:          runsOf(runs),
		Errors:        ErrorCounts(failures),
		Users:         inventory.Users,
		Unread: telemetry.Unread{
			Jobs:        int(inventory.Unread.Jobs),
			Connections: int(inventory.Unread.Connections),
			Accounts:    int(inventory.Unread.Accounts),
		},
	}, nil
}

// installation says where the instance runs. What cannot be read is left out and logged.
func (b *Builder) installation(ctx context.Context) telemetry.Installation {
	installation := telemetry.Installation{
		Kind: telemetry.InstallKind(b.facts.InstallKind),
		OS:   telemetry.OperatingSystem(b.facts.OS),
		Arch: telemetry.Architecture(b.facts.Arch),
	}
	if major, ok := optional(ctx, "the version of the database", b.instance.PostgresMajor); ok {
		installation.PostgresMajor = &major
	}
	if version, ok := optional(ctx, "the version of the orchestrator", b.instance.TemporalVersion); ok {
		installation.TemporalVersion = telemetry.TemporalVersion(version)
		// What the server said is not logged: it is not the report's to quote.
		if installation.TemporalVersion == "" {
			logger_interceptor.GetLoggerFromContextOrDefault(ctx).WarnContext(
				ctx, "a version of the orchestrator that is not one is left out of the usage report",
			)
		}
	}
	if workers, ok := optional(ctx, "the number of workers", b.instance.Workers); ok {
		installation.Workers = &workers
	}
	return installation
}

// optional makes a reading the report does without, within its own time. One that fails is
// logged, in the log of the instance, and tells the caller to leave the field out.
func optional[T any](ctx context.Context, what string, read func(context.Context) (T, error)) (T, bool) {
	bounded, cancel := context.WithTimeout(ctx, optionalReadTimeout)
	defer cancel()
	value, err := read(bounded)
	if err != nil {
		logger_interceptor.GetLoggerFromContextOrDefault(ctx).WarnContext(
			ctx, "the usage report is prepared without "+what+", which could not be read", "error", err,
		)
		return value, false
	}
	return value, true
}

// sourceEngines adds up the runs of the day per type and major version of their source. A job
// that is gone since its run, or whose source is not known, counts under the type other; a
// version that is not one or two numbers is not counted, and unshaped tells there was one.
func sourceEngines(
	versions []usagestore.SourceEngineRuns,
	typeOfJob map[string]string,
) (engines []telemetry.SourceEngine, unshaped bool) {
	type engine struct{ kind, major string }
	runs := make(map[engine]int)
	for _, version := range versions {
		major := telemetry.SourceMajor(version.VersionMajor)
		if major == "" {
			unshaped = true
			continue
		}
		// A job the map does not hold gives the empty type, which is other.
		runs[engine{telemetry.ConnectionType(typeOfJob[version.JobId]), major}] += int(version.Runs)
	}
	engines = make([]telemetry.SourceEngine, 0, len(runs))
	for key, count := range runs {
		engines = append(engines, telemetry.SourceEngine{Type: key.kind, Major: key.major, Runs: count})
	}
	return engines, unshaped
}

// refusalCounts keeps the refusals of the gates the report knows, and unknown tells there was
// one of another gate.
func refusalCounts(refusals []usagestore.GateCount) (counts []telemetry.GateCount, unknown bool) {
	gates := telemetry.Gates()
	counts = make([]telemetry.GateCount, 0, len(refusals))
	for _, refusal := range refusals {
		gate := string(refusal.Gate)
		if !slices.Contains(gates, gate) {
			unknown = true
			continue
		}
		counts = append(counts, telemetry.GateCount{Gate: gate, Count: int(refusal.Count)})
	}
	return counts, unknown
}

// ErrorCounts turns the errors the store counted into the rows of the report, never nil. A
// category or a step the report does not know is other, and rows that become the same are added
// up: nothing outside the two lists leaves, whatever a row holds.
func ErrorCounts(rows []usagestore.ErrorCount) []telemetry.ErrorCount {
	type place struct{ category, step string }
	counted := make(map[place]int, len(rows))
	for _, row := range rows {
		counted[place{telemetry.ErrorCategory(string(row.Category)), telemetry.ErrorStep(string(row.Step))}] += int(row.Count)
	}
	counts := make([]telemetry.ErrorCount, 0, len(counted))
	for key, count := range counted {
		counts = append(counts, telemetry.ErrorCount{Category: key.category, Step: key.step, Count: count})
	}
	return counts
}

// runsOf turns the runs of the day into their block. Kinds and statuses the report does not
// know are counted together, as other; rows leave as bands.
func runsOf(day *usagestore.DayRuns) telemetry.Runs {
	type outcome struct{ kind, status string }
	counted := make(map[outcome]int)
	for _, run := range day.ByStatus {
		counted[outcome{telemetry.JobKind(string(run.Kind)), telemetry.RunStatus(string(run.Status))}] += int(run.Count)
	}
	runs := telemetry.Runs{
		ByStatus:          make([]telemetry.RunCount, 0, len(counted)),
		RowsRead:          telemetry.RowsBucket(day.RowsRead),
		RowsDiscarded:     telemetry.RowsBucket(day.RowsDiscarded),
		Retries:           int(day.Retries),
		WithUncountedRows: int(day.WithUncountedRows),
	}
	for key, count := range counted {
		runs.ByStatus = append(runs.ByStatus, telemetry.RunCount{Kind: key.kind, Status: key.status, Count: count})
	}
	if day.DurationMedian != nil && day.DurationP95 != nil {
		runs.DurationSeconds = &telemetry.Durations{Median: *day.DurationMedian, P95: *day.DurationP95}
	}
	return runs
}
