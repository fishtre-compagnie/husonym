'use client';
import { Panel } from '@/components/ds';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip';
import { cn } from '@/libs/utils';
import {
  MagnifyingGlassIcon,
  Pencil1Icon,
  PlusIcon,
  ResetIcon,
} from '@radix-ui/react-icons';
import { ReactElement, useMemo, useState } from 'react';
import IndeterminateCheckbox from '../../JobMappingTable/IndeterminateCheckbox';
import { SubsetTableRow } from './Columns';

interface Props {
  data: SubsetTableRow[];
  onEdit(rowIndex: number, schema: string, table: string): void;
  onReset(rowIndex: number, schema: string, table: string): void;
  hasLocalChange(rowIndex: number, schema: string, table: string): boolean;
  onBulkEdit(data: SubsetTableRow[], onClearSelection: () => void): void;
}

type Filter = 'all' | 'filtered' | 'root';

// So many tables are listed at first; the rest come on demand.
const PAGE = 100;

const GRID =
  'grid grid-cols-[16px_minmax(0,1fr)_auto] gap-x-3 gap-y-2 md:grid-cols-[16px_minmax(220px,1fr)_64px_minmax(280px,2fr)_76px] md:items-center md:gap-x-5';

/**
 * Which rows of each table the job copies: one line per table, its WHERE clause read
 * in full. The list flows with the page: no box with a scroll of its own.
 */
