/**
 * Small charts and pieces of the home dashboard. Every color is a token, so they follow
 * both themes.
 */
import { useTranslations } from 'next-intl';
import { ReactElement, ReactNode } from 'react';

/** A bar for work whose progress is not known: it moves, it does not fill. */
export function LiveBar(props: { label: string }): ReactElement {
  const t = useTranslations('dashboard.home.live');
  return (
    <div
      role="progressbar"
      aria-label={props.label}
      aria-valuetext={t('inProgress')}
      className="h-2 overflow-hidden rounded-full bg-surface-2"
    >
      <div className="progress-flow h-full w-full rounded-full bg-primary/60" />
    </div>
  );
}

/** What a list says when it has nothing to show. */
export function EmptyRow(props: { children: ReactNode }): ReactElement {
  return (
    <p className="m-0 px-5 py-6 text-sm text-muted-foreground">
      {props.children}
    </p>
  );
}

/** A link that reads as a row of a Panel, with a visible focus. */
export const rowLinkFocus =
  'outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring';
