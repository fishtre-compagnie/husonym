import { englishTranslator } from '@/i18n/english';
import enSettings from '@/messages/en/settings.json';
import type { useTranslations } from 'next-intl';

// What the license helpers write their words with: the translator of settings.license,
// from useTranslations('settings.license') in a component.
export type LicenseTranslator = ReturnType<
  typeof useTranslations<'settings.license'>
>;

// The English words, for a caller that gives no translator (a test): the labels read
// as they always did.
export const ENGLISH_LICENSE_T = englishTranslator(
  enSettings.settings.license
) as unknown as LicenseTranslator;
