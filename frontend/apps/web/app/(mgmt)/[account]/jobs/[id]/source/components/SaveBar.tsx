import Spinner from '@/components/Spinner';
import { StatusDot } from '@/components/ds';
import { Button } from '@/components/ui/button';
import { useTranslations } from 'next-intl';
import { ReactElement } from 'react';

interface Props {
  isDirty: boolean;
  isSubmitting?: boolean;
  label?: string;
}

// The bar that saves the page's form, held at the bottom of the screen while the
// long mapping scrolls. It tells whether something is still to save; the button
// stays enabled either way, as the Update button it replaces did.
export default function SaveBar({
  isDirty,
  isSubmitting,
  label,
}: Props): ReactElement {
  const t = useTranslations('jobs.source.saveBar');
  return (
    <div className="pointer-events-none sticky bottom-4 z-20 flex justify-end">
      <div
        role="region"
        aria-label={t('region')}
        className="pointer-events-auto flex items-center gap-3 rounded-2xl border border-border bg-card py-2 pl-4 pr-2 shadow-pop"
      >
        <span
          className="flex items-center gap-2 text-sm font-medium"
          aria-live="polite"
        >
          <StatusDot tone={isDirty ? 'warn' : 'ok'} />
          {isDirty ? t('unsaved') : t('saved')}
        </span>
        <Button type="submit" disabled={isSubmitting}>
          {isSubmitting && <Spinner />}
          {label ?? t('save')}
        </Button>
      </div>
    </div>
  );
}
