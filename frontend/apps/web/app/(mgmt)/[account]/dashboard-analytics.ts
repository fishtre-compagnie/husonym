/**
 * The figures of the home dashboard that are read from the list of runs: the runs of each
 * day by outcome, and when in the week the jobs run. Pure functions, so they are tested
 * without a page.
 */
import { JobRun } from '@husonym/sdk';
import { runOutcome, toDate } from './dashboard-data';

const DAY = 86_400_000;

/** The finished runs of a day of the period, by outcome. */
export interface DayOutcomes {
  // Midnight of the day, in the zone of the browser.
  day: Date;
  succeeded: number;
  failed: number;
  canceled: number;
}

/**
 * The finished runs of each of the last `days` days, today included, by outcome. A run
 * counts on the day it ended; one still running counts nowhere.
 */
export function dailyOutcomes(
  runs: JobRun[],
  days: number,
  now: Date
): DayOutcomes[] {
  const today = startOfDay(now);
  const out: DayOutcomes[] = Array.from({ length: days }, (_, i) => ({
    day: addDays(today, i - days + 1),
    succeeded: 0,
    failed: 0,
    canceled: 0,
  }));
  const first = out[0].day.getTime();
  for (const run of runs) {
    const outcome = runOutcome(run.status);
    if (
      outcome !== 'succeeded' &&
      outcome !== 'failed' &&
      outcome !== 'canceled'
    ) {
      continue;
    }
    const at = toDate(run.completedAt) ?? toDate(run.startedAt);
    if (!at || at.getTime() > now.getTime()) {
      continue;
    }
    const index = dayIndex(first, startOfDay(at));
    if (index < 0 || index >= days) {
      continue;
    }
    out[index][outcome]++;
  }
  return out;
}

/** What a job did in an hour of the week. */
interface SlotJob {
  jobId: string;
  runs: number;
  failed: number;
  // The mean duration of its runs of the slot that ended; undefined when none did.
  meanMs?: number;
}

/** An hour of a day of the week, and the runs that started in it. */
export interface HeatSlot {
  // 0 is Monday, 6 is Sunday.
  weekday: number;
  hour: number;
  runs: number;
  failed: number;
  // The jobs of the slot, the one that ran most first.
  jobs: SlotJob[];
}

/**
 * When the jobs run: the runs that started in the last `days` days, by day of the week
 * (Monday first) and hour, in the zone of the browser. Seven rows of 24 slots.
 */
export function weekHeatmap(
  runs: JobRun[],
  days: number,
  now: Date
): HeatSlot[][] {
  const from = startOfDay(addDays(now, -days + 1)).getTime();
  const acc = Array.from({ length: 7 }, () =>
    Array.from(
      { length: 24 },
      () =>
        new Map<
          string,
          { runs: number; failed: number; ms: number; ended: number }
        >()
    )
  );
  for (const run of runs) {
    const started = toDate(run.startedAt);
    if (
      !started ||
      started.getTime() < from ||
      started.getTime() > now.getTime()
    ) {
      continue;
    }
    const weekday = (started.getDay() + 6) % 7;
    const slot = acc[weekday][started.getHours()];
    const e = slot.get(run.jobId) ?? { runs: 0, failed: 0, ms: 0, ended: 0 };
    e.runs++;
    if (runOutcome(run.status) === 'failed') {
      e.failed++;
    }
    const ended = toDate(run.completedAt);
    if (ended && ended.getTime() >= started.getTime()) {
      e.ms += ended.getTime() - started.getTime();
      e.ended++;
    }
    slot.set(run.jobId, e);
  }
  return acc.map((row, weekday) =>
    row.map((slot, hour) => {
      const jobs = [...slot.entries()]
        .map(([jobId, e]): SlotJob => ({
          jobId,
          runs: e.runs,
          failed: e.failed,
          meanMs: e.ended > 0 ? e.ms / e.ended : undefined,
        }))
        .sort((a, b) => b.runs - a.runs || a.jobId.localeCompare(b.jobId));
      return {
        weekday,
        hour,
        runs: jobs.reduce((s, j) => s + j.runs, 0),
        failed: jobs.reduce((s, j) => s + j.failed, 0),
        jobs,
      };
    })
  );
}

/**
 * The step of a slot on a scale of 0 (no run) to `steps` (the busiest slot), so the
 * colors spread over what the account does rather than over a fixed count.
 */
export function heatLevel(runs: number, busiest: number, steps = 5): number {
  if (runs <= 0 || busiest <= 0) {
    return 0;
  }
  return Math.min(steps, 1 + Math.floor((runs / busiest) * (steps - 0.001)));
}

/** The change from `previous` to `current`, in percent, rounded; undefined without a base. */
export function percentChange(
  current: number,
  previous: number
): number | undefined {
  if (previous <= 0) {
    return undefined;
  }
  return Math.round(((current - previous) / previous) * 100);
}

/** The share of the runs that completed, the canceled ones aside, in percent with one decimal. */
export function successRate(
  runs: number,
  completed: number,
  canceled: number
): number | undefined {
  const counted = runs - canceled;
  if (counted <= 0) {
    return undefined;
  }
  return Math.round((completed / counted) * 1000) / 10;
}

/** A round top for an axis that holds `max`: 1, 2 or 5 times a power of ten. */
export function niceMax(max: number): number {
  if (max <= 0) {
    return 1;
  }
  const mag = Math.pow(10, Math.floor(Math.log10(max)));
  for (const step of [1, 2, 5, 10]) {
    if (step * mag >= max) {
      return step * mag;
    }
  }
  return 10 * mag;
}

function startOfDay(d: Date): Date {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate());
}

function addDays(d: Date, n: number): Date {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate() + n);
}

// The days between two midnights, read on the calendar so a day of 23 or 25 hours is one day.
function dayIndex(first: number, day: Date): number {
  return Math.round((day.getTime() - first) / DAY);
}
