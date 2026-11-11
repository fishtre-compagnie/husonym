'use client';
import LicenseFeatureNotice from '@/components/LicenseFeatureNotice';
import { Panel, StatusDot, Tone } from '@/components/ds';
import { AppTableFeatures } from '@/components/table/features';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { useLicenseFeature } from '@/libs/hooks/useLicense';
import { cn } from '@/libs/utils';
import { Transformer } from '@/shared/transformers';
import { isInvalidTransformer } from '@/util/util';
import { JobMappingTransformerForm } from '@/yup-validations/jobs';
import { JobMapping, TransformerSource } from '@husonym/sdk';
import {
  ArrowRightIcon,
  ChevronRightIcon,
  MagnifyingGlassIcon,
  ReloadIcon,
} from '@radix-ui/react-icons';
import { Row } from '@tanstack/react-table';
import { ReactElement, ReactNode, useMemo, useState } from 'react';
import ApplyDefaultTransformersButton from '../SchemaTable/ApplyDefaultTransformersButton';
import ExportJobMappingsButton from '../SchemaTable/ExportJobMappingsButton';
import ImportJobMappingsButton, {
  ImportMappingsConfig,
} from '../SchemaTable/ImportJobMappingsButton';
import { TransformerResult } from '../SchemaTable/transformer-handler';
import { isAnonymizingTransformer, JobMappingRow, tableKeyOf } from './Columns';
import IndeterminateCheckbox from './IndeterminateCheckbox';
import MappingCell from './MappingCell';
import MappingDecisionPanel, {
  ColumnDecisionTarget,
} from './MappingDecisionPanel';
import RgpdCell from './RgpdCell';
import { rgpdRank, rgpdState } from './rgpd-state';
import SelectionBar from './SelectionBar';

interface Props {
  rows: JobMappingRow[];
  onTransformerUpdate(index: number, config: JobMappingTransformerForm): void;
  getAvailableTransformers(index: number): TransformerResult;
  getTransformerFromField(index: number): Transformer;
  getTransformerFromFieldValue(value: JobMappingTransformerForm): Transformer;
  getAvailableTransformersForBulk(
    rows: Row<AppTableFeatures, JobMappingRow>[]
  ): TransformerResult;
  onTransformerBulkUpdate(
    indices: number[],
    config: JobMappingTransformerForm
  ): void;
  getColumnDecision(index: number): ColumnDecisionTarget | undefined;

  onApplyDefaultClick(override: boolean): void;
  onExport(indices: number[], shouldFormat: boolean): void;
  onImport(jobmappings: JobMapping[], config: ImportMappingsConfig): void;
  hasMissingSourceColumnMappings: boolean;
  onRemoveMissingSourceColumnMappings(): void;

  // The content scan (Presidio): only when the job reads a source.
  showPiiScan?: boolean;
  onScanContent?(): void;
  isScanningPii?: boolean;
  sourceConnectionId?: string;
}

// What a column asks of the person, as the tabs sort them.
type Group = 'decide' | 'protected' | 'none';
type GroupFilter = Group | 'all';

const GROUPS: { value: GroupFilter; label: string }[] = [
  { value: 'decide', label: 'To decide' },
  { value: 'protected', label: 'Protected' },
  { value: 'none', label: 'Not personal' },
  { value: 'all', label: 'All columns' },
];

interface ColumnItem {
  row: JobMappingRow;
  index: number;
  group: Group;
}

interface TableItem {
  key: string;
  schema: string;
  table: string;
  columns: ColumnItem[];
  personal: number;
  toDecide: number;
}

// So many tables are listed at first; the rest come on demand. Only the tables
// opened render their columns: the page stays light on a large schema.
const TABLE_PAGE = 60;

/**
 * The mapping of a job read from a source, drawn as what it is: each table of the
 * source, and inside it each column with what the detection found and what leaves
 * it for the destinations.
 *
 *   public.users  ▸  12 columns · 4 personal · 2 to decide
 *       email   varchar · unique   Personal: Email   →   Generate email
 *
 * One list, in the flow of the page: no box with a scroll of its own.
 */
