'use client';
import { Segmented } from '@/components/ds';
import { useTranslations } from 'next-intl';
import { ReactElement } from 'react';
import { JobFilter, JobRow, matchesFilter } from './job-rows';

const FILTERS: JobFilter[] = ['all', 'active', 'paused', 'failing'];

interface Props {
  rows: JobRow[];
  value: JobFilter;
  onChange(value: JobFilter): void;
}

// JobFilterTabs narrows the list to the jobs in a state, each choice with how many jobs it
// keeps.
export function JobFilterTabs(props: Props): ReactElement {
  const { rows, value, onChange } = props;
  const t = useTranslations('jobs.list.filters');
  return (
    <Segmented
      ariaLabel={t('ariaLabel')}
      value={value}
      onChange={onChange}
      className="flex-wrap"
      options={FILTERS.map((f) => ({
        value: f,
        label: (
          <span className="flex items-center gap-2">
            {t(f)}
            <span className="text-xs text-muted-foreground tabular-nums">
              {rows.filter((r) => matchesFilter(r.state, f)).length}
            </span>
          </span>
        ),
      }))}
    />
  );
}