export default function SubsetTable(props: Props): ReactElement {
  const { data, onEdit, onReset, hasLocalChange, onBulkEdit } = props;

  const [filter, setFilter] = useState<Filter>('all');
  const [search, setSearch] = useState('');
  const [limit, setLimit] = useState(PAGE);
  const [selected, setSelected] = useState<Set<string>>(new Set());

  const rows = useMemo(
    () =>
      data
        .map((row, index) => ({
          row,
          index,
          key: `${row.schema}.${row.table}`,
        }))
        .sort((a, b) => a.key.localeCompare(b.key)),
    [data]
  );
  const counts: Record<Filter, number> = {
    all: rows.length,
    filtered: rows.filter((r) => !!r.row.where).length,
    root: rows.filter((r) => r.row.isRootTable).length,
  };

  const needle = search.trim().toLowerCase();
  const shown = rows.filter(
    (r) =>
      (filter === 'all' ||
        (filter === 'filtered' && !!r.row.where) ||
        (filter === 'root' && r.row.isRootTable)) &&
      (!needle || r.key.toLowerCase().includes(needle))
  );
  const page = shown.slice(0, limit);
  const selectedShown = page.filter((r) => selected.has(r.key)).length;

  function toggle(keys: string[], on: boolean): void {
    setSelected((prev) => {
      const next = new Set(prev);
      keys.forEach((k) => (on ? next.add(k) : next.delete(k)));
      return next;
    });
  }

  const selectedRows = rows.filter((r) => selected.has(r.key));

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-end gap-3 border-b border-line-soft">
        <div
          role="tablist"
          aria-label="Tables"
          className="-mb-px flex flex-wrap gap-0.5"
        >
          {(
            [
              ['all', 'All tables'],
              ['filtered', 'With a filter'],
              ['root', 'Root tables'],
            ] as [Filter, string][]
          ).map(([value, label]) => (
            <button
              key={value}
              type="button"
              role="tab"
              aria-selected={filter === value}
              onClick={() => {
                setFilter(value);
                setLimit(PAGE);
              }}
              className={cn(
                'flex cursor-pointer items-center gap-2 border-b-2 px-3 py-2.5 text-sm transition-colors',
                filter === value
                  ? 'border-primary text-hi'
                  : 'border-transparent text-muted-foreground hover:text-foreground'
              )}
            >
              {label}
              <span className="rounded-full bg-surface-2 px-1.5 py-px text-xs tabular-nums text-muted-foreground">
                {counts[value]}
              </span>
            </button>
          ))}
        </div>
        <span className="flex-1" />
        {selectedRows.length > 0 && (
          <div className="mb-2 flex items-center gap-2">
            <span className="text-sm text-muted-foreground">
              {selectedRows.length} selected
            </span>
            <Button
              type="button"
              size="sm"
              onClick={() =>
                onBulkEdit(
                  selectedRows.map((r) => r.row),
                  () => setSelected(new Set())
                )
              }
            >
              Edit the filter of {selectedRows.length}
            </Button>
            <Button
              type="button"
              size="sm"
              variant="ghost"
              onClick={() => setSelected(new Set())}
            >
              Clear
            </Button>
          </div>
        )}
        <label className="relative mb-2 w-full sm:w-72">
          <span className="sr-only">Search a table</span>
          <MagnifyingGlassIcon
            aria-hidden="true"
            className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground"
          />
          <Input
            type="search"
            value={search}
            onChange={(e) => {
              setSearch(e.target.value);
              setLimit(PAGE);
            }}
            placeholder="Schema or table"
            className="h-9 pl-9"
          />
        </label>
      </div>

      <Panel>
        <div
          className={cn(
            'border-b border-line-soft px-4 py-2.5 text-[11px] uppercase tracking-[0.06em] text-muted-foreground',
            GRID
          )}
        >
          <IndeterminateCheckbox
            checked={page.length > 0 && selectedShown === page.length}
            indeterminate={selectedShown > 0 && selectedShown < page.length}
            onChange={(e) =>
              toggle(
                page.map((r) => r.key),
                (e.target as HTMLInputElement).checked
              )
            }
            aria-label="Select the tables shown"
          />
          <span>Table</span>
          <span className="hidden md:block" />
          <span className="hidden md:block">Rows copied</span>
          <span className="sr-only md:not-sr-only md:text-right">Actions</span>
        </div>

        {shown.length === 0 && (
          <p className="m-0 px-4 py-10 text-center text-sm text-muted-foreground">
            {rows.length === 0
              ? 'No table is in this job.'
              : needle
                ? 'No table matches this search.'
                : filter === 'filtered'
                  ? 'No table has a filter: every row of every table is copied.'
                  : 'No root table.'}
          </p>
        )}

        <ul className="m-0 list-none p-0">
          {page.map(({ row, index, key }) => {
            const changed = hasLocalChange(index, row.schema, row.table);
            const isSelected = selected.has(key);
            return (
              <li
                key={key}
                className={cn(
                  'border-b border-line-soft px-4 py-2.5 transition-colors last:border-b-0 hover:bg-hover',
                  GRID,
                  isSelected && 'bg-brand-soft hover:bg-brand-soft'
                )}
              >
                <IndeterminateCheckbox
                  checked={isSelected}
                  onChange={(e) =>
                    toggle([key], (e.target as HTMLInputElement).checked)
                  }
                  aria-label={`Select ${key}`}
                />
                <span className="flex min-w-0 items-center gap-2">
                  <span
                    className="min-w-0 truncate font-mono text-[13px]"
                    title={key}
                  >
                    <span className="text-muted-foreground">{row.schema}.</span>
                    <span className="text-hi">{row.table}</span>
                  </span>
                  {changed && (
                    <span
                      className="size-2 shrink-0 rounded-full bg-warn"
                      title="Changed, not saved yet"
                      aria-label="Changed, not saved yet"
                    />
                  )}
                </span>
                <span className="row-start-1 col-start-3 md:col-start-auto md:row-start-auto">
                  {row.isRootTable && <RootBadge />}
                </span>
                <span className="col-span-2 col-start-2 min-w-0 md:col-span-1 md:col-start-auto">
                  <button
                    type="button"
                    onClick={() => onEdit(index, row.schema, row.table)}
                    className={cn(
                      'w-full cursor-pointer rounded-lg border px-2.5 py-1.5 text-left transition-colors hover:border-primary',
                      row.where
                        ? 'border-line-soft bg-surface-2'
                        : 'border-dashed border-border text-muted-foreground'
                    )}
                    aria-label={`Edit the filter of ${key}`}
                  >
                    {row.where ? (
                      <code className="block whitespace-pre-wrap break-all font-mono text-xs text-foreground">
                        <span className="text-muted-foreground">WHERE </span>
                        {row.where}
                      </code>
                    ) : (
                      <span className="flex items-center gap-1.5 text-[13px]">
                        All rows
                        <span className="ml-auto flex items-center gap-1 text-xs text-link">
                          <PlusIcon className="size-3" />
                          Add a filter
                        </span>
                      </span>
                    )}
                  </button>
                </span>
                <span className="col-start-2 flex gap-1 md:col-start-auto md:justify-end">
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    className="size-8"
                    aria-label={`Edit the filter of ${key}`}
                    title="Edit the filter"
                    onClick={() => onEdit(index, row.schema, row.table)}
                  >
                    <Pencil1Icon />
                  </Button>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    className="size-8"
                    disabled={!changed}
                    aria-label={`Undo the unsaved change of ${key}`}
                    title="Undo the change not saved yet"
                    onClick={() => onReset(index, row.schema, row.table)}
                  >
                    <ResetIcon />
                  </Button>
                </span>
              </li>
            );
          })}
        </ul>

        {shown.length > limit && (
          <div className="border-t border-line-soft px-4 py-3 text-center">
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => setLimit((l) => l + PAGE)}
            >
              Show {Math.min(PAGE, shown.length - limit)} more tables (
              {shown.length - limit} left)
            </Button>
          </div>
        )}
      </Panel>
    </div>
  );
}

function RootBadge(): ReactElement {
  return (
    <TooltipProvider>
      <Tooltip delayDuration={200}>
        <TooltipTrigger asChild>
          <span className="inline-flex cursor-help rounded-full bg-brand-soft px-2 py-0.5 text-xs font-medium text-link">
            Root
          </span>
        </TooltipTrigger>
        <TooltipContent className="max-w-xs text-center">
          A root table: no table it references. Filtering it also narrows the
          tables that reference it, when the subset follows the foreign keys.
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}