export default function MappingExplorer(props: Props): ReactElement {
  const {
    rows,
    onTransformerUpdate,
    getAvailableTransformers,
    getTransformerFromField,
    getTransformerFromFieldValue,
    getAvailableTransformersForBulk,
    onTransformerBulkUpdate,
    getColumnDecision,
    onApplyDefaultClick,
    onExport,
    onImport,
    hasMissingSourceColumnMappings,
    onRemoveMissingSourceColumnMappings,
    showPiiScan,
    onScanContent,
    isScanningPii,
    sourceConnectionId,
  } = props;

  const tables = useMemo(() => summarize(rows), [rows]);
  const counts = useMemo(() => {
    const c: Record<GroupFilter, number> = {
      decide: 0,
      protected: 0,
      none: 0,
      all: rows.length,
    };
    tables.forEach((t) => t.columns.forEach((col) => (c[col.group] += 1)));
    return c;
  }, [tables, rows.length]);

  // Opens on what needs the person, when something does.
  const [group, setGroup] = useState<GroupFilter>(() =>
    counts.decide > 0 ? 'decide' : 'all'
  );
  const [search, setSearch] = useState('');
  const [expanded, setExpanded] = useState<Set<string>>(
    () => new Set(tables.length === 1 ? [tables[0].key] : [])
  );
  const [limit, setLimit] = useState(TABLE_PAGE);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [opened, setOpened] = useState<number | null>(null);

  const needle = search.trim().toLowerCase();

  // Each table with the columns the tab and the search keep. A table whose name
  // matches keeps all its columns of the tab; otherwise only the columns that match.
  const shown = useMemo(() => {
    const out: {
      table: TableItem;
      columns: ColumnItem[];
      byColumn: boolean;
    }[] = [];
    for (const t of tables) {
      const inGroup = t.columns.filter(
        (c) => group === 'all' || c.group === group
      );
      if (inGroup.length === 0) {
        continue;
      }
      if (!needle || t.key.toLowerCase().includes(needle)) {
        out.push({ table: t, columns: inGroup, byColumn: false });
        continue;
      }
      const matching = inGroup.filter((c) =>
        c.row.column.toLowerCase().includes(needle)
      );
      if (matching.length > 0) {
        out.push({ table: t, columns: matching, byColumn: true });
      }
    }
    return out;
  }, [tables, group, needle]);

  // A table opens on a click, or by itself when the search found it by a column.
  const isOpen = (key: string, byColumn: boolean): boolean =>
    expanded.has(key) || byColumn;

  // The columns the decision panel walks through: those on screen, in order.
  const visibleColumns = useMemo(
    () =>
      shown
        .slice(0, limit)
        .filter((s) => isOpen(s.table.key, s.byColumn))
        .flatMap((s) => s.columns),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [shown, limit, expanded]
  );
  const position =
    opened === null ? -1 : visibleColumns.findIndex((c) => c.index === opened);
  const target = opened === null ? undefined : getColumnDecision(opened);

  function toggleTable(key: string): void {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(key)) {
        next.delete(key);
      } else {
        next.add(key);
      }
      return next;
    });
  }

  function toggleColumns(indices: number[], on: boolean): void {
    setSelected((prev) => {
      const next = new Set(prev);
      indices.forEach((i) => (on ? next.add(i) : next.delete(i)));
      return next;
    });
  }

  const selectedIndices = Array.from(selected);
  const allOpen =
    shown.length > 0 && shown.every((s) => isOpen(s.table.key, s.byColumn));

  return (
    <div className="flex flex-col gap-4">
      <Toolbar
        group={group}
        counts={counts}
        onGroup={(g) => {
          setGroup(g);
          setLimit(TABLE_PAGE);
        }}
        search={search}
        onSearch={(v) => {
          setSearch(v);
          setLimit(TABLE_PAGE);
        }}
        actions={
          <>
            {showPiiScan && (
              <ScanButton
                isScanning={!!isScanningPii}
                onScan={() => onScanContent?.()}
              />
            )}
            <ApplyDefaultTransformersButton
              isDisabled={rows.length === 0}
              onClick={onApplyDefaultClick}
            />
            <ImportJobMappingsButton onImport={onImport} />
            <ExportJobMappingsButton
              onClick={(shouldFormat) =>
                onExport(selectedIndices, shouldFormat)
              }
              count={selectedIndices.length}
            />
          </>
        }
      />
      {showPiiScan && (
        <LicenseFeatureNotice feature="pii_detection" className="w-full" />
      )}
      {hasMissingSourceColumnMappings && (
        <div className="flex flex-wrap items-center gap-3 rounded-xl border border-warn-line bg-warn-soft px-4 py-3 text-sm text-warn-fg">
          <span className="flex-1">
            Some mapped columns no longer exist in the source.
          </span>
          <Button
            type="button"
            size="sm"
            variant="outline"
            onClick={onRemoveMissingSourceColumnMappings}
          >
            Remove them from the mapping
          </Button>
        </div>
      )}

      <Panel>
        <div className="flex items-center gap-3 border-b border-line-soft px-4 py-2.5 text-xs text-muted-foreground">
          <span className="flex-1">
            {shown.length} table{shown.length === 1 ? '' : 's'} ·{' '}
            {group === 'all' && !needle
              ? `${rows.length} columns`
              : `${shown.reduce((n, s) => n + s.columns.length, 0)} of ${rows.length} columns`}
          </span>
          {shown.length > 1 && (
            <button
              type="button"
              className="cursor-pointer text-link hover:underline"
              onClick={() =>
                setExpanded(
                  allOpen
                    ? new Set()
                    : new Set(shown.slice(0, limit).map((s) => s.table.key))
                )
              }
            >
              {allOpen ? 'Collapse all' : 'Expand all'}
            </button>
          )}
        </div>

        {shown.length === 0 && (
          <p className="m-0 px-4 py-10 text-center text-sm text-muted-foreground">
            {rows.length === 0
              ? 'No table is in this job yet. Choose the tables above.'
              : needle
                ? 'No table or column matches this search.'
                : group === 'decide'
                  ? 'Nothing left to decide: every personal column is protected.'
                  : 'No column in this group.'}
          </p>
        )}

        <ul className="m-0 list-none p-0">
          {shown.slice(0, limit).map(({ table, columns, byColumn }) => {
            const open = isOpen(table.key, byColumn);
            const indices = columns.map((c) => c.index);
            const selectedHere = indices.filter((i) => selected.has(i)).length;
            return (
              <li
                key={table.key}
                className="border-b border-line-soft last:border-b-0"
              >
                <TableLine
                  table={table}
                  open={open}
                  onToggle={() => toggleTable(table.key)}
                />
                {open && (
                  <div className="border-t border-line-soft bg-background/40 pb-2">
                    <ColumnsHead
                      checked={
                        selectedHere > 0 && selectedHere === indices.length
                      }
                      indeterminate={
                        selectedHere > 0 && selectedHere < indices.length
                      }
                      onCheck={(on) => toggleColumns(indices, on)}
                      tableKey={table.key}
                    />
                    {columns.map((c) => (
                      <ColumnLine
                        key={c.index}
                        item={c}
                        selected={selected.has(c.index)}
                        onSelect={(on) => toggleColumns([c.index], on)}
                        onOpen={() => setOpened(c.index)}
                        transformerName={transformerName(
                          getTransformerFromField(c.index)
                        )}
                        hasSuggestion={hasSuggestion(
                          c.row,
                          getAvailableTransformers(c.index)
                        )}
                      />
                    ))}
                    {columns.length < table.columns.length && (
                      <p className="m-0 px-4 pt-2 text-xs text-muted-foreground sm:pl-[52px]">
                        {table.columns.length - columns.length} other column
                        {table.columns.length - columns.length === 1
                          ? ''
                          : 's'}{' '}
                        of this table hidden by the filter.
                      </p>
                    )}
                  </div>
                )}
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
              onClick={() => setLimit((l) => l + TABLE_PAGE)}
            >
              Show {Math.min(TABLE_PAGE, shown.length - limit)} more tables (
              {shown.length - limit} left)
            </Button>
          </div>
        )}
      </Panel>

      <SelectionBar
        count={selectedIndices.length}
        getAllowedTransformers={() =>
          getAvailableTransformersForBulk(
            selectedIndices.map((i) => asRow(rows[i], i))
          )
        }
        getTransformerFromFieldValue={getTransformerFromFieldValue}
        onApply={(value) => {
          onTransformerBulkUpdate(selectedIndices, value);
          setSelected(new Set());
        }}
        onClear={() => setSelected(new Set())}
      />

      {target && opened !== null && (
        <MappingDecisionPanel
          key={opened}
          target={target}
          getTransformers={() => getAvailableTransformers(opened)}
          getTransformerFromFieldValue={getTransformerFromFieldValue}
          sourceConnectionId={sourceConnectionId}
          navigation={{
            position: position + 1,
            total: visibleColumns.length,
            onPrevious:
              position > 0
                ? () => setOpened(visibleColumns[position - 1].index)
                : undefined,
            onNext:
              position >= 0 && position < visibleColumns.length - 1
                ? () => setOpened(visibleColumns[position + 1].index)
                : undefined,
          }}
          onClose={() => setOpened(null)}
          onApply={(transformer) => onTransformerUpdate(opened, transformer)}
        />
      )}
    </div>
  );
}

function Toolbar(props: {
  group: GroupFilter;
  counts: Record<GroupFilter, number>;
  onGroup(g: GroupFilter): void;
  search: string;
  onSearch(v: string): void;
  actions: ReactNode;
}): ReactElement {
  const { group, counts, onGroup, search, onSearch, actions } = props;
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center justify-end gap-2">
        {actions}
      </div>
      <div className="flex flex-wrap items-end gap-3 border-b border-line-soft">
        <div
          role="tablist"
          aria-label="Columns by what they ask"
          className="-mb-px flex flex-wrap gap-0.5"
        >
          {GROUPS.map((g) => {
            const isSelected = g.value === group;
            const alarm = g.value === 'decide' && counts.decide > 0;
            return (
              <button
                key={g.value}
                type="button"
                role="tab"
                aria-selected={isSelected}
                onClick={() => onGroup(g.value)}
                className={cn(
                  'flex cursor-pointer items-center gap-2 border-b-2 px-3 py-2.5 text-sm transition-colors',
                  isSelected
                    ? 'border-primary text-hi'
                    : 'border-transparent text-muted-foreground hover:text-foreground'
                )}
              >
                {g.label}
                <span
                  className={cn(
                    'rounded-full px-1.5 py-px text-xs tabular-nums',
                    alarm
                      ? 'bg-danger-soft text-danger-fg'
                      : 'bg-surface-2 text-muted-foreground'
                  )}
                >
                  {counts[g.value]}
                </span>
              </button>
            );
          })}
        </div>
        <span className="flex-1" />
        <label className="relative mb-2 w-full sm:w-64">
          <span className="sr-only">Search a table or a column</span>
          <MagnifyingGlassIcon
            aria-hidden="true"
            className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground"
          />
          <Input
            type="search"
            value={search}
            onChange={(e) => onSearch(e.target.value)}
            placeholder="Table or column"
            className="h-9 pl-9"
          />
        </label>
      </div>
    </div>
  );
}

