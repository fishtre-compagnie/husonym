import { Timestamp, timestampDate } from '@bufbuild/protobuf/wkt';
import { useDateFnsLocale } from '@/i18n/date-fns';
import {
  format,
  formatDistanceToNowStrict,
  Locale as DateFnsLocale,
} from 'date-fns';
import { useLocale, useTranslations } from 'next-intl';
import { useCallback, useEffect, useMemo, useState } from 'react';

/**
 * A span of time as a person reads it: 350 ms, 42 s, 7 min 42 s, 1 h 03 min. The units
 * read the same in every language the UI speaks; `locale` sets the decimal mark.
 */
function formatSpan(ms: number, locale = 'en'): string {
  if (!Number.isFinite(ms) || ms < 0) {
    return '–';
  }
  if (ms < 1000) {
    return `${Math.round(ms)} ms`;
  }
  const totalSeconds = Math.floor(ms / 1000);
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  if (hours > 0) {
    return `${hours} h ${String(minutes).padStart(2, '0')} min`;
  }
  if (minutes > 0) {
    return `${minutes} min ${String(seconds).padStart(2, '0')} s`;
  }
  if (ms < 10_000) {
    return `${(ms / 1000).toLocaleString(locale, {
      minimumFractionDigits: 1,
      maximumFractionDigits: 1,
    })} s`;
  }
  return `${seconds} s`;
}

export function toDate(ts?: Timestamp): Date | undefined {
  return ts ? timestampDate(ts) : undefined;
}

/** How long a run lasted, or has lasted so far when it has no end yet. */
export function runSpan(
  startedAt?: Timestamp,
  completedAt?: Timestamp,
  now: Date = new Date()
): number | undefined {
  const start = toDate(startedAt);
  if (!start) {
    return undefined;
  }
  const end = toDate(completedAt) ?? now;
  return end.getTime() - start.getTime();
}

/** How long ago a date was, in the words of `locale`: 5 minutes ago, il y a 5 minutes. */
function formatAgo(date?: Date, locale?: DateFnsLocale): string {
  if (!date) {
    return '–';
  }
  return formatDistanceToNowStrict(date, { addSuffix: true, locale });
}

type RunDateFormat =
  'short' | 'medium' | 'day' | 'dateTime' | 'dateTimeMs' | 'full';

export interface RunFormat {
  /** The UI language, for numbers formatted by hand. */
  locale: string;
  /** A span of time, with the UI language's decimal mark. */
  span(ms: number): string;
  /** How long ago, in the UI language. */
  ago(date?: Date): string;
  /** A date in one of the run pages' formats, written the UI language's way. */
  date(date: Date, pattern: RunDateFormat): string;
}

/** The run pages' formatters of durations and dates, in the language the UI is shown in. */
export function useRunFormat(): RunFormat {
  const locale = useLocale();
  const dateLocale = useDateFnsLocale();
  const t = useTranslations('runs.dateFormat');
  const span = useCallback((ms: number) => formatSpan(ms, locale), [locale]);
  const ago = useCallback(
    (date?: Date) => formatAgo(date, dateLocale),
    [dateLocale]
  );
  const date = useCallback(
    (d: Date, pattern: RunDateFormat) =>
      format(d, t(pattern), { locale: dateLocale }),
    [t, dateLocale]
  );
  return useMemo(
    () => ({ locale, span, ago, date }),
    [locale, span, ago, date]
  );
}

/** The current time, ticking every `everyMs` while `active`. */
export function useNow(active: boolean, everyMs = 1000): Date {
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    if (!active) {
      return;
    }
    const t = setInterval(() => setNow(new Date()), everyMs);
    return () => clearInterval(t);
  }, [active, everyMs]);
  return now;
}
