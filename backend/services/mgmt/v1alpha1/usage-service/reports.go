package v1alpha1_usageservice

import (
	"context"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/internal/usagereport"
	"github.com/fishtre-compagnie/husonym/backend/internal/usagestore"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/rbac"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	// reportDays is how many closed days back the reports are told of: the window the sender
	// sends within.
	reportDays = 30
	// waitBeforeFirst is how long an instance that starts sending waits before it sends anything.
	waitBeforeFirst = 24 * time.Hour
)

// reportStore is what the service reads the usage reports and their sending from:
// usagestore.Store.
type reportStore interface {
	SendingSince(ctx context.Context) (*time.Time, error)
	LastSentAt(ctx context.Context) (*time.Time, error)
	ListReportSendings(ctx context.Context, from, to time.Time) ([]usagestore.ReportSending, error)
	Report(ctx context.Context, day time.Time) (*usagestore.StoredReport, error)
}

// periodBuilder makes the sealed usage report for a period of months: usagereport.Builder.
type periodBuilder interface {
	BuildPeriod(ctx context.Context, from, to, now time.Time) (*usagereport.Sealed, error)
}

// GetUsageReporting tells under which mode the usage report of the instance is sent, and what
// became of the reports of the last 30 days. Without a license key in force there is no mode:
// nothing is prepared nor sent, and nothing is listed.
func (s *Service) GetUsageReporting(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetUsageReportingRequest],
) (*connect.Response[mgmtv1alpha1.GetUsageReportingResponse], error) {
	if err := s.canView(ctx, req.Msg.GetAccountId()); err != nil {
		return nil, err
	}

	now := s.now()
	keyMode, err := s.key.TelemetryMode(ctx, now)
	if errors.Is(err, usagereport.ErrNoLicenseInForce) {
		return connect.NewResponse(&mgmtv1alpha1.GetUsageReportingResponse{
			Diagnostics: s.cfg.Diagnostics,
		}), nil
	}
	if err != nil {
		return nil, fmt.Errorf("unable to read the usage report mode of the license: %w", err)
	}
	mode, below := telemetry.EffectiveMode(keyMode, s.cfg.ModeSetting)
	res := &mgmtv1alpha1.GetUsageReportingResponse{
		LicenseMode:  licenseModeOf(keyMode),
		Mode:         modeOf(mode),
		BelowLicense: below,
		Diagnostics:  s.cfg.Diagnostics,
	}

	lastSent, err := s.reports.LastSentAt(ctx)
	if err != nil {
		return nil, err
	}
	res.LastSentAt = optionalTimestamp(lastSent)

	// What was recorded of an instance that stopped sending may linger until the next pass.
	var since *time.Time
	if mode == telemetry.ModeOnline {
		if since, err = s.reports.SendingSince(ctx); err != nil {
			return nil, err
		}
	}
	res.SendingSince = optionalTimestamp(since)
	if since != nil {
		if first := since.Add(waitBeforeFirst); first.After(now) {
			res.FirstSendAt = timestamppb.New(first)
		}
		res.Silent = now.Sub(*since) > reportDays*24*time.Hour &&
			(lastSent == nil || now.Sub(*lastSent) > reportDays*24*time.Hour)
	}

	yesterday := now.UTC().Truncate(24*time.Hour).AddDate(0, 0, -1)
	sendings, err := s.reports.ListReportSendings(ctx, yesterday.AddDate(0, 0, -(reportDays-1)), yesterday)
	if err != nil {
		return nil, err
	}
	for i := range sendings {
		sending := &sendings[i]
		res.Reports = append(res.Reports, &mgmtv1alpha1.UsageReportSummary{
			Day:      dateOf(sending.Day),
			Status:   statusOfSending(sending, mode, since),
			SentAt:   optionalTimestamp(sending.SentAt),
			Attempts: sending.Attempts,
		})
	}
	return connect.NewResponse(res), nil
}

// GetUsageReport gives the usage report of a day as it is kept: the document is the stored one,
// byte for byte.
func (s *Service) GetUsageReport(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetUsageReportRequest],
) (*connect.Response[mgmtv1alpha1.GetUsageReportResponse], error) {
	if err := s.canView(ctx, req.Msg.GetAccountId()); err != nil {
		return nil, err
	}
	day, ok := dayOf(req.Msg.GetDay(), s.now())
	if !ok {
		return nil, husonymerrors.NewBadRequest("the day is not a day of the calendar, or it is in the future")
	}
	report, err := s.reports.Report(ctx, day)
	if err != nil {
		return nil, err
	}
	if report == nil {
		return nil, husonymerrors.NewNotFound("there is no usage report for that day")
	}
	return connect.NewResponse(&mgmtv1alpha1.GetUsageReportResponse{
		Document:       string(report.Document),
		Seal:           report.Seal,
		KeyFingerprint: report.KeyFingerprint,
	}), nil
}

