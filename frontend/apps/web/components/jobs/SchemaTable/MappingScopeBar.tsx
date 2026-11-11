'use client';
import DualListBox, {
  Action,
  Option,
} from '@/components/DualListBox/DualListBox';
import Spinner from '@/components/Spinner';
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
  ChevronDownIcon,
  ExclamationTriangleIcon,
  ReloadIcon,
} from '@radix-ui/react-icons';
import { ReactElement, useState } from 'react';
import { FormError } from './FormErrorsCard';

interface Props {
  options: Option[];
  selected: Set<string>;
  onChange(items: Set<string>, action: Action): void;
  // One table only, for the jobs that generate a single table.
  mode: 'single' | 'many';

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
    formErrors,
    isValidating,
    onValidate,
  } = props;
  const [choosing, setChoosing] = useState(false);
  const [showAll, setShowAll] = useState(false);

  const errors = formErrors.filter((e) => e.level === 'error');
  const warnings = formErrors.filter((e) => e.level === 'warning');
  const problems = [...errors, ...warnings];
  const listed = showAll ? problems : problems.slice(0, SHOWN_PROBLEMS);

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-x-6 gap-y-3 rounded-2xl border border-line-soft bg-card px-5 py-4">
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className="text-[13px] text-muted-foreground">
            Tables copied by this job
          </span>
          <span className="font-display text-lg font-semibold">
            {selected.size}{' '}
            <span className="text-sm font-normal text-muted-foreground">
              of {options.length} in the source
            </span>
          </span>
        </div>
        <Button
          type="button"
          variant="outline"
          onClick={() => setChoosing(true)}
        >
          Choose tables
          <ChevronDownIcon className="size-3.5" />
        </Button>

        <span
          className="hidden h-10 w-px bg-line-soft sm:block"
          aria-hidden="true"
        />

        <div className="flex items-center gap-3">
          <span
            className={cn(
              'flex items-center gap-2 text-sm',
              errors.length > 0
                ? 'font-medium text-danger-fg'
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
            {problemSummary(errors.length, warnings.length)}
          </span>
          {onValidate && (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              disabled={isValidating}
              onClick={() => onValidate()}
            >
              {isValidating ? (
                <Spinner className="size-3.5" />
              ) : (
                <ReloadIcon className="size-3.5" />
              )}
              Check again
            </Button>
          )}
        </div>
      </div>

      {problems.length > 0 && (
        <ul
          aria-label="Problems in the mapping"
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
                  ? 'Show fewer'
                  : `Show the ${problems.length - SHOWN_PROBLEMS} others`}
              </button>
            </li>
          )}
        </ul>
      )}

      <Dialog open={choosing} onOpenChange={setChoosing}>
        <DialogContent className="max-w-4xl">
          <DialogHeader>
            <DialogTitle>Tables copied by this job</DialogTitle>
            <DialogDescription>
              Move to the right the tables the job reads from the source and
              writes to the destinations. Their columns then appear in the
              mapping.
            </DialogDescription>
          </DialogHeader>
          <DualListBox
            options={options}
            selected={selected}
            onChange={onChange}
            mode={mode}
          />
          <div className="flex justify-end">
            <Button type="button" onClick={() => setChoosing(false)}>
              Done
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function problemSummary(errors: number, warnings: number): string {
  if (errors === 0 && warnings === 0) {
    return 'Mapping valid';
  }
  const parts: string[] = [];
  if (errors > 0) {
    parts.push(`${errors} error${errors === 1 ? '' : 's'}`);
  }
  if (warnings > 0) {
    parts.push(`${warnings} warning${warnings === 1 ? '' : 's'}`);
  }
  return parts.join(', ');
}
