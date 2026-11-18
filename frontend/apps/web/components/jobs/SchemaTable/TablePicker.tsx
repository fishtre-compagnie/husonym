'use client';
import { Option } from '@/components/DualListBox/DualListBox';
import { Segmented } from '@/components/ds';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from '@/components/ui/dialog';
import { cn } from '@/libs/utils';
import {
  CheckIcon,
  DividerHorizontalIcon,
  MagnifyingGlassIcon,
} from '@radix-ui/react-icons';
import { useVirtualizer } from '@tanstack/react-virtual';
import { useFormatter, useTranslations } from 'next-intl';
import { ReactElement, useMemo, useRef, useState } from 'react';

interface Props {
  open: boolean;
  onOpenChange(open: boolean): void;
  // Every table the job may copy, as `schema.table`.
  options: Option[];
  selected: Set<string>;
  // How many columns each table holds in the source, by `schema.table`.
  columnCounts: Record<string, number>;
  // One table only, for the jobs that generate a single table.
  mode: 'single' | 'many';
  onApply(items: Set<string>): void;
}

/**
 * Chooses the tables a job copies: the schemas of the source on the left, the
 * tables of one of them on the right. Nothing changes before "Apply".
 */
export default function TablePicker(props: Props): ReactElement {
  const { open, onOpenChange } = props;
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex h-[min(720px,90vh)] max-w-5xl flex-col gap-0 overflow-hidden p-0">
        {/* The content is mounted only while open, so each opening starts from the job's tables. */}
        <PickerBody {...props} onClose={() => onOpenChange(false)} />
      </DialogContent>
    </Dialog>
  );
}

type Filter = 'all' | 'on' | 'off';
type Check = 'all' | 'some' | 'none';

interface Table {
  key: string;
  schema: string;
  table: string;
}

const ROW_HEIGHT = 40;

