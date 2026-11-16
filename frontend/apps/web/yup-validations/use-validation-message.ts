import { useLocale, useTranslations } from 'next-intl';
import { useCallback } from 'react';
import { matchValidationMessage } from './validation-message';

type Translate = (key: string, values?: Record<string, string>) => string;

/** Turns a validation message into the words of the user's language. */
export function useValidationMessage(): (message: string) => string {
  const locale = useLocale();
  const t = useTranslations('validation') as unknown as Translate;
  return useCallback(
    (message: string) => {
      // English is the schemas' own language: their text is shown untouched.
      if (locale === 'en') {
        return message;
      }
      const match = matchValidationMessage(message);
      return match ? t(match.key, match.values) : message;
    },
    [locale, t]
  );
}
