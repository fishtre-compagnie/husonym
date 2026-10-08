package cpstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	cpdb "github.com/fishtre-compagnie/husonym/controlplane/gen/db"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// What the operator console reads. Nothing here writes, and nothing here reads the encoded key
// of a license: no type of this file can carry it. What the console writes is in operator.go.

// ErrNotFound is returned when the store holds nothing under what was asked for.
var ErrNotFound = errors.New("not found")

// CustomerSummary is a customer as a list shows it.
type CustomerSummary struct {
	ID         uuid.UUID
	ExternalID string
	Name       string
	Licenses   int
	// RecentInstances is how many instances were seen under its licenses in the last
	// RecentInstanceDays days.
	RecentInstances int
	// NearestExpiry is the next expiry to come among its licenses; when all of them have expired,
	// the last one. Zero for a customer without a license.
	NearestExpiry time.Time
}

// CustomerDetail is a customer with its licenses and the instances seen under them.
type CustomerDetail struct {
	ID         uuid.UUID
	ExternalID string
	Name       string
	Note       string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	// Licenses come with the latest expiry first.
	Licenses []LicenseSummary
	// Instances come with the one heard of last first.
	Instances []InstanceSummary
}

// LicenseSummary is a license as a list shows it.
type LicenseSummary struct {
	ID           string
	CustomerID   uuid.UUID
	CustomerName string
	Plan         string
	Telemetry    license.TelemetryMode
	ExpiresAt    time.Time
	// State is the state at the instant the summary was asked for.
	State license.State
	// HasSuccessor tells whether another license says it succeeds this one.
	HasSuccessor bool
}

// LicenseDetail is everything stored of a license but its key.
type LicenseDetail struct {
	LicenseSummary
	KeyFingerprint string
	Kid            string
	// Features is nil when the key lists none and allows all of them.
	Features []string
	// Limits is nil when the key carries none.
	Limits *license.Limits
	// StoredTelemetry is what the key says, which LicenseSummary.Telemetry gives a meaning to.
	StoredTelemetry string
	IssuedAt        time.Time
	// GraceDays is nil when the key does not say.
	GraceDays             *int
	SigningKeyFingerprint string
	Note                  string
	Origin                string
	CreatedAt             time.Time
	// IssuedBy is the operator who issued the license from the console and JournaledAt the instant
	// the journal gives to it. Empty and zero for a license that came from the registry.
	IssuedBy    string
	JournaledAt time.Time
	// PredecessorID is the license this one succeeds, empty when it succeeds none.
	PredecessorID string
	SuccessorIDs  []string
	// Instances come with the one heard of last first.
	Instances []InstanceSummary
	// SealRejections are the ones of the last SealRejectionDays days, the latest day first.
	SealRejections []SealRejection
}

// InstanceSummary is an instance as seen under a license.
type InstanceSummary struct {
	LicenseID      string
	InstanceID     string
	FirstSeenAt    time.Time
	LastSeenAt     time.Time
	LastReportDay  time.Time
	HusonymVersion string
	// InstallKind is empty when no report told it.
	InstallKind string
}

// InstanceDetail is an instance with what its license caps and the reports received of it.
type InstanceDetail struct {
	InstanceSummary
	CustomerID   uuid.UUID
	CustomerName string
	// Limits is nil when the key of the license carries none.
	Limits *license.Limits
	// Reports come with the newest day first, InstanceReportsCap of them at most.
	Reports []ReportSummary
}

// ReportSummary is a stored report as a list shows it.
type ReportSummary struct {
	Day        time.Time
	ReceivedAt time.Time
	Conflicts  int
	// Sources is the number of sources the document tells, nil when it tells none that can be read.
	Sources *int
}

// StoredReport is a report as it was received, what happened to its row since, and the customer
// of its license.
type StoredReport struct {
	LicenseID    string
	InstanceID   string
	CustomerID   uuid.UUID
	CustomerName string
	Day          time.Time
	// Document is the exact bytes received, which the seal is over.
	Document   []byte
	Seal       string
	ReceivedAt time.Time
	Conflicts  int
	// LastConflictAt is zero when no other document came for that instance and day.
	LastConflictAt time.Time
}