function ScanButton(props: {
  isScanning: boolean;
  onScan(): void;
}): ReactElement {
  const { isScanning, onScan } = props;
  // Without the pii_detection feature the API refuses every table: the button is
  // greyed and the notice beneath the toolbar says why.
  const { allowed } = useLicenseFeature('pii_detection');
  return (
    <Button
      variant="outline"
      type="button"
      disabled={isScanning || !allowed}
      onClick={onScan}
      title="Reads a sample of each column to find personal data, also in columns whose name says nothing. The suggested transformers are applied."
    >
      {isScanning ? (
        <ReloadIcon className="size-3.5 animate-spin" />
      ) : (
        <MagnifyingGlassIcon className="size-3.5" />
      )}
      {isScanning ? 'Scanning…' : 'Scan the content'}
    </Button>
  );
}

// The grid of a table's line and of its column lines share their right part, so
// the state of a table stands above the transformers of its columns.
const TABLE_GRID =
  'sm:grid sm:grid-cols-[16px_minmax(0,1fr)_88px_88px_170px] sm:items-center sm:gap-4';
const COLUMN_GRID =
  'grid grid-cols-[16px_minmax(0,1fr)] gap-x-3 gap-y-1.5 md:grid-cols-[16px_minmax(140px,1.1fr)_minmax(150px,1fr)_16px_minmax(200px,1.3fr)] md:items-center md:gap-x-4';

