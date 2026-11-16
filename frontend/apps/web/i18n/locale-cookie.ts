import { LOCALE_COOKIE, LocalePreference, toPreference } from './config';

const ONE_YEAR = 60 * 60 * 24 * 365;

/** The preference the cookie holds, in the browser; '' when there is none. */
export function readLocaleCookie(): LocalePreference {
  const entry = document.cookie
    .split('; ')
    .find((c) => c.startsWith(`${LOCALE_COOKIE}=`));
  return toPreference(entry?.slice(LOCALE_COOKIE.length + 1));
}

/** Mirrors a preference in the cookie the server reads; '' removes it. */
export function writeLocaleCookie(preference: LocalePreference): void {
  document.cookie = preference
    ? `${LOCALE_COOKIE}=${preference}; path=/; max-age=${ONE_YEAR}; samesite=lax`
    : `${LOCALE_COOKIE}=; path=/; max-age=0; samesite=lax`;
}
