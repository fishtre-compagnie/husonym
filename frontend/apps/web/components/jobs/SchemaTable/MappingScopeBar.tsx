'use client';
import DualListBox, {
  Action,
  Option,
} from '@/components/DualListBox/DualListBox';
import Spinner from '@/components/Spinner';
import { Panel, ProgressBar } from '@/components/ds';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { cn } from '@/libs/utils';
import {
  CheckCircledIcon,
  ExclamationTriangleIcon,
  ReloadIcon,
  TableIcon,
} from '@radix-ui/react-icons';
import { useFormatter, useTranslations } from 'next-intl';
import { ReactElement, useState } from 'react';
import { FormError } from './FormErrorsCard';

interface Props {
  options: Option[];
  selected: Set<string>;
  onChange(items: Set<string>, action: Action): void;
  // One table only, for the jobs that generate a single table.
  mode: 'single' | 'many';
  // How many columns the mapping holds, told beside the tables.
  columnCount?: number;

  formErrors: FormError[];
  isValidating?: boolean;
  onValidate?(): void;
}

// So many problems are listed before the person asks to see them all.
const SHOWN_PROBLEMS = 4;

/**
 * Above the mapping: which tables of the source the job copies, and whether the
 * mapping holds together. One line when all is well; the tables are chosen in a
 * dialog, and the problems unfold only when there are some.
 */
export default function MappingScopeBar(props: Props): ReactElement {
  const {
    options,
    selected,
    onChange,
    mode,
    columnCount,
    formErrors,
    isValidating,
    onValidate,
  } = props;
  const t = useTranslations('mapping.schemaTable.scopeBar');
  const format = useFormatter();
  const [choosing, setChoosing] = useState(false);
  const [showAll, setShowAll] = useState(false);

  const errors = formErrors.filter((e) => e.level === 'error');
  const warnings = formErrors.filter((e) => e.level === 'warning');
  const problems = [...errors, ...warnings];
  const listed = showAll ? problems : problems.slice(0, SHOWN_PROBLEMS);

  const schemas = countBySchema(options, selected);
  const single = mode === 'single';

  return (
    <div className="flex flex-col gap-3">
      <Panel>
        <div className="flex flex-col gap-5 p-5 sm:flex-row sm:items-start sm:justify-between">
          <div className="flex min-w-0 gap-4">
            <span
              className="flex size-11 shrink-0 items-center justify-center rounded-xl bg-brand-soft text-primary"
              aria-hidden="true"
            >
              <TableIcon className="size-5" />
            </span>
            <div className="flex min-w-0 flex-col gap-1">
              <h2 className="m-0 font-display text-base font-semibold">
                {single ? t('titleSingle') : t('titleMany')}
              </h2>
              <p className="m-0 max-w-xl text-[13px] text-muted-foreground">
                {single ? t('descriptionSingle') : t('descriptionMany')}
              </p>
            </div>
          </div>
          <Button
            type="button"
            size="lg"
            className="shrink-0"
            onClick={() => setChoosing(true)}
          >
            <TableIcon className="size-4" />
            {single ? t('chooseTable') : t('chooseTables')}
          </Button>
        </div>

        <div className="flex flex-col gap-3 px-5 pb-5">
          <div className="flex flex-wrap items-baseline gap-x-2 gap-y-1">
            <span className="font-display text-3xl font-semibold tabular-nums">
              {format.number(selected.size)}
            </span>
            <span className="text-sm text-muted-foreground">
              {t('ofTables', { total: format.number(options.length) })}
            </span>
            {columnCount !== undefined && (
              <span className="text-sm text-muted-foreground">
                {t('columnsMapped', { count: columnCount })}
              </span>
            )}
          </div>
          <ProgressBar
            value={
              options.length > 0 ? (selected.size / options.length) * 100 : 0
            }
            label={t('progressLabel')}
            className="h-1.5"
          />
          {schemas.length > 1 && (
            <ul
              aria-label={t('bySchema')}
              className="m-0 flex list-none flex-wrap gap-2 p-0"
            >
              {schemas.map((sc) => (
                <li
                  key={sc.schema}
                  className="rounded-md border border-line-soft px-2 py-1 text-[12px]"
                >
                  <span className="font-mono">{sc.schema}</span>{' '}
                  <span className="tabular-nums text-muted-foreground">
                    {sc.selected} / {sc.total}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </div>

        <div className="flex flex-wrap items-center gap-3 border-t border-line-soft bg-surface-2 px-5 py-3">
          <span
            className={cn(
              'flex items-center gap-2 text-sm font-medium',
              errors.length > 0
                ? 'text-danger-fg'
                : warnings.length > 0
                  ? 'text-warn-fg'
                  : 'text-ok'
            )}
          >
            {problems.length === 0 ? (
              <CheckCircledIcon className="size-4" />
            ) : (
              <ExclamationTriangleIcon className="size-4" />
            )}
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
      </Panel>

      {problems.length > 0 && (
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
      )}

      <Dialog open={choosing} onOpenChange={setChoosing}>
        <DialogContent className="max-w-4xl">
          <DialogHeader>
            <DialogTitle>{t('dialogTitle')}</DialogTitle>
            <DialogDescription>{t('dialogDescription')}</DialogDescription>
          </DialogHeader>
          <DualListBox
            options={options}
            selected={selected}
            onChange={onChange}
            mode={mode}
          />
          <div className="flex justify-end">
            <Button type="button" onClick={() => setChoosing(false)}>
              {t('done')}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function problemSummary(
  t: ReturnType<typeof useTranslations<'mapping.schemaTable.scopeBar'>>,
  errors: number,
  warnings: number
): string {
  if (errors === 0 && warnings === 0) {
    return t('valid');
  }
  const parts: string[] = [];
  if (errors > 0) {
    parts.push(t('errors', { count: errors }));
  }
  if (warnings > 0) {
    parts.push(t('warnings', { count: warnings }));
  }
  return parts.join(', ');
}

// The tables of each schema of the source, and how many of them are copied.
function countBySchema(
  options: Option[],
  selected: Set<string>
): { schema: string; total: number; selected: number }[] {
  const bySchema = new Map<string, { total: number; selected: number }>();
  for (const o of options) {
    const schema = o.value.split('.')[0] ?? '';
    const count = bySchema.get(schema) ?? { total: 0, selected: 0 };
    count.total++;
    if (selected.has(o.value)) {
      count.selected++;
    }
    bySchema.set(schema, count);
  }
  return Array.from(bySchema, ([schema, c]) => ({ schema, ...c })).sort(
    (a, b) => b.selected - a.selected || a.schema.localeCompare(b.schema)
  );
}