function PickerBody(props: Props & { onClose(): void }): ReactElement {
  const { options, selected, columnCounts, mode, onApply, onClose } = props;
  const t = useTranslations('mapping.schemaTable.tablePicker');
  const format = useFormatter();
  const single = mode === 'single';

  const [draft, setDraft] = useState<Set<string>>(() => new Set(selected));
  const [query, setQuery] = useState('');
  const [filter, setFilter] = useState<Filter>('all');

  const schemas = useMemo(() => groupBySchema(options), [options]);
  const [active, setActive] = useState<string>(
    () =>
      schemas.find((s) => s.tables.some((tb) => selected.has(tb.key)))
        ?.schema ??
      schemas[0]?.schema ??
      ''
  );
  const manySchemas = schemas.length > 1;
  const q = query.trim().toLowerCase();

  const rows = useMemo(() => {
    let pool: Table[] = q
      ? schemas.flatMap((s) => s.tables)
      : (schemas.find((s) => s.schema === active)?.tables ?? []);
    if (q) {
      pool = pool.filter((r) => r.key.toLowerCase().includes(q));
    }
    if (filter === 'on') {
      pool = pool.filter((r) => draft.has(r.key));
    } else if (filter === 'off') {
      pool = pool.filter((r) => !draft.has(r.key));
    }
    return pool;
  }, [schemas, active, q, filter, draft]);

  const listRef = useRef<HTMLDivElement>(null);
  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => listRef.current,
    estimateSize: () => ROW_HEIGHT,
    overscan: 12,
  });

  function setMany(keys: string[], on: boolean): void {
    setDraft((prev) => {
      const next = new Set(prev);
      keys.forEach((k) => (on ? next.add(k) : next.delete(k)));
      return next;
    });
  }
  function toggle(key: string): void {
    if (single) {
      setDraft(new Set([key]));
      return;
    }
    setMany([key], !draft.has(key));
  }

  const visibleKeys = rows.map((r) => r.key);
  const visibleCheck = check(
    visibleKeys.filter((k) => draft.has(k)).length,
    visibleKeys.length
  );

  const added = [...draft].filter((k) => !selected.has(k)).length;
  const removed = [...selected].filter((k) => !draft.has(k)).length;
  const changes = added + removed;
  const draftSchemas = new Set([...draft].map((k) => splitKey(k).schema));

  return (
    <>
      <div className="flex items-start gap-4 border-b border-line-soft px-6 pb-4 pt-5 pr-14">
        <div className="flex min-w-0 grow flex-col gap-1">
          <DialogTitle className="m-0 text-lg font-semibold">
            {single ? t('titleSingle') : t('titleMany')}
          </DialogTitle>
          <DialogDescription className="m-0 text-[13px]">
            {single ? t('descriptionSingle') : t('descriptionMany')}
          </DialogDescription>
        </div>
        {!manySchemas && schemas[0] && (
          <span className="shrink-0 whitespace-nowrap rounded-full bg-brand-soft px-2.5 py-1 text-xs font-medium text-primary">
            {t.rich('schemaBadge', {
              schema: schemas[0].schema,
              mono: (chunks) => <span className="font-mono">{chunks}</span>,
            })}
          </span>
        )}
      </div>

      <div className="flex min-h-0 grow">
        {manySchemas && (
          <nav
            aria-label={t('schemas')}
            className="flex w-[280px] shrink-0 flex-col border-r border-line-soft bg-surface-2/50"
          >
            <div className="flex items-baseline justify-between px-4 pb-2 pt-3.5">
              <span className="text-[11px] font-semibold uppercase tracking-[0.06em] text-muted-foreground">
                {t('schemas')}
              </span>
              <span className="text-xs text-muted-foreground tabular-nums">
                {format.number(schemas.length)}
              </span>
            </div>
            <ul className="m-0 flex list-none flex-col gap-0.5 overflow-y-auto px-2 pb-3">
              {schemas.map((s) => {
                const keys = s.tables.map((tb) => tb.key);
                const n = keys.filter((k) => draft.has(k)).length;
                const state = check(n, keys.length);
                const isActive = s.schema === active && !q;
                return (
                  <li
                    key={s.schema}
                    className={cn(
                      'flex items-center gap-1 rounded-lg py-0.5 pl-1 pr-1.5',
                      isActive && 'bg-card shadow-[0_0_0_1px_var(--border)]'
                    )}
                  >
                    {!single && (
                      <button
                        type="button"
                        aria-label={
                          state === 'all'
                            ? t('uncheckAllIn', { schema: s.schema })
                            : t('checkAllIn', { schema: s.schema })
                        }
                        onClick={() => setMany(keys, state !== 'all')}
                        className="grid size-7 shrink-0 cursor-pointer place-items-center"
                      >
                        <CheckBox state={state} />
                      </button>
                    )}
                    <button
                      type="button"
                      aria-current={isActive ? 'true' : undefined}
                      onClick={() => {
                        setActive(s.schema);
                        setQuery('');
                        listRef.current?.scrollTo({ top: 0 });
                      }}
                      className={cn(
                        'flex min-w-0 grow cursor-pointer flex-col gap-1.5 py-1.5 pr-1.5 text-left',
                        single && 'pl-2'
                      )}
                    >
                      <span className="flex w-full items-baseline gap-2">
                        <span className="grow truncate font-mono text-[12.5px] font-medium">
                          {s.schema}
                        </span>
                        <span className="text-[11.5px] text-muted-foreground tabular-nums">
                          {format.number(n)} / {format.number(keys.length)}
                        </span>
                      </span>
                      <span className="block h-[3px] w-full overflow-hidden rounded-full bg-muted">
                        <span
                          className="block h-full rounded-full bg-primary"
                          style={{ width: `${(n / keys.length) * 100}%` }}
                        />
                      </span>
                    </button>
                  </li>
                );
              })}
            </ul>
          </nav>
        )}

        <div className="flex min-w-0 grow flex-col">
          <div className="flex flex-wrap items-center gap-3 border-b border-line-soft px-5 py-3.5">
            <label className="flex h-[38px] min-w-[220px] grow items-center gap-2 rounded-[9px] border border-border bg-card px-3 focus-within:outline-2 focus-within:outline-link">
              <MagnifyingGlassIcon
                className="size-4 shrink-0 text-muted-foreground"
                aria-hidden="true"
              />
              <span className="sr-only">{t('search')}</span>
              <input
                type="search"
                value={query}
                onChange={(e) => {
                  setQuery(e.target.value);
                  listRef.current?.scrollTo({ top: 0 });
                }}
                placeholder={manySchemas ? t('searchAll') : t('search')}
                className="grow border-none bg-transparent text-[13.5px] outline-none placeholder:text-muted-foreground"
              />
            </label>
            {!single && (
              <Segmented<Filter>
                ariaLabel={t('filter')}
                value={filter}
                onChange={setFilter}
                options={[
                  { value: 'all', label: t('filterAll') },
                  { value: 'on', label: t('filterOn') },
                  { value: 'off', label: t('filterOff') },
                ]}
              />
            )}
          </div>

          <div className="flex items-center gap-2.5 border-b border-line-soft bg-surface-2/40 px-5 py-2.5">
            {!single && rows.length > 0 && (
              <button
                type="button"
                onClick={() => setMany(visibleKeys, visibleCheck !== 'all')}
                className="flex cursor-pointer items-center gap-2.5 py-1 text-[13px] font-medium"
              >
                <CheckBox state={visibleCheck} />
                {visibleCheck === 'all' ? t('uncheckAll') : t('checkAll')}
              </button>
            )}
            <span className="grow text-[12.5px] text-muted-foreground">
              {q
                ? t('scopeSearch', { count: rows.length, query: query.trim() })
                : manySchemas
                  ? t('scopeSchema', { schema: active, count: rows.length })
                  : t('scopeTables', { count: rows.length })}
            </span>
            <span className="text-xs text-muted-foreground">
              {t('columns')}
            </span>
          </div>

          <div ref={listRef} className="min-h-0 grow overflow-y-auto px-2 py-1">
            {rows.length === 0 ? (
              <p className="my-10 text-center text-[13px] text-muted-foreground">
                {options.length === 0 ? t('noTables') : t('noMatch')}
              </p>
            ) : (
              <ul
                className="relative m-0 list-none p-0"
                style={{ height: virtualizer.getTotalSize() }}
              >
                {virtualizer.getVirtualItems().map((item) => {
                  const r = rows[item.index];
                  const on = draft.has(r.key);
                  const changed = on !== selected.has(r.key);
                  const cols = columnCounts[r.key];
                  return (
                    <li
                      key={r.key}
                      className="absolute left-0 top-0 w-full"
                      style={{
                        height: ROW_HEIGHT,
                        transform: `translateY(${item.start}px)`,
                      }}
                    >
                      <button
                        type="button"
                        role={single ? 'radio' : 'checkbox'}
                        aria-checked={on}
                        onClick={() => toggle(r.key)}
                        className={cn(
                          'flex h-full w-full cursor-pointer items-center gap-3 rounded-lg px-3 text-left transition-colors hover:bg-hover',
                          on && 'bg-brand-soft hover:bg-brand-soft'
                        )}
                      >
                        {single ? (
                          <Radio on={on} />
                        ) : (
                          <CheckBox state={on ? 'all' : 'none'} />
                        )}
                        <span className="min-w-0 grow truncate font-mono text-[13px]">
                          {q && manySchemas && (
                            <span className="text-muted-foreground">
                              {r.schema}.
                            </span>
                          )}
                          {r.table}
                        </span>
                        {changed && (
                          <span
                            className={cn(
                              'shrink-0 rounded-full px-2 py-0.5 text-[11px] font-medium',
                              on
                                ? 'bg-ok-soft text-ok'
                                : 'bg-warn-soft text-warn-fg'
                            )}
                          >
                            {on ? t('added') : t('removed')}
                          </span>
                        )}
                        <span className="w-14 shrink-0 text-right text-xs text-muted-foreground tabular-nums">
                          {cols !== undefined ? format.number(cols) : '—'}
                        </span>
                      </button>
                    </li>
                  );
                })}
              </ul>
            )}
          </div>
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-3 border-t border-line-soft px-6 py-3.5">
        <div className="flex min-w-0 grow flex-col gap-0.5">
          <span className="text-[13.5px] font-semibold">
            {t('summary', {
              count: draft.size,
              schemas: manySchemas ? draftSchemas.size : 0,
            })}
          </span>
          <span className="text-xs text-muted-foreground">
            {changes > 0 ? t('diff', { added, removed }) : t('noChange')}
          </span>
        </div>
        <Button type="button" variant="outline" onClick={onClose}>
          {t('cancel')}
        </Button>
        <Button
          type="button"
          disabled={changes === 0}
          onClick={() => {
            onApply(draft);
            onClose();
          }}
        >
          {changes > 0 ? t('applyCount', { count: changes }) : t('apply')}
        </Button>
      </div>
    </>
  );
}

