import { DEFAULT_CRON_STRING } from '../schedule';

// The phrases a schedule is said with: keys of the `jobs.schedule` messages.
type ScheduleKey =
  | 'manual'
  | 'hourly'
  | 'daily'
  | 'weekly'
  | 'monthly'
  | 'everyMinutes'
  | 'everyHours'
  | 'dailyAt'
  | 'weekdaysAt'
  | 'everyDayAt'
  | 'monthlyAt';

export type ScheduleLabel =
  // The cron expression itself, which no phrase could tell.
  | { isCron: true; text: string }
  // A phrase of the messages, with its values.
  | {
      isCron: false;
      key: ScheduleKey;
      values?: Record<string, string | number>;
    };

const MACROS: Record<string, ScheduleKey> = {
  '@hourly': 'hourly',
  '@daily': 'daily',
  '@midnight': 'daily',
  '@weekly': 'weekly',
  '@monthly': 'monthly',
};

function isNumber(s: string): boolean {
  return /^\d+$/.test(s);
}

function time(hour: string, minute: string): string {
  return `${hour.padStart(2, '0')}:${minute.padStart(2, '0')}`;
}

function phrase(
  key: ScheduleKey,
  values?: Record<string, string | number>
): ScheduleLabel {
  return { isCron: false, key, values };
}

// scheduleLabel says a job's schedule in words, for the common shapes of a cron
// expression; any other shape is shown as it is. A job without a schedule is stored with
// DEFAULT_CRON_STRING: it only runs when started by hand. Schedules run in UTC.
export function scheduleLabel(cron: string | undefined): ScheduleLabel {
  const expr = (cron ?? '').trim();
  if (!expr || expr === DEFAULT_CRON_STRING) {
    return phrase('manual');
  }
  const macro = MACROS[expr.toLowerCase()];
  if (macro) {
    return phrase(macro);
  }
  const cronText: ScheduleLabel = { isCron: true, text: expr };
  const parts = expr.split(/\s+/);
  if (parts.length !== 5) {
    return cronText;
  }
  const [min, hour, dom, month, dow] = parts;
  if (month !== '*') {
    return cronText;
  }
  const everyMin = /^\*\/(\d+)$/.exec(min);
  if (everyMin && hour === '*' && dom === '*' && dow === '*') {
    return phrase('everyMinutes', { count: everyMin[1] });
  }
  if (isNumber(min) && hour === '*' && dom === '*' && dow === '*') {
    return phrase('hourly');
  }
  const everyHour = /^\*\/(\d+)$/.exec(hour);
  if (isNumber(min) && everyHour && dom === '*' && dow === '*') {
    return phrase('everyHours', { count: everyHour[1] });
  }
  if (!isNumber(min) || !isNumber(hour)) {
    return cronText;
  }
  const at = time(hour, min);
  if (dom === '*' && dow === '*') {
    return phrase('dailyAt', { time: at });
  }
  if (dom === '*' && dow === '1-5') {
    return phrase('weekdaysAt', { time: at });
  }
  if (dom === '*' && isNumber(dow) && Number(dow) <= 7) {
    return phrase('everyDayAt', { day: String(Number(dow) % 7), time: at });
  }
  if (isNumber(dom) && dow === '*') {
    return phrase('monthlyAt', { day: dom, time: at });
  }
  return cronText;
}