// SealRejection is how many reports were refused for their seal under a license on a day.
type SealRejection struct {
	LicenseID    string
	CustomerID   uuid.UUID
	CustomerName string
	Day          time.Time
	Count        int
	LastAt       time.Time
}

// PendingGroup is what is pending under one fingerprint.
type PendingGroup struct {
	KeyFingerprint string
	Reports        int
	// OldReports is how many of them were received more than OldPendingAfter ago.
	OldReports  int
	Oldest      time.Time
	Newest      time.Time
	InstanceIDs []string
}

// Customers lists the customers, ordered by name.
func (s *Store) Customers(ctx context.Context, now time.Time) ([]CustomerSummary, error) {
	rows, err := cpdb.New(s.pool).ListCustomerSummaries(ctx, cpdb.ListCustomerSummariesParams{
		SeenSince: recentSince(now),
		Now:       toTimestamptz(now),
	})
	if err != nil {
		return nil, fmt.Errorf("unable to list the customers: %w", err)
	}
	customers := make([]CustomerSummary, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		customers = append(customers, CustomerSummary{
			ID:              row.ID.Bytes,
			ExternalID:      row.ExternalID,
			Name:            row.Name,
			Licenses:        int(row.Licenses),
			RecentInstances: int(row.RecentInstances),
			NearestExpiry:   toTime(row.NearestExpiry),
		})
	}
	return customers, nil
}

// Customer gives a customer, its licenses with their state at now, and its instances.
// ErrNotFound when no customer has the id.
func (s *Store) Customer(ctx context.Context, id uuid.UUID, now time.Time) (*CustomerDetail, error) {
	queries := cpdb.New(s.pool)
	customerID := pgtype.UUID{Bytes: id, Valid: true}
	row, err := queries.GetCustomer(ctx, customerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("unable to read a customer: %w", err)
	}
	customer := &CustomerDetail{
		ID:         row.ID.Bytes,
		ExternalID: row.ExternalID,
		Name:       row.Name,
		Note:       row.Note,
		CreatedAt:  toTime(row.CreatedAt),
		UpdatedAt:  toTime(row.UpdatedAt),
	}

	licenses, err := queries.ListLicensesOfCustomer(ctx, customerID)
	if err != nil {
		return nil, fmt.Errorf("unable to list the licenses of a customer: %w", err)
	}
	customer.Licenses = make([]LicenseSummary, 0, len(licenses))
	for i := range licenses {
		l := &licenses[i]
		customer.Licenses = append(customer.Licenses, licenseSummary(
			l.ID, l.CustomerID, l.CustomerName, l.Plan, l.Telemetry, l.ExpiresAt, l.GraceDays, l.HasSuccessor, now))
	}

	instances, err := queries.ListInstancesOfCustomer(ctx, customerID)
	if err != nil {
		return nil, fmt.Errorf("unable to list the instances of a customer: %w", err)
	}
	customer.Instances = make([]InstanceSummary, 0, len(instances))
	for i := range instances {
		customer.Instances = append(customer.Instances, instanceSummary(&instances[i].ControlplaneInstance))
	}
	return customer, nil
}

