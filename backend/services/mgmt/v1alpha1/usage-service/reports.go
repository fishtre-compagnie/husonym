package v1alpha1_usageservice

import (
	"context"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	"github.com/fishtre-compagnie/husonym/backend/internal/usagereport"
	"github.com/fishtre-compagnie/husonym/backend/internal/usagestore"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/rbac"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// periodBuildTimeout bounds the making of the report for a period: a database that does not
// answer does not hold the caller.
const periodBuildTimeout = 30 * time.Second

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
// became of the reports of the last 30 days: the window the sender sends within. Without a
// license key in force there is no mode: nothing is prepared nor sent, and nothing is listed.
//
// While the instance has sent no report since it started sending, it also tells when the first
// one leaves at the earliest: a day after the oldest report that is to be sent was prepared, as
// the sender waits. There is no such moment while no report is to be sent yet.
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
		res.Silent = now.Sub(*since) > usagereport.SentDays*24*time.Hour &&
			(lastSent == nil || now.Sub(*lastSent) > usagereport.SentDays*24*time.Hour)
	}

	yesterday := now.UTC().Truncate(24*time.Hour).AddDate(0, 0, -1)
	sendings, err := s.reports.ListReportSendings(ctx, yesterday.AddDate(0, 0, -(usagereport.SentDays-1)), yesterday)
	if err != nil {
		return nil, err
	}
	// The reports come newest first: the last one that is to be sent is the oldest.
	var oldestToSend *usagestore.ReportSending
	for i := range sendings {
		sending := &sendings[i]
		status := statusOfSending(sending, mode, since, s.cfg.Diagnostics)
		if status == mgmtv1alpha1.UsageReportStatus_USAGE_REPORT_STATUS_TO_BE_SENT ||
			status == mgmtv1alpha1.UsageReportStatus_USAGE_REPORT_STATUS_NOT_SENT {
			oldestToSend = sending
		}
		res.Reports = append(res.Reports, &mgmtv1alpha1.UsageReportSummary{
			Day:      dateOf(sending.Day),
			Status:   status,
			SentAt:   optionalTimestamp(sending.SentAt),
			Attempts: sending.Attempts,
		})
	}
	if since != nil && oldestToSend != nil && usagereport.WaitsBeforeFirst(*since, lastSent) {
		res.FirstSendAt = timestamppb.New(oldestToSend.PreparedAt.Add(usagereport.WaitBeforeFirst))
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
//
// A period is telemetry.MaxPeriodMonths months at most. The longest one reads the license key,
// the id of the instance, the reports of the day kept for those months in one query (some 730
// documents of a few kilobytes), and the runs and the refusals of each month in three queries a
// month, each over an index: some seventy-five queries in all. The whole is given
// periodBuildTimeout; past it the caller is answered DeadlineExceeded.
//
// Any member who may view an account can ask for it, as often as they like: one period is built
// at a time in a process, so that what the calls cost never adds up to more than one build at
// once. A call waits for its turn within its own periodBuildTimeout, then builds in what is left
// of it.
//
// A report that cannot be made is answered in fixed words, and why is logged: an error of the
// database or of the schema may quote what it read.
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
	buildCtx, cancel := context.WithTimeout(ctx, s.periodTimeout)
	defer cancel()
	if err := s.takePeriodTurn(buildCtx); err != nil {
		if ctx.Err() != nil {
			return nil, connect.NewError(connect.CodeCanceled, errors.New("the report for the period was given up"))
		}
		return nil, connect.NewError(connect.CodeDeadlineExceeded, fmt.Errorf(
			"the report for the period was not built within %s: another one was being built", s.periodTimeout,
		))
	}
	defer func() { <-s.periodTurn }()
	sealed, err := s.periods.BuildPeriod(buildCtx, from, to, s.now())
	switch {
	case err == nil:
	case errors.Is(err, usagereport.ErrNoLicenseInForce):
		return nil, husonymerrors.NewFailedPrecondition("no license key is in force: no usage report is made")
	case errors.Is(err, usagereport.ErrPeriod):
		return nil, husonymerrors.NewBadRequest(err.Error())
	case errors.Is(buildCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil:
		return nil, connect.NewError(connect.CodeDeadlineExceeded, fmt.Errorf(
			"the report for the period was not built within %s", s.periodTimeout,
		))
	default:
		// The builder already logged what the schema refuses of a document.
		if !errors.Is(err, usagereport.ErrPeriodNotBuilt) {
			logger_interceptor.GetLoggerFromContextOrDefault(ctx).WarnContext(
				ctx, "the usage report for a period could not be built", "error", err,
			)
		}
		return nil, husonymerrors.NewInternalError(usagereport.ErrPeriodNotBuilt.Error())
	}
	return connect.NewResponse(&mgmtv1alpha1.GetUsagePeriodReportResponse{
		Document:       string(sealed.Document),
		Seal:           sealed.Seal,
		KeyFingerprint: sealed.KeyFingerprint,
	}), nil
}

// takePeriodTurn waits for the turn to build a period, until ctx ends. A turn that is free is
// taken whatever became of ctx: the build then answers for it, as it does without a wait.
func (s *Service) takePeriodTurn(ctx context.Context) error {
	select {
	case s.periodTurn <- struct{}{}:
		return nil
	default:
	}
	select {
	case s.periodTurn <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
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
// send, when its day precedes the day sending began, or when it carries the diagnostics and they
// are switched off: the sender does not take it.
func statusOfSending(
	sending *usagestore.ReportSending, mode telemetry.Mode, since *time.Time, diagnostics bool,
) mgmtv1alpha1.UsageReportStatus {
	switch {
	case sending.SentAt != nil:
		return mgmtv1alpha1.UsageReportStatus_USAGE_REPORT_STATUS_SENT
	case mode != telemetry.ModeOnline || since == nil || sending.Day.UTC().Before(since.UTC().Truncate(24*time.Hour)):
		return mgmtv1alpha1.UsageReportStatus_USAGE_REPORT_STATUS_KEPT
	case sending.Diagnostics && !diagnostics:
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
	// An unknown mode of the key reads as online, as license.Key.TelemetryMode and telemetry.EffectiveMode do.
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