function TableLine(props: {
  table: TableItem;
  open: boolean;
  onToggle(): void;
}): ReactElement {
  const { table, open, onToggle } = props;
  const state = tableState(table);
  return (
    <button
      type="button"
      aria-expanded={open}
      onClick={onToggle}
      className={cn(
        'flex w-full cursor-pointer flex-wrap items-center gap-x-4 gap-y-1 px-4 py-3 text-left text-sm transition-colors hover:bg-hover',
        TABLE_GRID
      )}
    >
      <ChevronRightIcon
        aria-hidden="true"
        className={cn(
          'size-4 text-muted-foreground transition-transform',
          open && 'rotate-90 text-link'
        )}
      />
      <span className="min-w-0 flex-1 truncate font-mono text-[13px]">
        <span className="text-muted-foreground">{table.schema}.</span>
        <span className="font-medium text-hi">{table.table}</span>
      </span>
      <span className="text-xs tabular-nums text-muted-foreground sm:text-right">
        {table.columns.length} column{table.columns.length === 1 ? '' : 's'}
      </span>
      <span className="text-xs tabular-nums text-muted-foreground sm:text-right">
        {table.personal > 0 ? `${table.personal} personal` : '—'}
      </span>
      <span
        className={cn(
          'flex items-center gap-2 text-[13px]',
          state.tone === 'danger'
            ? 'font-medium text-danger-fg'
            : state.tone === 'ok'
              ? 'text-ok'
              : 'text-muted-foreground'
        )}
      >
        <StatusDot tone={state.tone} />
        {state.label}
      </span>
    </button>
  );
}

