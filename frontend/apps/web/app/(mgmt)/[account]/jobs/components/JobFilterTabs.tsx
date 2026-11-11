'use client';
import { Segmented } from '@/components/ds';
import { ReactElement } from 'react';
import { JobFilter, JobRow, matchesFilter } from './job-rows';

const FILTERS: { value: JobFilter; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'active', label: 'Active' },
  { value: 'paused', label: 'Paused' },
  { value: 'failing', label: 'Failing' },
];

interface Props {
  rows: JobRow[];
  value: JobFilter;
  onChange(value: JobFilter): void;
}

// JobFilterTabs narrows the list to the jobs in a state, each choice with how many jobs it
// keeps.
export function JobFilterTabs(props: Props): ReactElement {
  const { rows, value, onChange } = props;
  return (
    <Segmented
      ariaLabel="Filter jobs by state"
      value={value}
      onChange={onChange}
      className="flex-wrap"
      options={FILTERS.map((f) => ({
        value: f.value,
        label: (
          <span className="flex items-center gap-2">
            {f.label}
            <span className="text-xs text-muted-foreground tabular-nums">
              {rows.filter((r) => matchesFilter(r.state, f.value)).length}
            </span>
          </span>
        ),
      }))}
    />
  );
}
