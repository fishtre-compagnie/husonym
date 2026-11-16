import { getRequestConfig } from 'next-intl/server';
import { cookies } from 'next/headers';
import { loadMessages } from '../messages';
import { LOCALE_COOKIE, resolveLocale, toPreference } from './config';

// Without a locale in the URL, the language comes from the cookie that mirrors the
// user's preference; a missing or unknown value falls back to the default.
export default getRequestConfig(async () => {
  const store = await cookies();
  const locale = resolveLocale(toPreference(store.get(LOCALE_COOKIE)?.value));
  return {
    locale,
    messages: await loadMessages(locale),
  };
});