function ColumnsHead(props: {
  checked: boolean;
  indeterminate: boolean;
  onCheck(on: boolean): void;
  tableKey: string;
}): ReactElement {
  const { checked, indeterminate, onCheck, tableKey } = props;
  return (
    <div
      className={cn(
        'px-4 pb-1.5 pt-3 text-[11px] uppercase tracking-[0.06em] text-muted-foreground sm:pl-9',
        COLUMN_GRID
      )}
    >
      <span title={`Select the columns of ${tableKey}`}>
        <IndeterminateCheckbox
          checked={checked}
          indeterminate={indeterminate}
          onChange={(e) => onCheck((e.target as HTMLInputElement).checked)}
          aria-label={`Select the columns of ${tableKey} shown`}
        />
      </span>
      <span>Column</span>
      <span className="hidden md:block">Detected</span>
      <span className="hidden md:block" />
      <span className="hidden md:block">Leaves for the destination as</span>
    </div>
  );
}

function ColumnLine(props: {
  item: ColumnItem;
  selected: boolean;
  onSelect(on: boolean): void;
  onOpen(): void;
  transformerName: string;
  hasSuggestion: boolean;
}): ReactElement {
  const { item, selected, onSelect, onOpen, transformerName, hasSuggestion } =
    props;
  const { row } = item;
  const anonymizes = isAnonymizingTransformer(row.transformer);
  const state = rgpdState({
    isSensitive: row.isSensitive,
    confidence: row.piiConfidence,
    isAnonymized: anonymizes,
    contentNotAnalyzed: row.contentNotAnalyzed,
  });
  return (
    <div
      className={cn(
        'mx-2 rounded-lg px-2 py-2 transition-colors hover:bg-hover sm:pl-7',
        COLUMN_GRID,
        selected && 'bg-brand-soft hover:bg-brand-soft'
      )}
    >
      <span className="pt-0.5 md:pt-0">
        <IndeterminateCheckbox
          checked={selected}
          onChange={(e) => onSelect((e.target as HTMLInputElement).checked)}
          aria-label={`Select ${row.column}`}
        />
      </span>
      <span className="flex min-w-0 flex-col">
        <span className="truncate font-mono text-[13px] text-hi">
          {row.column}
        </span>
        <span className="truncate font-mono text-[11px] text-muted-foreground">
          {columnMeta(row)}
        </span>
      </span>
      <span className="col-start-2 md:col-start-auto">
        <RgpdCell
          isSensitive={row.isSensitive}
          dataCategory={row.dataCategory}
          confidence={row.piiConfidence}
          method={row.piiDetectionMethod}
          contentNotAnalyzed={row.contentNotAnalyzed}
          isAnonymized={anonymizes}
          hasSuggestion={hasSuggestion}
        />
      </span>
      <ArrowRightIcon
        aria-hidden="true"
        className="hidden size-4 text-muted-foreground md:block"
      />
      <span className="col-start-2 min-w-0 md:col-start-auto">
        <MappingCell
          column={row.column}
          transformer={row.transformer}
          name={transformerName}
          isSensitive={row.isSensitive}
          state={state}
          onOpen={onOpen}
        />
      </span>
    </div>
  );
}