// LicenseDetail gives everything stored of a license but its key, with its state at now, the
// licenses before and after it, its instances and its late seal rejections. ErrNotFound when no
// license has the id.
func (s *Store) LicenseDetail(ctx context.Context, id string, now time.Time) (*LicenseDetail, error) {
	queries := cpdb.New(s.pool)
	row, err := queries.GetLicenseDetail(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("unable to read a license: %w", err)
	}
	limits, err := toLimits(row.Limits)
	if err != nil {
		return nil, err
	}
	successors, err := queries.ListSuccessorsOfLicense(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("unable to list the successors of a license: %w", err)
	}
	detail := &LicenseDetail{
		LicenseSummary: licenseSummary(
			row.ID, row.CustomerID, row.CustomerName, row.Plan, row.Telemetry, row.ExpiresAt, row.GraceDays,
			len(successors) > 0, now),
		KeyFingerprint:        row.KeyFingerprint,
		Kid:                   row.Kid,
		Features:              row.Features,
		Limits:                limits,
		StoredTelemetry:       row.Telemetry,
		IssuedAt:              toTime(row.IssuedAt),
		GraceDays:             toGraceDays(row.GraceDays),
		SigningKeyFingerprint: row.SigningKeyFingerprint,
		Note:                  row.Note,
		Origin:                row.Origin,
		CreatedAt:             toTime(row.CreatedAt),
		PredecessorID:         row.SucceedsLicenseID.String,
		SuccessorIDs:          successors,
	}
	issuing, err := queries.GetLicenseIssuing(ctx, pgtype.Text{String: id, Valid: true})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return nil, fmt.Errorf("unable to read who issued a license: %w", err)
	default:
		detail.IssuedBy = issuing.Operator
		detail.JournaledAt = toTime(issuing.At)
	}

	instances, err := queries.ListInstancesOfLicense(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("unable to list the instances of a license: %w", err)
	}
	detail.Instances = make([]InstanceSummary, 0, len(instances))
	for i := range instances {
		detail.Instances = append(detail.Instances, instanceSummary(&instances[i].ControlplaneInstance))
	}

	detail.SealRejections, err = sealRejections(ctx, queries, now, pgtype.Text{String: id, Valid: true})
	if err != nil {
		return nil, err
	}
	return detail, nil
}

