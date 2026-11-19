import { ENGLISH_USAGE_I18N, UsageI18n } from './i18n';

// The periods the Usage pages offer, in the order they are listed.
export const USAGE_PERIODS = [
  'last-7-days',
  'last-30-days',
  'last-90-days',
  'this-month',
  'last-month',
] as const;

export type UsagePeriod = (typeof USAGE_PERIODS)[number];

export const DEFAULT_USAGE_PERIOD: UsagePeriod = 'last-30-days';

export function periodLabel(
  period: UsagePeriod,
  i18n: UsageI18n = ENGLISH_USAGE_I18N
): string {
  return i18n.t(`periods.${period}`);
}

export function isUsagePeriod(value: string): value is UsagePeriod {
  return USAGE_PERIODS.some((period) => period === value);
}

// A day of the calendar, in the shape of the Date of the API. It is of no zone by
// itself: the zone it is read in travels next to it.
interface CalendarDay {
  year: number;
  month: number;
  day: number;
}

// The days a page asks for, both included.
interface UsageRange {
  fromDay: CalendarDay;
  toDay: CalendarDay;
}

// The zone of the browser, by its IANA name: the days of a page are the viewer's.
export function browserTimeZone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone;
}

// The days of a period for someone whose clock reads `now` in `timeZone`. Only the
// day it is there is read from the clock: the rest is arithmetic on the calendar, so
// that a day of 23 or 25 hours is still one day.
export function periodRange(
  period: UsagePeriod,
  now: Date,
  timeZone: string
): UsageRange {
  const today = dayIn(timeZone, now);
  switch (period) {
    case 'last-7-days':
      return { fromDay: daysBefore(today, 6), toDay: today };
    case 'last-30-days':
      return { fromDay: daysBefore(today, 29), toDay: today };
    case 'last-90-days':
      return { fromDay: daysBefore(today, 89), toDay: today };
    case 'this-month':
      return { fromDay: { ...today, day: 1 }, toDay: today };
    case 'last-month': {
      // The day before the first of this month is the last of the month before.
      const last = daysBefore({ ...today, day: 1 }, 1);
      return { fromDay: { ...last, day: 1 }, toDay: last };
    }
  }
}

// The range of as many days just before `range`: the period a figure is compared to.
export function previousRange(range: UsageRange): UsageRange {
  const days = daysBetween(range.fromDay, range.toDay) + 1;
  return {
    fromDay: daysBefore(range.fromDay, days),
    toDay: daysBefore(range.fromDay, 1),
  };
}

// The days from `from` to `to`, read on the UTC calendar.
function daysBetween(from: CalendarDay, to: CalendarDay): number {
  return Math.round(
    (Date.UTC(to.year, to.month - 1, to.day) -
      Date.UTC(from.year, from.month - 1, from.day)) /
      86_400_000
  );
}

// The day it is in a zone at a moment.
function dayIn(timeZone: string, at: Date): CalendarDay {
  const parts = new Intl.DateTimeFormat('en-US', {
    timeZone,
    year: 'numeric',
    month: 'numeric',
    day: 'numeric',
  }).formatToParts(at);
  const part = (type: Intl.DateTimeFormatPartTypes): number =>
    Number(parts.find((p) => p.type === type)?.value);
  return { year: part('year'), month: part('month'), day: part('day') };
}

// Calendar days have no hour: they are counted on the UTC calendar, which has no day
// of 23 or 25 hours.
function daysBefore(day: CalendarDay, count: number): CalendarDay {
  const at = new Date(Date.UTC(day.year, day.month - 1, day.day - count));
  return {
    year: at.getUTCFullYear(),
    month: at.getUTCMonth() + 1,
    day: at.getUTCDate(),
  };
}

// A day as an axis or a row names it, in the language given: "Oct 9", "9 oct.". The
// day is of no zone: it is written as the UTC calendar reads it.
export function dayLabel(
  day: Omit<CalendarDay, 'year'>,
  locale: string = ENGLISH_USAGE_I18N.locale
): string {
  // Any year will do to place a month and a day; a leap one keeps February 29.
  return new Intl.DateTimeFormat(locale, {
    month: 'short',
    day: 'numeric',
    timeZone: 'UTC',
  }).format(new Date(Date.UTC(2024, day.month - 1, day.day)));
}

// The days of a range and the zone they were counted in, as the line under the title
// says them: "Sep 10 – Oct 9, 2026 · days in Europe/Paris". The zone is the one the
// API says it used, which is not always the one that was asked: without it, the days
// are said alone.
export function rangeLabel(
  range: UsageRange,
  timeZone: string | undefined,
  i18n: UsageI18n = ENGLISH_USAGE_I18N
): string {
  const days = daysLabel(range, i18n);
  return timeZone ? i18n.t('range.withZone', { days, timeZone }) : days;
}

function daysLabel({ fromDay, toDay }: UsageRange, i18n: UsageI18n): string {
  const { t, locale } = i18n;
  // The years are given as text: a number would have its digits grouped.
  const to = dayLabel(toDay, locale);
  if (fromDay.year !== toDay.year) {
    return t('range.otherYears', {
      from: dayLabel(fromDay, locale),
      fromYear: String(fromDay.year),
      to,
      toYear: String(toDay.year),
    });
  }
  if (fromDay.month === toDay.month && fromDay.day === toDay.day) {
    return t('range.sameDay', { day: to, year: String(toDay.year) });
  }
  return t('range.sameYear', {
    from: dayLabel(fromDay, locale),
    to,
    year: String(toDay.year),
  });
}
