// The schedule a job is stored with when it was given none: a job always has one.
export const DEFAULT_CRON_STRING = '0 0 1 1 *';

// The schedule a copy of a job starts with. The default cron of an unscheduled job is not a
// schedule: copying it would give the copy one, which a license without scheduling refuses.
export function cronToClone(cron: string | undefined): string | undefined {
  return cron === DEFAULT_CRON_STRING ? '' : cron;
}
