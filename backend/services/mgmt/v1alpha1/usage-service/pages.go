package v1alpha1_usageservice

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/internal/usagestore"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// latestRuns is how many runs of a job its usage lists at most.
const latestRuns = 20

// jobKindPrefix is what the names of the JobKind enum start with: the rest is the kind as the
// usage store names it, in upper case.
const jobKindPrefix = "JOB_KIND_"

// pageStore is what the usage pages are read from: usagestore.Store.
type pageStore interface {
	UsageTotals(ctx context.Context, scope usagestore.Scope, period usagestore.Period) (*usagestore.UsageTotals, error)
	UsageDays(ctx context.Context, scope usagestore.Scope, period usagestore.Period) ([]usagestore.UsageDay, error)
	UsageJobs(ctx context.Context, accountId string, period usagestore.Period) ([]usagestore.JobUsage, error)
	UsageErrors(ctx context.Context, accountId string, period usagestore.Period) ([]usagestore.CategoryCount, error)
	AccountRefusals(ctx context.Context, accountId string, period usagestore.Period) ([]usagestore.GateCount, error)
	LatestRuns(ctx context.Context, scope usagestore.Scope, period usagestore.Period, limit int32) ([]usagestore.RunRow, error)
}

// GetAccountUsage gives what the runs of an account add up to over a period of days, from the
// counters of the instance: its totals, each of its days, its jobs, its errors by category and
// what the license refused it.
//
// The days are the ones of the time zone asked, and of UTC when the zone is not known: to this
// service, or to the database, which has its own list of zones. The answer names the zone the
// days were counted in. The refusals of the license are kept by UTC day and stay so whatever the
// zone.
//
// The totals count every run of the account. The jobs are the ones that have a run counted in the
// period and still exist: the runs of a job deleted since are in the totals and in no line of the
// jobs. The kind of a job is the one its runs tell, and only its name is read from the job.
func (s *Service) GetAccountUsage(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetAccountUsageRequest],
) (*connect.Response[mgmtv1alpha1.GetAccountUsageResponse], error) {
	accountId := req.Msg.GetAccountId()
	if err := s.canView(ctx, accountId); err != nil {
		return nil, err
	}

	scope := usagestore.Scope{AccountId: accountId}
	var (
		totals   *usagestore.UsageTotals
		days     []usagestore.UsageDay
		jobs     []usagestore.JobUsage
		failures []usagestore.CategoryCount
		refusals []usagestore.GateCount
	)
	asked := periodOf(req.Msg.GetFromDay(), req.Msg.GetToDay(), req.Msg.GetTimeZone())
	read, err := usagestore.ReadInZone(asked, func(period usagestore.Period) error {
		var err error
		if totals, err = s.pages.UsageTotals(ctx, scope, period); err != nil {
			return err
		}
		if days, err = s.pages.UsageDays(ctx, scope, period); err != nil {
			return err
		}
		if jobs, err = s.pages.UsageJobs(ctx, accountId, period); err != nil {
			return err
		}
		if failures, err = s.pages.UsageErrors(ctx, accountId, period); err != nil {
			return err
		}
		refusals, err = s.pages.AccountRefusals(ctx, accountId, period)
		return err
	})
	if err != nil {
		return nil, pageError(err)
	}
	names, err := s.jobNamesOf(ctx, accountId)
	if err != nil {
		return nil, err
	}

	res := &mgmtv1alpha1.GetAccountUsageResponse{
		Totals:               totalsOf(totals),
		DurationTotalSeconds: totals.DurationTotal,
		Days:                 daysOf(days),
		TimeZone:             read.ZoneName(),
	}
	for i := range jobs {
		job := &jobs[i]
		// A job that is gone has no name to show and no page to lead to.
		name, ok := names[job.JobId]
		if !ok {
			continue
		}
		res.Jobs = append(res.Jobs, &mgmtv1alpha1.JobUsage{
			JobId:                 job.JobId,
			JobName:               name,
			Kind:                  kindOf(job.Kind),
			Totals:                totalsOf(&job.Totals),
			DurationMedianSeconds: job.Totals.DurationMedian,
		})
	}
	for _, failure := range failures {
		res.Errors = append(res.Errors, &mgmtv1alpha1.UsageErrorCount{
			Category: toldCategory(failure.Category), Runs: failure.Count,
		})
	}
	for _, refusal := range refusals {
		res.Refusals = append(res.Refusals, &mgmtv1alpha1.GateRefusalCount{
			Gate: string(refusal.Gate), Refusals: refusal.Count,
		})
	}
	return connect.NewResponse(res), nil
}