// Instance gives an instance as seen under a license, what that license caps, and the reports
// received of it. ErrNotFound when the license was never seen on that instance.
func (s *Store) Instance(ctx context.Context, licenseID, instanceID string) (*InstanceDetail, error) {
	queries := cpdb.New(s.pool)
	row, err := queries.GetInstance(ctx, cpdb.GetInstanceParams{LicenseID: licenseID, InstanceID: instanceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("unable to read an instance: %w", err)
	}
	limits, err := toLimits(row.Limits)
	if err != nil {
		return nil, err
	}
	reports, err := queries.ListReportsOfInstance(ctx, cpdb.ListReportsOfInstanceParams{
		LicenseID:  licenseID,
		InstanceID: instanceID,
		AtMost:     InstanceReportsCap,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to list the reports of an instance: %w", err)
	}
	instance := &InstanceDetail{
		InstanceSummary: instanceSummary(&row.ControlplaneInstance),
		CustomerID:      row.CustomerID.Bytes,
		CustomerName:    row.CustomerName,
		Limits:          limits,
		Reports:         make([]ReportSummary, 0, len(reports)),
	}
	for i := range reports {
		report := &reports[i]
		instance.Reports = append(instance.Reports, ReportSummary{
			Day:        report.Day.Time,
			ReceivedAt: toTime(report.ReceivedAt),
			Conflicts:  int(report.Conflicts),
			Sources:    sourcesCount(report.Document),
		})
	}
	return instance, nil
}

// Report gives the report stored for a license, an instance and the UTC day of day, with the
// customer of the license. ErrNotFound when there is none.
func (s *Store) Report(ctx context.Context, licenseID, instanceID string, day time.Time) (*StoredReport, error) {
	row, err := cpdb.New(s.pool).GetUsageReport(ctx, cpdb.GetUsageReportParams{
		LicenseID:  licenseID,
		InstanceID: instanceID,
		Day:        utcDate(day),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("unable to read a usage report: %w", err)
	}
	return &StoredReport{
		LicenseID:      row.LicenseID,
		InstanceID:     row.InstanceID,
		CustomerID:     row.CustomerID.Bytes,
		CustomerName:   row.CustomerName,
		Day:            row.Day.Time,
		Document:       []byte(row.Document),
		Seal:           row.Seal,
		ReceivedAt:     toTime(row.ReceivedAt),
		Conflicts:      int(row.Conflicts),
		LastConflictAt: toTime(row.LastConflictAt),
	}, nil
}

// PendingByFingerprint groups the pending reports by fingerprint, the group waiting for the
// longest first. now tells which of them are old.
func (s *Store) PendingByFingerprint(ctx context.Context, now time.Time) ([]PendingGroup, error) {
	rows, err := cpdb.New(s.pool).ListPendingGroups(ctx, toTimestamptz(now.Add(-OldPendingAfter)))
	if err != nil {
		return nil, fmt.Errorf("unable to group the pending usage reports: %w", err)
	}
	groups := make([]PendingGroup, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		groups = append(groups, PendingGroup{
			KeyFingerprint: row.KeyFingerprint,
			Reports:        int(row.Reports),
			OldReports:     int(row.OldReports),
			Oldest:         toTime(row.Oldest),
			Newest:         toTime(row.Newest),
			InstanceIDs:    row.InstanceIds,
		})
	}
	return groups, nil
}

// sealRejections lists the seal rejections of the last SealRejectionDays days, of one license or,
// without one, of all of them.
func sealRejections(
	ctx context.Context, queries *cpdb.Queries, now time.Time, licenseID pgtype.Text,
) ([]SealRejection, error) {
	rows, err := queries.ListSealRejections(ctx, cpdb.ListSealRejectionsParams{
		SinceDay:  utcDate(now.UTC().AddDate(0, 0, -(SealRejectionDays - 1))),
		LicenseID: licenseID,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to list the seal rejections: %w", err)
	}
	rejections := make([]SealRejection, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		rejections = append(rejections, SealRejection{
			LicenseID:    row.LicenseID,
			CustomerID:   row.CustomerID.Bytes,
			CustomerName: row.CustomerName,
			Day:          row.Day.Time,
			Count:        int(row.Count),
			LastAt:       toTime(row.LastAt),
		})
	}
	return rejections, nil
}

// licenseSummary reads what the columns of a license say at now: its state as its key would give
// it, and its telemetry mode as its key would read it.
func licenseSummary(
	id string, customerID pgtype.UUID, customerName, plan, telemetry string,
	expiresAt pgtype.Timestamptz, graceDays pgtype.Int4, hasSuccessor bool, now time.Time,
) LicenseSummary {
	key := &license.Key{ExpiresAt: expiresAt.Time, GraceDays: toGraceDays(graceDays), Telemetry: telemetry}
	return LicenseSummary{
		ID:           id,
		CustomerID:   customerID.Bytes,
		CustomerName: customerName,
		Plan:         plan,
		Telemetry:    key.TelemetryMode(),
		ExpiresAt:    toTime(expiresAt),
		State:        key.StateAt(now),
		HasSuccessor: hasSuccessor,
	}
}

func instanceSummary(row *cpdb.ControlplaneInstance) InstanceSummary {
	return InstanceSummary{
		LicenseID:      row.LicenseID,
		InstanceID:     row.InstanceID,
		FirstSeenAt:    toTime(row.FirstSeenAt),
		LastSeenAt:     toTime(row.LastSeenAt),
		LastReportDay:  row.LastReportDay.Time,
		HusonymVersion: row.HusonymVersion,
		InstallKind:    row.InstallKind.String,
	}
}

// sourcesCount reads the number of sources a report tells. A document that does not parse, or
// that tells none, gives nil: the list of the reports of an instance is shown all the same.
func sourcesCount(document string) *int {
	var report struct {
		Sources struct {
			Count *int `json:"count"`
		} `json:"sources"`
	}
	if err := json.Unmarshal([]byte(document), &report); err != nil {
		return nil
	}
	return report.Sources.Count
}

func toLimits(stored []byte) (*license.Limits, error) {
	if stored == nil {
		return nil, nil
	}
	var limits license.Limits
	if err := json.Unmarshal(stored, &limits); err != nil {
		return nil, fmt.Errorf("unable to read the limits of a license: %w", err)
	}
	return &limits, nil
}

func toGraceDays(stored pgtype.Int4) *int {
	if !stored.Valid {
		return nil
	}
	days := int(stored.Int32)
	return &days
}

// toTime is the instant stored, in UTC; zero for a NULL.
func toTime(stored pgtype.Timestamptz) time.Time {
	if !stored.Valid {
		return time.Time{}
	}
	return stored.Time.UTC()
}

// recentSince is the first day an instance seen lately may have last reported on.
func recentSince(now time.Time) pgtype.Date {
	return utcDate(now.UTC().AddDate(0, 0, -RecentInstanceDays))
}
