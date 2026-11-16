import { useLocale, useTranslations } from 'next-intl';
import { useMemo } from 'react';
import { UsageI18n } from './i18n';

// The words and the language a Usage page writes its labels in.
export function useUsageI18n(): UsageI18n {
  const t = useTranslations('settings.usage');
  const locale = useLocale();
  return useMemo(() => ({ t, locale }), [t, locale]);
}
