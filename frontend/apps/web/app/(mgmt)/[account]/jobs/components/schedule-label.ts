import { DEFAULT_CRON_STRING } from '../schedule';

export interface ScheduleLabel {
  // What the list says of the schedule.
  text: string;
  // Whether the text is the cron expression itself, which no phrase could tell.
  isCron: boolean;
}

const DAYS = [
  'Sunday',
  'Monday',
  'Tuesday',
  'Wednesday',
  'Thursday',
  'Friday',
  'Saturday',
];

const MACROS: Record<string, string> = {
  '@hourly': 'Hourly',
  '@daily': 'Daily at 00:00 UTC',
  '@midnight': 'Daily at 00:00 UTC',
  '@weekly': 'Every Sunday at 00:00 UTC',
  '@monthly': 'Monthly on day 1 at 00:00 UTC',
};

function isNumber(s: string): boolean {
  return /^\d+$/.test(s);
}

function time(hour: string, minute: string): string {
  return `${hour.padStart(2, '0')}:${minute.padStart(2, '0')} UTC`;
}

// scheduleLabel says a job's schedule in words, for the common shapes of a cron
// expression; any other shape is shown as it is. A job without a schedule is stored with
// DEFAULT_CRON_STRING: it only runs when started by hand. Schedules run in UTC.
export function scheduleLabel(cron: string | undefined): ScheduleLabel {
  const expr = (cron ?? '').trim();
  if (!expr || expr === DEFAULT_CRON_STRING) {
    return { text: 'Manual', isCron: false };
  }
  const macro = MACROS[expr.toLowerCase()];
  if (macro) {
    return { text: macro, isCron: false };
  }
  const parts = expr.split(/\s+/);
  if (parts.length !== 5) {
    return { text: expr, isCron: true };
  }
  const [min, hour, dom, month, dow] = parts;
  if (month !== '*') {
    return { text: expr, isCron: true };
  }
  const everyMin = /^\*\/(\d+)$/.exec(min);
  if (everyMin && hour === '*' && dom === '*' && dow === '*') {
    return { text: `Every ${everyMin[1]} min`, isCron: false };
  }
  if (isNumber(min) && hour === '*' && dom === '*' && dow === '*') {
    return { text: 'Hourly', isCron: false };
  }
  const everyHour = /^\*\/(\d+)$/.exec(hour);
  if (isNumber(min) && everyHour && dom === '*' && dow === '*') {
    return { text: `Every ${everyHour[1]} h`, isCron: false };
  }
  if (!isNumber(min) || !isNumber(hour)) {
    return { text: expr, isCron: true };
  }
  const at = time(hour, min);
  if (dom === '*' && dow === '*') {
    return { text: `Daily at ${at}`, isCron: false };
  }
  if (dom === '*' && dow === '1-5') {
    return { text: `Weekdays at ${at}`, isCron: false };
  }
  if (dom === '*' && isNumber(dow) && Number(dow) <= 7) {
    return { text: `Every ${DAYS[Number(dow) % 7]} at ${at}`, isCron: false };
  }
  if (isNumber(dom) && dow === '*') {
    return { text: `Monthly on day ${dom} at ${at}`, isCron: false };
  }
  return { text: expr, isCron: true };
}
