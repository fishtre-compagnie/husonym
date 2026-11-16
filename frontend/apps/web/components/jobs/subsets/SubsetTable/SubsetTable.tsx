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
import { useTranslations } from 'next-intl';
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
  const t = useTranslations('mapping.subsets.table');

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
          aria-label={t('tabs')}
          className="-mb-px flex flex-wrap gap-0.5"
        >
          {(
            [
              ['all', t('all')],
              ['filtered', t('filtered')],
              ['root', t('root')],
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
              {t('selected', { count: selectedRows.length })}
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
              {t('editSelected', { count: selectedRows.length })}
            </Button>
            <Button
              type="button"
              size="sm"
              variant="ghost"
              onClick={() => setSelected(new Set())}
            >
              {t('clear')}
            </Button>
          </div>
        )}
        <label className="relative mb-2 w-full sm:w-72">
          <span className="sr-only">{t('search')}</span>
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
            placeholder={t('searchPlaceholder')}
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
            aria-label={t('selectShown')}
          />
          <span>{t('table')}</span>
          <span className="hidden md:block" />
          <span className="hidden md:block">{t('rowsCopied')}</span>
          <span className="sr-only md:not-sr-only md:text-right">
            {t('actions')}
          </span>
        </div>

        {shown.length === 0 && (
          <p className="m-0 px-4 py-10 text-center text-sm text-muted-foreground">
            {rows.length === 0
              ? t('emptyJob')
              : needle
                ? t('emptySearch')
                : filter === 'filtered'
                  ? t('emptyFiltered')
                  : t('emptyRoot')}
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
                  aria-label={t('select', { table: key })}
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
                      title={t('changed')}
                      aria-label={t('changed')}
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
                    aria-label={t('editFilterOf', { table: key })}
                  >
                    {row.where ? (
                      <code className="block whitespace-pre-wrap break-all font-mono text-xs text-foreground">
                        <span className="text-muted-foreground">WHERE </span>
                        {row.where}
                      </code>
                    ) : (
                      <span className="flex items-center gap-1.5 text-[13px]">
                        {t('allRows')}
                        <span className="ml-auto flex items-center gap-1 text-xs text-link">
                          <PlusIcon className="size-3" />
                          {t('addFilter')}
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
                    aria-label={t('editFilterOf', { table: key })}
                    title={t('editFilter')}
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
                    aria-label={t('undoOf', { table: key })}
                    title={t('undo')}
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
              {t('showMore', {
                count: Math.min(PAGE, shown.length - limit),
                left: shown.length - limit,
              })}
            </Button>
          </div>
        )}
      </Panel>
    </div>
  );
}

function RootBadge(): ReactElement {
  const t = useTranslations('mapping.subsets.table');
  return (
    <TooltipProvider>
      <Tooltip delayDuration={200}>
        <TooltipTrigger asChild>
          <span className="inline-flex cursor-help rounded-full bg-brand-soft px-2 py-0.5 text-xs font-medium text-link">
            {t('rootBadge')}
          </span>
        </TooltipTrigger>
        <TooltipContent className="max-w-xs text-center">
          {t('rootTooltip')}
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}
