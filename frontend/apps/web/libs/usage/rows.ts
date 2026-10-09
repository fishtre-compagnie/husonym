import { gateLabel } from '@/libs/license/license';
import type {
  GateRefusalCount,
  JobUsage,
  UsageErrorCount,
  UsageTotals,
} from '@husonym/sdk';
import { errorCategoryLabel } from './labels';
import {
  durationLabel,
  formatCount,
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
// read the most rows first). The note says, once, why some jobs show no row count.
export function jobsTable(
  jobs: readonly JobUsage[],
  accountName: string
): { rows: JobRow[]; note: string | undefined } {
  return {
    rows: jobs.map((job) => ({
      id: job.jobId,
      name: job.jobName,
      href: `/${accountName}/jobs/${job.jobId}/usage`,
      rowsRead: rowsLabel(job.kind, job.totals?.rowsRead ?? ZERO),
      runs: formatCount(job.totals?.runs ?? ZERO),
      success: successRateLabel(job.totals),
      duration: durationLabel(job.durationMedianSeconds),
    })),
    note: jobs.map((job) => rowsHint(job.kind)).find((hint) => hint),
  };
}

// What the table of jobs says when it has no row. Jobs deleted since are not listed
// while their runs stay counted: runs without a job are not "no job ran".
export function jobsEmptyLine(totals: UsageTotals | undefined): string {
  return noRunLine(totals) === undefined
    ? 'The jobs that ran in this period no longer exist.'
    : 'No job ran in this period.';
}

// What the errors say when there is none.
export function errorsEmptyLine(totals: UsageTotals | undefined): string {
  return noRunLine(totals) ?? 'Every run of this period completed.';
}

// A row of a table of counts: what is counted, and how many.
interface CountRow {
  key: string;
  label: string;
  count: string;
}

// The runs that did not complete, by category, in the order the API gives them (the
// category with the most runs first).
export function errorRows(errors: readonly UsageErrorCount[]): CountRow[] {
  return errors.map((error) => ({
    key: String(error.category),
    label: errorCategoryLabel(error.category),
    count: formatCount(error.runs),
  }));
}

// What the license refused, by gate. A gate that refused nothing is no row.
export function refusalRows(refusals: readonly GateRefusalCount[]): CountRow[] {
  return refusals
    .filter((refusal) => refusal.refusals > ZERO)
    .map((refusal) => ({
      key: refusal.gate,
      label: gateLabel(refusal.gate),
      count: formatCount(refusal.refusals),
    }));
}
