import { englishTranslator } from '@/i18n/english';
import enJobs from '@/messages/en/jobs.json';
import { JobMappingChange, JobMappingChangeKind } from '@husonym/sdk';
import type { useTranslations } from 'next-intl';

// The words of a kind of change, from useTranslations('jobs.review.changeKinds').
export type ChangeKindTranslator = ReturnType<
  typeof useTranslations<'jobs.review.changeKinds'>
>;

const ENGLISH_CHANGE_KINDS = englishTranslator(
  enJobs.jobs.review.changeKinds
) as unknown as ChangeKindTranslator;

export function columnName(c: JobMappingChange): string {
  return `${c.column?.schema}.${c.column?.table}.${c.column?.column}`;
}

export function changeLabel(
  c: JobMappingChange,
  t: ChangeKindTranslator = ENGLISH_CHANGE_KINDS
): string {
  switch (c.kind) {
    case JobMappingChangeKind.ADDED:
      return t('added');
    case JobMappingChangeKind.REMOVED:
      return t('removed');
    case JobMappingChangeKind.TYPE_CHANGED:
      return t('typeChanged', { from: c.previousDataType, to: c.dataType });
    default:
      return t('changed');
  }
}

// Whether the run left the column in clear.
export function isPassthrough(c: JobMappingChange): boolean {
  return c.transformer?.config?.config.case === 'passthroughConfig';
}

// The order in which changes are worth looking at: a column that reads as personal data and
// ships in clear, then a column whose type moved under its mapping, then any other column shipped
// in clear, then the columns the run anonymized, then the removals, which change nothing in
// what leaves the source.
export function urgency(c: JobMappingChange): number {
  if (c.kind === JobMappingChangeKind.ADDED && isPassthrough(c)) {
    return c.piiCategory ? 0 : 2;
  }
  if (c.kind === JobMappingChangeKind.TYPE_CHANGED) {
    return 1;
  }
  if (c.kind === JobMappingChangeKind.ADDED) {
    return 3;
  }
  return 4;
}
