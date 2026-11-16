'use client';
import type { Locale as DateFnsLocale } from 'date-fns';
import { enUS, fr } from 'date-fns/locale';
import { useLocale } from 'next-intl';
import { useCallback } from 'react';
import { formatDateTime } from '@/util/util';
import type { Locale } from './config';

const DATE_FNS_LOCALES: Record<Locale, DateFnsLocale> = { en: enUS, fr };

/** The date-fns locale of a UI language. */
function dateFnsLocale(locale: Locale): DateFnsLocale {
  return DATE_FNS_LOCALES[locale];
}

/**
 * The date-fns locale of the language the UI is shown in, to pass as `locale` to
 * `format`, `formatDistance` and the like. Dates stay in the browser's time zone.
 */
export function useDateFnsLocale(): DateFnsLocale {
  return dateFnsLocale(useLocale());
}

/**
 * formatDateTime bound to the language the UI is shown in: 04/16/2024 02:30:00 PM in
 * English, 16/04/2024 14:30:00 in French.
 */
export function useFormatDateTime(): (
  date?: string | Date | number,
  is24Hour?: boolean
) => string | undefined {
  const locale = useLocale();
  return useCallback(
    (date?: string | Date | number, is24Hour?: boolean) =>
      formatDateTime(date, is24Hour, locale),
    [locale]
  );
}