// GetUsagePeriodReport makes the usage report of the instance for a period of months and seals
// it with the license key in force, whatever the mode the report of the day is sent under. The
// document is the one the seal is of, byte for byte.
func (s *Service) GetUsagePeriodReport(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetUsagePeriodReportRequest],
) (*connect.Response[mgmtv1alpha1.GetUsagePeriodReportResponse], error) {
	if err := s.canView(ctx, req.Msg.GetAccountId()); err != nil {
		return nil, err
	}
	from, errFrom := time.Parse(telemetry.MonthLayout, req.Msg.GetFromMonth())
	to, errTo := time.Parse(telemetry.MonthLayout, req.Msg.GetToMonth())
	if errFrom != nil || errTo != nil {
		return nil, husonymerrors.NewBadRequest("a month of the period is not a month of the calendar, written as 2026-01")
	}
	sealed, err := s.periods.BuildPeriod(ctx, from, to, s.now())
	switch {
	case errors.Is(err, usagereport.ErrNoLicenseInForce):
		return nil, husonymerrors.NewFailedPrecondition("no license key is in force: no usage report is made")
	case errors.Is(err, usagereport.ErrPeriod):
		return nil, husonymerrors.NewBadRequest(err.Error())
	case err != nil:
		return nil, fmt.Errorf("unable to make the usage report of the period: %w", err)
	}
	return connect.NewResponse(&mgmtv1alpha1.GetUsagePeriodReportResponse{
		Document:       string(sealed.Document),
		Seal:           sealed.Seal,
		KeyFingerprint: sealed.KeyFingerprint,
	}), nil
}

// canView lets through who may see the account, as GetLicenseUsage does.
func (s *Service) canView(ctx context.Context, accountId string) error {
	user, err := s.userdataclient.GetUser(ctx)
	if err != nil {
		return err
	}
	return user.EnforceAccount(ctx, userdata.NewIdentifier(accountId), rbac.AccountAction_View)
}

// statusOfSending tells what became of a report. A report is kept when the instance does not
// send, or when its day precedes the day sending began: the sender never takes it.
func statusOfSending(
	sending *usagestore.ReportSending, mode telemetry.Mode, since *time.Time,
) mgmtv1alpha1.UsageReportStatus {
	switch {
	case sending.SentAt != nil:
		return mgmtv1alpha1.UsageReportStatus_USAGE_REPORT_STATUS_SENT
	case mode != telemetry.ModeOnline || since == nil || sending.Day.UTC().Before(since.UTC().Truncate(24*time.Hour)):
		return mgmtv1alpha1.UsageReportStatus_USAGE_REPORT_STATUS_KEPT
	case sending.Attempts > 0:
		return mgmtv1alpha1.UsageReportStatus_USAGE_REPORT_STATUS_NOT_SENT
	}
	return mgmtv1alpha1.UsageReportStatus_USAGE_REPORT_STATUS_TO_BE_SENT
}

func licenseModeOf(mode license.TelemetryMode) mgmtv1alpha1.UsageReportingMode {
	switch mode {
	case license.TelemetryNone:
		return mgmtv1alpha1.UsageReportingMode_USAGE_REPORTING_MODE_NONE
	case license.TelemetryOfflineReport:
		return mgmtv1alpha1.UsageReportingMode_USAGE_REPORTING_MODE_OFFLINE_REPORT
	case license.TelemetryOnline:
		return mgmtv1alpha1.UsageReportingMode_USAGE_REPORTING_MODE_ONLINE
	}
	return mgmtv1alpha1.UsageReportingMode_USAGE_REPORTING_MODE_ONLINE
}

func modeOf(mode telemetry.Mode) mgmtv1alpha1.UsageReportingMode {
	switch mode {
	case telemetry.ModeNone:
		return mgmtv1alpha1.UsageReportingMode_USAGE_REPORTING_MODE_NONE
	case telemetry.ModeOfflineReport:
		return mgmtv1alpha1.UsageReportingMode_USAGE_REPORTING_MODE_OFFLINE_REPORT
	case telemetry.ModeOnline:
		return mgmtv1alpha1.UsageReportingMode_USAGE_REPORTING_MODE_ONLINE
	}
	return mgmtv1alpha1.UsageReportingMode_USAGE_REPORTING_MODE_UNSPECIFIED
}

func optionalTimestamp(at *time.Time) *timestamppb.Timestamp {
	if at == nil {
		return nil
	}
	return timestamppb.New(*at)
}

func dateOf(day time.Time) *mgmtv1alpha1.Date {
	day = day.UTC()
	//nolint:gosec // the parts of a calendar day are positive
	return &mgmtv1alpha1.Date{Year: uint32(day.Year()), Month: uint32(day.Month()), Day: uint32(day.Day())}
}

// dayOf reads a date as a UTC day. It is false for a date that is not a day of the calendar,
// or that is after today.
func dayOf(date *mgmtv1alpha1.Date, now time.Time) (time.Time, bool) {
	year, month, dayOfMonth := date.GetYear(), date.GetMonth(), date.GetDay()
	if year < 1 || year > 9999 || month < 1 || month > 12 || dayOfMonth < 1 || dayOfMonth > 31 {
		return time.Time{}, false
	}
	day := time.Date(int(year), time.Month(month), int(dayOfMonth), 0, 0, 0, 0, time.UTC)
	// time.Date rolls an impossible day over to the next month.
	if day.Day() != int(dayOfMonth) || day.After(now.UTC()) {
		return time.Time{}, false
	}
	return day, true
}
