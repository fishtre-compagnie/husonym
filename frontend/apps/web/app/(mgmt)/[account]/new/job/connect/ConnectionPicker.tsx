'use client';
import Spinner from '@/components/Spinner';
import { SelectCard } from '@/components/progress-steps/Wizard';
import { cn } from '@/libs/utils';
import { Connection } from '@husonym/sdk';
import { PlusIcon } from '@radix-ui/react-icons';
import { ReactElement, ReactNode } from 'react';

interface Props {
  postgres?: Connection[];
  mysql?: Connection[];
  s3?: Connection[];
  openai?: Connection[];
  mongodb?: Connection[];
  gcpcs?: Connection[];
  dynamodb?: Connection[];
  mssql?: Connection[];

  value: string | undefined;
  /** Called with a connection id, or with `newConnectionValue`. */
  onChange(value: string): void;
  /** Provide a value to offer creating a new connection. */
  newConnectionValue?: string;
  /** Connections shown but not pickable, with the reason why. */
  disabledReasons?: Record<string, string>;
  /** The selected connection is being checked. */
  isChecking?: boolean;
  /** Shown under the selected card: the result of its check. */
  selectedDetail?: ReactNode;
  ariaLabel: string;
  emptyLabel?: string;
}

const ENGINES = [
  ['postgres', 'PG', 'PostgreSQL'],
  ['mysql', 'MySQL', 'MySQL'],
  ['mssql', 'MSSQL', 'SQL Server'],
  ['mongodb', 'Mongo', 'MongoDB'],
  ['dynamodb', 'DDB', 'DynamoDB'],
  ['s3', 'S3', 'AWS S3'],
  ['gcpcs', 'GCS', 'GCP Cloud Storage'],
  ['openai', 'AI', 'OpenAI-compatible'],
] as const;

/**
 * The connections a step can pick from, as selectable cards (a radio group).
 * Same groups as ConnectionSelectContent, so a page swaps one for the other
 * without changing what it does with the value.
 */
export default function ConnectionPicker(props: Props): ReactElement {
  const {
    value,
    onChange,
    newConnectionValue,
    disabledReasons = {},
    isChecking,
    selectedDetail,
    ariaLabel,
    emptyLabel = 'No connection of a suitable type yet.',
  } = props;

  const items = ENGINES.flatMap(([key, short, long]) =>
    (props[key] ?? []).map((connection) => ({ connection, short, long }))
  );

  return (
    <div className="flex flex-col gap-2.5">
      <div
        role="radiogroup"
        aria-label={ariaLabel}
        className="grid grid-cols-[repeat(auto-fill,minmax(260px,1fr))] gap-2.5"
      >
        {items.map(({ connection, short, long }) => {
          const selected = connection.id === value;
          const disabledReason = disabledReasons[connection.id];
          return (
            <SelectCard
              key={connection.id}
              selected={selected}
              disabled={!!disabledReason}
              onClick={() => onChange(connection.id)}
              className="items-center gap-3.5 px-4 py-3.5"
            >
              <span
                aria-hidden="true"
                className="grid size-10 shrink-0 place-items-center rounded-[10px] bg-surface-2 font-mono text-[11px] text-link"
              >
                {short}
              </span>
              <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                <span className="truncate font-semibold">
                  {connection.name}
                </span>
                <span className="truncate text-xs text-muted-foreground">
                  {disabledReason ?? long}
                </span>
              </span>
              {selected && isChecking && (
                <span className="inline-flex items-center gap-1.5 rounded-full bg-surface-2 px-2 py-1 text-xs text-muted-foreground">
                  <Spinner className="size-3" />
                  Checking
                </span>
              )}
            </SelectCard>
          );
        })}
        {newConnectionValue && (
          <button
            type="button"
            onClick={() => onChange(newConnectionValue)}
            className={cn(
              'flex cursor-pointer items-center gap-3.5 rounded-[14px] border border-dashed border-border px-4 py-3.5 text-left text-link transition-colors hover:border-quiet hover:bg-hover',
              'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-link'
            )}
          >
            <span
              aria-hidden="true"
              className="grid size-10 shrink-0 place-items-center rounded-[10px] bg-brand-soft"
            >
              <PlusIcon className="size-4" />
            </span>
            <span className="font-medium">New connection</span>
          </button>
        )}
      </div>
      {items.length === 0 && (
        <p className="m-0 text-[13px] text-muted-foreground">{emptyLabel}</p>
      )}
      {value && selectedDetail && <div className="flex">{selectedDetail}</div>}
    </div>
  );
}
