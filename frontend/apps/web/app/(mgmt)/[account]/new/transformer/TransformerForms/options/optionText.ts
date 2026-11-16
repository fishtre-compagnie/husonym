'use client';
import { useTranslations } from 'next-intl';

type OptionTextKind = 'label' | 'description';

// The text of a transformer option in the UI language. The registry keeps the English text;
// the translations live under `connections.transformerOptions.<config message>.<field>`, and an
// option without one keeps its English text.
export type OptionText = (
  schemaName: string,
  field: string,
  kind: OptionTextKind,
  fallback: string
) => string;

export function useOptionText(): OptionText {
  const t = useTranslations('connections.transformerOptions');
  return (schemaName, field, kind, fallback) => {
    const key = `${schemaName}.${field}.${kind}` as Parameters<typeof t>[0];
    return t.has(key) ? t(key) : fallback;
  };
}