// GetJobUsage gives what the runs of a job of an account add up to over a period of days, each
// of its days, and the latest of its runs counted in the period.
//
// The runs are read by account and by job at once, and so is the job, once, for its kind: a job
// of another account has no run and no kind here, and the answer is the one of a job that never
// ran, which tells nothing of what other accounts hold. The other jobs of the account are not
// read.
func (s *Service) GetJobUsage(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetJobUsageRequest],
) (*connect.Response[mgmtv1alpha1.GetJobUsageResponse], error) {
	accountId, jobId := req.Msg.GetAccountId(), req.Msg.GetJobId()
	if err := s.canView(ctx, accountId); err != nil {
		return nil, err
	}

	scope := usagestore.Scope{AccountId: accountId, JobId: jobId}
	var (
		totals *usagestore.UsageTotals
		days   []usagestore.UsageDay
		runs   []usagestore.RunRow
	)
	asked := periodOf(req.Msg.GetFromDay(), req.Msg.GetToDay(), req.Msg.GetTimeZone())
	read, err := usagestore.ReadInZone(asked, func(period usagestore.Period) error {
		var err error
		if totals, err = s.pages.UsageTotals(ctx, scope, period); err != nil {
			return err
		}
		if days, err = s.pages.UsageDays(ctx, scope, period); err != nil {
			return err
		}
		runs, err = s.pages.LatestRuns(ctx, scope, period, latestRuns)
		return err
	})
	if err != nil {
		return nil, pageError(err)
	}
	kind, err := s.kindOfJob(ctx, accountId, jobId)
	if err != nil {
		return nil, err
	}

	res := &mgmtv1alpha1.GetJobUsageResponse{
		// The kind is the one of the job, so that a job with no run in the period tells it too.
		Kind:                  kindOf(kind),
		Totals:                totalsOf(totals),
		DurationMedianSeconds: totals.DurationMedian,
		Days:                  daysOf(days),
		TimeZone:              read.ZoneName(),
	}
	for i := range runs {
		run := &runs[i]
		res.Runs = append(res.Runs, &mgmtv1alpha1.RunUsage{
			RunId:           run.RunId,
			Status:          runStatusOf(run.Status),
			StartedAt:       timestamppb.New(run.StartedAt),
			EndedAt:         optionalTimestamp(run.EndedAt),
			RowsRead:        run.RowsRead,
			TablesUncounted: run.TablesUncounted,
			ErrorCategory:   toldCategory(run.Error.Category),
			ErrorStep:       toldStep(run.Error.Step),
		})
	}
	return connect.NewResponse(res), nil
}

// jobNamesOf gives the names of the jobs the account holds, by id. It is asked of the account
// alone: a job of another account is never read.
func (s *Service) jobNamesOf(ctx context.Context, accountId string) (map[string]string, error) {
	accountUuid, err := husonymdb.ToUuid(accountId)
	if err != nil {
		return nil, husonymerrors.NewBadRequest("the account id is not a uuid")
	}
	rows, err := s.db.Q.ListJobNamesByAccount(ctx, s.db.Db, accountUuid)
	if err != nil {
		return nil, fmt.Errorf("unable to read the names of the jobs of the account: %w", err)
	}
	names := make(map[string]string, len(rows))
	for _, row := range rows {
		names[husonymdb.UUIDString(row.ID)] = row.Name
	}
	return names, nil
}

// kindOfJob gives the kind of a job of the account, and no kind for a job the account does not
// hold. The job is asked by its id and by the account at once: a job of another account is never
// read.
func (s *Service) kindOfJob(ctx context.Context, accountId, jobId string) (usagestore.JobKind, error) {
	accountUuid, err := husonymdb.ToUuid(accountId)
	if err != nil {
		return "", husonymerrors.NewBadRequest("the account id is not a uuid")
	}
	jobUuid, err := husonymdb.ToUuid(jobId)
	if err != nil {
		return "", husonymerrors.NewBadRequest("the job id is not a uuid")
	}
	row, err := s.db.Q.GetJobKindSourceByAccount(ctx, s.db.Db, db_queries.GetJobKindSourceByAccountParams{
		ID: jobUuid, AccountID: accountUuid,
	})
	if husonymdb.IsNoRows(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("unable to read the job of the account: %w", err)
	}
	return usagestore.KindOfJob(&db_queries.HusonymApiJob{
		ConnectionOptions: row.ConnectionOptions, JobtypeConfig: row.JobtypeConfig,
	}), nil
}