function CheckBox(props: { state: Check }): ReactElement {
  const { state } = props;
  return (
    <span
      aria-hidden="true"
      className={cn(
        'grid size-4 shrink-0 place-items-center rounded-[4px] border-[1.5px]',
        state === 'none'
          ? 'border-muted-foreground/50 bg-card'
          : 'border-primary bg-primary text-primary-foreground'
      )}
    >
      {state === 'all' && <CheckIcon className="size-3.5" />}
      {state === 'some' && <DividerHorizontalIcon className="size-3.5" />}
    </span>
  );
}

function Radio(props: { on: boolean }): ReactElement {
  return (
    <span
      aria-hidden="true"
      className={cn(
        'grid size-4 shrink-0 place-items-center rounded-full border-[1.5px]',
        props.on ? 'border-primary' : 'border-muted-foreground/50'
      )}
    >
      {props.on && <span className="size-2 rounded-full bg-primary" />}
    </span>
  );
}

function check(n: number, total: number): Check {
  if (n === 0 || total === 0) {
    return 'none';
  }
  return n === total ? 'all' : 'some';
}

export function splitKey(key: string): { schema: string; table: string } {
  const dot = key.indexOf('.');
  return dot < 0
    ? { schema: '', table: key }
    : { schema: key.slice(0, dot), table: key.slice(dot + 1) };
}

function groupBySchema(
  options: Option[]
): { schema: string; tables: Table[] }[] {
  const bySchema = new Map<string, Table[]>();
  for (const o of options) {
    const { schema, table } = splitKey(o.value);
    const list = bySchema.get(schema) ?? [];
    list.push({ key: o.value, schema, table });
    bySchema.set(schema, list);
  }
  return Array.from(bySchema, ([schema, tables]) => ({
    schema,
    tables: tables.sort((a, b) => a.table.localeCompare(b.table)),
  })).sort((a, b) => a.schema.localeCompare(b.schema));
}
