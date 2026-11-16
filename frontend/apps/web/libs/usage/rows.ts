import type { LicenseTranslator } from '@/libs/license/i18n';
import { gateLabel } from '@/libs/license/license';
import { timestampDate } from '@bufbuild/protobuf/wkt';
import { JobRunStatus, RunErrorStep } from '@husonym/sdk';
import type {
  GateRefusalCount,
  GetJobUsageResponse,
  JobUsage,
  RunUsage,
  UsageErrorCount,
  UsageTotals,
} from '@husonym/sdk';
import { ENGLISH_USAGE_I18N, UsageI18n } from './i18n';
import { errorCategoryLabel, errorStepLabel } from './labels';
import { formatCount } from './numbers';
import {
  durationLabel,
  noRunLine,
  rowsHint,
  rowsLabel,
  successRateLabel,
} from './totals';

const ZERO = BigInt(0);

// A job in the table of the jobs of an account, every cell ready to show.
interface JobRow {
  id: string;
  name: string;
  // The Usage page of the job.
  href: string;
  rowsRead: string;
  runs: string;
  success: string;
  duration: string;
}

// The table of the jobs of an account, in the order the API gives them (the job that
// read the most rows first). The rows of a job are marked when some of its runs did not
// count them all, as the rows of a run are in the latest runs of a job. The note says,
// once, why some jobs show no row count.
export function jobsTable(
  jobs: readonly JobUsage[],
  accountName: string,
  i18n: UsageI18n = ENGLISH_USAGE_I18N
): { rows: JobRow[]; note: string | undefined } {
  return {
    rows: jobs.map((job) => ({
      id: job.jobId,
      name: job.jobName,
      href: `/${accountName}/jobs/${job.jobId}/usage`,
      rowsRead: rowsLabel(
        job.kind,
        job.totals?.rowsRead ?? ZERO,
        job.totals?.runsWithUncountedRows,
        i18n
      ),
      runs: formatCount(job.totals?.runs ?? ZERO, i18n.locale),
      success: successRateLabel(job.totals),
      duration: durationLabel(job.durationMedianSeconds, i18n),
    })),
    note: jobs.map((job) => rowsHint(job.kind, i18n)).find((hint) => hint),
  };
}

// What the table of jobs says when it has no row. Jobs deleted since are not listed
// while their runs stay counted: runs without a job are not "no job ran".
export function jobsEmptyLine(
  totals: UsageTotals | undefined,
  i18n: UsageI18n = ENGLISH_USAGE_I18N
): string {
  return noRunLine(totals, i18n) === undefined
    ? i18n.t('jobsGone')
    : i18n.t('noJobRan');
}

// What the errors say when there is none.
export function errorsEmptyLine(
  totals: UsageTotals | undefined,
  i18n: UsageI18n = ENGLISH_USAGE_I18N
): string {
  return noRunLine(totals, i18n) ?? i18n.t('allCompleted');
}

// A row of a table of counts: what is counted, and how many.
interface CountRow {
  key: string;
  label: string;
  count: string;
}

// The runs that did not complete, by category, in the order the API gives them (the
// category with the most runs first).
export function errorRows(
  errors: readonly UsageErrorCount[],
  i18n: UsageI18n = ENGLISH_USAGE_I18N
): CountRow[] {
  return errors.map((error) => ({
    key: String(error.category),
    label: errorCategoryLabel(error.category, i18n),
    count: formatCount(error.runs, i18n.locale),
  }));
}

// A run in the latest runs of a job, every cell ready to show but its start and its
// status, which the table hands to what already shows a date and a status.
interface RunRow {
  id: string;
  // The page of the run.
  href: string;
  startedAt: Date | undefined;
  status: JobRunStatus;
  duration: string;
  rowsRead: string;
  // Empty for a run that completed.
  error: string;
}

// The latest runs of a job in the period, in the order the API gives them (the most
// recently recorded first). The API lists a number of them at most: the caption says
// so when the period counts more runs than are listed.
export function runsTable(
  usage: Pick<GetJobUsageResponse, 'runs' | 'totals' | 'kind'>,
  accountName: string,
  i18n: UsageI18n = ENGLISH_USAGE_I18N
): { rows: RunRow[]; caption: string | undefined } {
  const { runs, totals, kind } = usage;
  return {
    rows: runs.map((run) => ({
      id: run.runId,
      href: `/${accountName}/runs/${run.runId}`,
      startedAt: run.startedAt ? timestampDate(run.startedAt) : undefined,
      status: run.status,
      duration: durationLabel(runSeconds(run), i18n),
      rowsRead: rowsLabel(kind, run.rowsRead, run.tablesUncounted, i18n),
      error: runErrorLabel(run, i18n),
    })),
    caption:
      (totals?.runs ?? ZERO) > BigInt(runs.length)
        ? i18n.t('recentRuns', {
            count: formatCount(runs.length, i18n.locale),
          })
        : undefined,
  };
}

// The whole seconds a run lasted, never less than none (two clocks may disagree).
// Nothing for a run without a known start or a known end.
function runSeconds(
  run: Pick<RunUsage, 'startedAt' | 'endedAt'>
): bigint | undefined {
  if (!run.startedAt || !run.endedAt) {
    return undefined;
  }
  const milliseconds =
    timestampDate(run.endedAt).getTime() -
    timestampDate(run.startedAt).getTime();
  return BigInt(Math.max(0, Math.floor(milliseconds / 1000)));
}

// What kept a run from completing and, when it says something, the step it was at.
function runErrorLabel(
  run: Pick<RunUsage, 'status' | 'errorCategory' | 'errorStep'>,
  i18n: UsageI18n
): string {
  if (run.status === JobRunStatus.COMPLETE) {
    return '';
  }
  const category = errorCategoryLabel(run.errorCategory, i18n);
  const saysNothing =
    run.errorStep === RunErrorStep.UNSPECIFIED ||
    run.errorStep === RunErrorStep.OTHER;
  return saysNothing
    ? category
    : i18n.t('errorWithStep', {
        category,
        step: errorStepLabel(run.errorStep, i18n),
      });
}

// What the license refused, by gate. A gate that refused nothing is no row.
export function refusalRows(
  refusals: readonly GateRefusalCount[],
  locale?: string,
  t?: LicenseTranslator
): CountRow[] {
  return refusals
    .filter((refusal) => refusal.refusals > ZERO)
    .map((refusal) => ({
      key: refusal.gate,
      label: gateLabel(refusal.gate, t),
      count: formatCount(refusal.refusals, locale),
    }));
}
