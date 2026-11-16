import { englishTranslator } from '@/i18n/english';
import enSettings from '@/messages/en/settings.json';
import type { useTranslations } from 'next-intl';

// What the Usage pages write their words and numbers with: the translator of
// settings.usage and the language of the UI.
type UsageTranslator = ReturnType<typeof useTranslations<'settings.usage'>>;

export interface UsageI18n {
  t: UsageTranslator;
  locale: string;
}

// The English words, for a caller that has no language to give (a test, a page not
// translated yet): the labels read as they always did.
export const ENGLISH_USAGE_I18N: UsageI18n = {
  t: englishTranslator(enSettings.settings.usage) as unknown as UsageTranslator,
  locale: 'en',
};
