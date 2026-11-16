'use client';
import type { Locale as DateFnsLocale } from 'date-fns';
import { enUS, fr } from 'date-fns/locale';
import { useLocale } from 'next-intl';
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
