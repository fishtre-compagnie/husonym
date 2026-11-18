'use client';
import { Action, Option } from '@/components/DualListBox/DualListBox';
import Spinner from '@/components/Spinner';
import { Panel } from '@/components/ds';
import { Button } from '@/components/ui/button';
import { cn } from '@/libs/utils';
import {
  ExclamationTriangleIcon,
  ReloadIcon,
  TableIcon,
} from '@radix-ui/react-icons';
import { useTranslations } from 'next-intl';
import { ReactElement, useState } from 'react';
import { FormError } from './FormErrorsCard';
import TablePicker, { splitKey } from './TablePicker';

interface Props {
  options: Option[];
  selected: Set<string>;
  onChange(items: Set<string>, action: Action): void;
  // One table only, for the jobs that generate a single table.
  mode: 'single' | 'many';
  // How many columns the mapping holds, told beside the tables.
  columnCount?: number;
  // How many columns each table holds in the source, by `schema.table`.
  columnCounts: Record<string, number>;

  formErrors: FormError[];
  isValidating?: boolean;
  onValidate?(): void;
}

// So many problems are listed before the person asks to see them all.
const SHOWN_PROBLEMS = 4;
// So many schemas are named beside the tables before the rest is counted.
const NAMED_SCHEMAS = 2;

/**
 * Above the mapping: the button that chooses the tables the job copies, and one
 * line telling what the choice holds. The problems of the mapping unfold below
 * only when there are some.
 */
export default function MappingScopeBar(props: Props): ReactElement {
  const {
    options,
    selected,
    onChange,
    mode,
    columnCount,
    columnCounts,
    formErrors,
    isValidating,
    onValidate,
  } = props;
  const t = useTranslations('mapping.schemaTable.scopeBar');
  const [choosing, setChoosing] = useState(false);
  const [showAll, setShowAll] = useState(false);

  const errors = formErrors.filter((e) => e.level === 'error');
  const warnings = formErrors.filter((e) => e.level === 'warning');
  const problems = [...errors, ...warnings];
  const listed = showAll ? problems : problems.slice(0, SHOWN_PROBLEMS);

  const single = mode === 'single';
  const empty = selected.size === 0;
  const schemas = Array.from(
    new Set(Array.from(selected, (k) => splitKey(k).schema))
  ).sort((a, b) => a.localeCompare(b));
  const named = schemas.slice(0, NAMED_SCHEMAS);
  const others = schemas.length - named.length;

  return (
    <div className="flex flex-col gap-3">
      <Panel>
        <div className="flex flex-col gap-3.5 px-6 py-5">
          <div className="flex flex-col gap-0.5">
            <h2 className="m-0 font-display text-[15px] font-semibold">
              {single ? t('titleSingle') : t('titleMany')}
            </h2>
            <p className="m-0 text-[13px] text-muted-foreground">
              {empty
                ? single
                  ? t('descriptionSingleEmpty')
                  : t('descriptionManyEmpty')
                : single
                  ? t('descriptionSingle')
                  : t('descriptionMany')}
            </p>
          </div>
          <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
            <Button
              type="button"
              size="lg"
              className="shrink-0"
              onClick={() => setChoosing(true)}
            >
              <TableIcon className="size-4" />
              {single ? t('chooseTable') : t('chooseTables')}
            </Button>
            {empty ? (
              <span className="text-[13px] text-muted-foreground">
                {t('noneSelected', { total: options.length })}
              </span>
            ) : (
              <span className="text-[13.5px]">
                <strong className="font-semibold">
                  {t('tables', { count: selected.size })}
                </strong>{' '}
                <span className="text-muted-foreground">{t('in')}</span>{' '}
                {named.map((schema, i) => (
                  <span key={schema}>
                    {i > 0 && <span className="text-muted-foreground">, </span>}
                    <span className="font-mono text-[12.5px]">{schema}</span>
                  </span>
                ))}
                {others > 0 && (
                  <span className="text-muted-foreground">
                    {' '}
                    {t('moreSchemas', { count: others })}
                  </span>
                )}
                {columnCount !== undefined && (
                  <span className="text-muted-foreground">
                    {' '}
                    {t('columnsMapped', { count: columnCount })}
                  </span>
                )}
              </span>
            )}
          </div>
        </div>
      </Panel>

      {problems.length > 0 && (
        <div className="flex flex-col gap-1.5">
          <div className="flex flex-wrap items-center gap-3">
            <span
              className={cn(
                'flex items-center gap-2 text-sm font-medium',
                errors.length > 0 ? 'text-danger-fg' : 'text-warn-fg'
              )}
            >
              <ExclamationTriangleIcon className="size-4" />
              {problemSummary(t, errors.length, warnings.length)}
            </span>
            {onValidate && (
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="ml-auto"
                disabled={isValidating}
                onClick={() => onValidate()}
              >
                {isValidating ? (
                  <Spinner className="size-3.5" />
                ) : (
                  <ReloadIcon className="size-3.5" />
                )}
                {t('checkAgain')}
              </Button>
            )}
          </div>
          <ul
            aria-label={t('problems')}
            className="m-0 flex list-none flex-col gap-1.5 p-0"
          >
            {listed.map((p, i) => (
              <li
                key={`${p.path}-${i}`}
                className={cn(
                  'flex flex-wrap gap-x-2 rounded-lg px-3 py-2 text-[13px]',
                  p.level === 'error'
                    ? 'bg-danger-soft text-danger-fg'
                    : 'bg-warn-soft text-warn-fg'
                )}
              >
                {p.path && <span className="font-mono">{p.path}</span>}
                <span>{p.message}</span>
              </li>
            ))}
            {problems.length > SHOWN_PROBLEMS && (
              <li>
                <button
                  type="button"
                  className="cursor-pointer text-[13px] text-link hover:underline"
                  onClick={() => setShowAll((v) => !v)}
                >
                  {showAll
                    ? t('showFewer')
                    : t('showOthers', {
                        count: problems.length - SHOWN_PROBLEMS,
                      })}
                </button>
              </li>
            )}
          </ul>
        </div>
      )}

      <TablePicker
        open={choosing}
        onOpenChange={setChoosing}
        options={options}
        selected={selected}
        columnCounts={columnCounts}
        mode={mode}
        onApply={(items) => onChange(items, 'add')}
      />
    </div>
  );
}

function problemSummary(
  t: ReturnType<typeof useTranslations<'mapping.schemaTable.scopeBar'>>,
  errors: number,
  warnings: number
): string {
  const parts: string[] = [];
  if (errors > 0) {
    parts.push(t('errors', { count: errors }));
  }
  if (warnings > 0) {
    parts.push(t('warnings', { count: warnings }));
  }
  return parts.join(', ');
}
