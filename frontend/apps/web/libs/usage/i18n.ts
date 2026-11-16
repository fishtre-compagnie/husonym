import type { useTranslations } from 'next-intl';
import enSettings from '@/messages/en/settings.json';

// What the Usage pages write their words and numbers with: the translator of
// settings.usage and the language of the UI.
type UsageTranslator = ReturnType<typeof useTranslations<'settings.usage'>>;

export interface UsageI18n {
  t: UsageTranslator;
  locale: string;
}

type Values = Record<string, string | number>;

// The English message of a key of settings.usage, such as "range.withZone".
function englishMessage(key: string): string {
  let node: unknown = enSettings.settings.usage;
  for (const part of key.split('.')) {
    node = (node as Record<string, unknown> | undefined)?.[part];
  }
  return typeof node === 'string' ? node : key;
}

// Fills a message with its values. The messages of settings.usage hold nothing but
// plain arguments and plurals of one and other, which is all this reads.
function fill(message: string, values: Values = {}): string {
  const plurals = message.replace(
    /\{(\w+), plural, one \{([^{}]*)\} other \{([^{}]*)\}\}/g,
    (_, name: string, one: string, other: string) => {
      const count = values[name];
      return (count === 1 ? one : other).replace(/#/g, String(count));
    }
  );
  return plurals.replace(/\{(\w+)\}/g, (whole, name: string) =>
    name in values ? String(values[name]) : whole
  );
}

// The English words, for a caller that has no language to give (a test, a page not
// translated yet): the labels read as they always did. It is made without next-intl,
// which is not loaded where there is no React to render.
export const ENGLISH_USAGE_I18N: UsageI18n = {
  t: ((key: string, values?: Values) =>
    fill(englishMessage(key), values)) as unknown as UsageTranslator,
  locale: 'en',
};