// periodOf is the period a request asks for: its two days as they are written, in its time
// zone. A zone that is not named, or whose name is not one, is UTC: the page of a browser whose
// zone is not known here is not refused for it. Days that make no period are refused by the
// store, which is the one to know what a period is.
func periodOf(from, to *mgmtv1alpha1.Date, zone string) usagestore.Period {
	location, err := time.LoadLocation(zone)
	if err != nil {
		location = time.UTC
	}
	return usagestore.Period{From: calendarDayOf(from), To: calendarDayOf(to), Zone: location}
}

func calendarDayOf(date *mgmtv1alpha1.Date) usagestore.CalendarDay {
	return usagestore.CalendarDay{
		Year: int(date.GetYear()), Month: time.Month(date.GetMonth()), Day: int(date.GetDay()),
	}
}

// pageError tells a period that cannot be read, which is the fault of the request, from a read
// that failed.
func pageError(err error) error {
	if errors.Is(err, usagestore.ErrPeriod) {
		return husonymerrors.NewBadRequest(err.Error())
	}
	return err
}

func totalsOf(totals *usagestore.UsageTotals) *mgmtv1alpha1.UsageTotals {
	return &mgmtv1alpha1.UsageTotals{
		Runs:                  totals.Runs,
		RunsCompleted:         totals.Completed,
		RunsCanceled:          totals.Canceled,
		RowsRead:              totals.RowsRead,
		RowsDiscarded:         totals.RowsDiscarded,
		RunsWithUncountedRows: totals.WithUncountedRows,
	}
}

func daysOf(days []usagestore.UsageDay) []*mgmtv1alpha1.UsageDay {
	told := make([]*mgmtv1alpha1.UsageDay, 0, len(days))
	for _, day := range days {
		told = append(told, &mgmtv1alpha1.UsageDay{
			//nolint:gosec // the parts of a calendar day are positive
			Day:      &mgmtv1alpha1.Date{Year: uint32(day.Day.Year), Month: uint32(day.Day.Month), Day: uint32(day.Day.Day)},
			RowsRead: day.RowsRead,
			Runs:     day.Runs,
		})
	}
	return told
}

// runStatusOf tells the status of a run of the usage store as the runs of a job are told
// everywhere else.
func runStatusOf(status usagestore.Status) mgmtv1alpha1.JobRunStatus {
	switch status {
	case usagestore.StatusRunning:
		return mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_RUNNING
	case usagestore.StatusCompleted:
		return mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_COMPLETE
	case usagestore.StatusFailed:
		return mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_FAILED
	case usagestore.StatusCanceled:
		return mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_CANCELED
	case usagestore.StatusTerminated:
		return mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_TERMINATED
	case usagestore.StatusTimedOut:
		return mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_TIMED_OUT
	}
	return mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_UNSPECIFIED
}

// toldCategory is the category of an error as the worker tells it: toldError the other way
// round, by the same names. No category is unspecified, and one the enum does not name is other.
func toldCategory(category usagestore.ErrorCategory) mgmtv1alpha1.RunErrorCategory {
	if category == "" {
		return mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_UNSPECIFIED
	}
	if number := mgmtv1alpha1.RunErrorCategory_value[errorCategoryPrefix+strings.ToUpper(string(category))]; number != 0 {
		return mgmtv1alpha1.RunErrorCategory(number)
	}
	return mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_OTHER
}

// toldStep is the step of an error as the worker tells it, as toldCategory is for its category.
func toldStep(step usagestore.ErrorStep) mgmtv1alpha1.RunErrorStep {
	if step == "" {
		return mgmtv1alpha1.RunErrorStep_RUN_ERROR_STEP_UNSPECIFIED
	}
	if number := mgmtv1alpha1.RunErrorStep_value[errorStepPrefix+strings.ToUpper(string(step))]; number != 0 {
		return mgmtv1alpha1.RunErrorStep(number)
	}
	return mgmtv1alpha1.RunErrorStep_RUN_ERROR_STEP_OTHER
}

// kindOf is the kind of a job by the name the usage store gives it. A kind the enum does not
// name, and the one of a job that is not there, is unspecified.
func kindOf(kind usagestore.JobKind) mgmtv1alpha1.JobKind {
	return mgmtv1alpha1.JobKind(mgmtv1alpha1.JobKind_value[jobKindPrefix+strings.ToUpper(string(kind))])
}