function columnMeta(row: JobMappingRow): string {
  const parts = [row.dataType];
  const c = row.constraints;
  if (c.isPrimaryKey) {
    parts.push('primary key');
  }
  if (c.foreignKey[0]) {
    parts.push(`→ ${c.foreignKey[1].join(', ')}`);
  }
  if (c.virtualForeignKey[0]) {
    parts.push(`virtual → ${c.virtualForeignKey[1].join(', ')}`);
  }
  if (c.isUnique && !c.isPrimaryKey) {
    parts.push('unique');
  }
  if (row.isNullable) {
    parts.push('nullable');
  }
  if (row.attributes.generatedType) {
    parts.push('generated');
  }
  if (row.attributes.identityType) {
    parts.push('identity');
  }
  return parts.filter(Boolean).join(' · ');
}

function transformerName(t: Transformer): string {
  return isInvalidTransformer(t) ? '' : (t.name ?? '');
}

// A suggested transformer counts when the column can take it: it has to be among
// those offered for the type of the column and of the job.
function hasSuggestion(
  row: JobMappingRow,
  offered: TransformerResult
): boolean {
  return (
    row.suggestedTransformerSource !== TransformerSource.UNSPECIFIED &&
    offered.system.some((t) => t.source === row.suggestedTransformerSource)
  );
}

// The bulk helpers read the rows of a TanStack table: this stands for one.
function asRow(
  row: JobMappingRow,
  index: number
): Row<AppTableFeatures, JobMappingRow> {
  return {
    index,
    original: row,
    getValue: (id: string) => (row as unknown as Record<string, unknown>)[id],
  } as unknown as Row<AppTableFeatures, JobMappingRow>;
}

function tableState(t: TableItem): { label: string; tone: Tone } {
  if (t.toDecide > 0) {
    return { label: `${t.toDecide} to decide`, tone: 'danger' };
  }
  if (t.personal > 0) {
    return { label: 'Protected', tone: 'ok' };
  }
  return { label: 'Nothing personal', tone: 'quiet' };
}

function groupOf(rank: number): Group {
  return rank >= 2 ? 'decide' : rank === 1 ? 'protected' : 'none';
}

function summarize(rows: JobMappingRow[]): TableItem[] {
  const byKey = new Map<string, TableItem>();
  rows.forEach((row, index) => {
    const key = tableKeyOf(row.schema, row.table);
    let t = byKey.get(key);
    if (!t) {
      t = {
        key,
        schema: row.schema,
        table: row.table,
        columns: [],
        personal: 0,
        toDecide: 0,
      };
      byKey.set(key, t);
    }
    const state = rgpdState({
      isSensitive: row.isSensitive,
      confidence: row.piiConfidence,
      isAnonymized: isAnonymizingTransformer(row.transformer),
      contentNotAnalyzed: row.contentNotAnalyzed,
    });
    const group = groupOf(rgpdRank(state));
    t.columns.push({ row, index, group });
    if (
      state === 'compliant' ||
      state === 'not_anonymized' ||
      state === 'review'
    ) {
      t.personal += 1;
    }
    if (group === 'decide') {
      t.toDecide += 1;
    }
  });
  // By name: a table does not move under the cursor once its columns are decided.
  return Array.from(byKey.values()).sort((a, b) => a.key.localeCompare(b.key));
}
