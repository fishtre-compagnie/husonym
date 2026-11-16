// The one place that says which languages the UI speaks, which one it falls back to,
// and where the user's choice is kept for the server to read before any API call.

export const LOCALES = ['en', 'fr'] as const;
export type Locale = (typeof LOCALES)[number];

// The instance's language: what a user who has not chosen one sees.
const DEFAULT_LOCALE: Locale = 'en';

// Mirrors the preference stored with the user: absent when the user follows the default.
export const LOCALE_COOKIE = 'HUSONYM_LOCALE';

// A user's preference: a supported language, or '' to follow the instance's default.
export type LocalePreference = Locale | '';

function isLocale(value: unknown): value is Locale {
  return (
    typeof value === 'string' && (LOCALES as readonly string[]).includes(value)
  );
}

/** The preference a raw value stands for: anything unknown follows the default. */
export function toPreference(
  value: string | undefined | null
): LocalePreference {
  return isLocale(value) ? value : '';
}

/** The language a preference is shown in. */
export function resolveLocale(preference: LocalePreference): Locale {
  return preference || DEFAULT_LOCALE;
}
