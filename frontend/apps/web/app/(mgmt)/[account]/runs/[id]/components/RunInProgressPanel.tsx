import { Panel, Pill, StatusDot } from '@/components/ds';
import { TableProgress } from '@/components/RunTImeline/events';
import { cn } from '@/libs/utils';
import { CodeIcon } from '@radix-ui/react-icons';
import { format } from 'date-fns';
import Link from 'next/link';
import { ReactElement } from 'react';
import { formatSpan } from '../../components/run-format';

/** How long one pass of a table may run before the run gives it up. */
export interface TableLimit {
  ms: number;
  // the job sets none: the worker's default applies
  isDefault: boolean;
}

interface Props {
  tables: TableProgress[];
  // tables the job maps that the run has not begun yet
  waiting: string[];
  limit?: TableLimit;
  // where the limit is changed: the job's page
  jobHref?: string;
  now: Date;
  onViewSelectClicked(schema: string, table: string): void;
}

// So many waiting tables are named, and so many finished ones listed.
const UP_NEXT = 4;
const JUST_FINISHED = 3;

const ROW_GRID =
  'grid grid-cols-[minmax(0,1fr)_120px_minmax(180px,320px)_32px] items-center gap-5';

/**
 * While the run goes on: the tables it is copying now, how long each has run, and how
 * much of the limit per table that is, so a table about to time out shows before it
 * does. Below, what comes next and what just ended.
 */
export default function RunInProgressPanel(props: Props): ReactElement {
  const { tables, waiting, limit, jobHref, now, onViewSelectClicked } = props;

  const running = tables
    .filter((t) => t.status === 'running')
    .map((t) => ({ t, since: t.runningSince ?? t.start }))
    .sort((a, b) => a.since.getTime() - b.since.getTime());
  const finished = tables.filter((t) => t.status === 'completed').length;
  const failed = tables.filter((t) => t.status === 'failed').length;
  const ended = tables
    .filter((t) => t.status !== 'running')
    .sort((a, b) => b.end.getTime() - a.end.getTime())
    .slice(0, JUST_FINISHED);
  const total = tables.length + waiting.length;

  return (
    <Panel aria-label="Tables in progress">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2 border-b border-line-soft px-5 py-4">
        <h2 className="m-0 font-display text-base font-semibold">
          In progress
        </h2>
        <Pill tone="brand">
          {running.length} {running.length === 1 ? 'table' : 'tables'}
        </Pill>
        {limit && (
          <span className="ml-auto text-[13px] text-muted-foreground">
            Limit per table:{' '}
            <span className="font-medium text-foreground">
              {formatSpan(limit.ms)}
            </span>
            {limit.isDefault && ' (default)'}
            {jobHref && (
              <>
                {' · '}
                <Link href={jobHref} className="text-link hover:underline">
                  change
                </Link>
              </>
            )}
          </span>
        )}
      </div>

      <div className="px-5 pb-1.5 pt-4">
        <div
          aria-hidden="true"
          className="flex h-2 overflow-hidden rounded-full bg-surface-2"
        >
          <span
            className="bg-quiet"
            style={{ width: share(finished, total) }}
          />
          <span
            className="bg-primary"
            style={{ width: share(running.length, total) }}
          />
          <span className="bg-danger" style={{ width: share(failed, total) }} />
        </div>
        <ul className="m-0 mt-2.5 flex list-none flex-wrap gap-x-5 gap-y-1.5 p-0 text-[13px] text-muted-foreground">
          <Count fill="bg-quiet" count={finished} label="finished" />
          <Count fill="bg-primary" count={running.length} label="in progress" />
          {failed > 0 && (
            <Count
              fill="bg-danger"
              count={failed}
              label="failed"
              countClass="text-danger-fg"
            />
          )}
          <Count
            fill="border border-border bg-surface-2"
            count={waiting.length}
            label="waiting"
          />
        </ul>
      </div>

      {running.length === 0 ? (
        <p className="m-0 px-5 py-6 text-center text-muted-foreground">
          No table is being copied right now.
        </p>
      ) : (
        <div className="overflow-x-auto">
          <div className="min-w-[640px]">
            <div
              className={cn(
                ROW_GRID,
                'px-5 pb-2 pt-4 text-[11px] uppercase tracking-[0.06em] text-muted-foreground'
              )}
            >
              <span>Table</span>
              <span className="text-right">Running for</span>
              <span>{limit ? 'Share of the limit' : ''}</span>
              <span className="sr-only">Actions</span>
            </div>
            <ul className="m-0 list-none p-0">
              {running.map(({ t, since }) => {
                const elapsed = Math.max(0, now.getTime() - since.getTime());
                const ratio = limit ? elapsed / limit.ms : 0;
                const tone =
                  ratio >= 0.8 ? 'danger' : ratio >= 0.5 ? 'warn' : 'brand';
                const left = limit ? limit.ms - elapsed : 0;
                return (
                  <li
                    key={t.key}
                    className={cn(
                      ROW_GRID,
                      'border-t border-line-soft px-5 py-3',
                      tone === 'danger' && 'bg-danger-soft'
                    )}
                  >
                    <div className="flex min-w-0 flex-col gap-0.5">
                      <div className="flex min-w-0 items-center gap-2">
                        <StatusDot tone="brand" label="Running" pulse />
                        <span
                          className="min-w-0 truncate font-mono text-[13px]"
                          title={t.key}
                        >
                          <span className="text-muted-foreground">
                            {t.schema}.
                          </span>
                          <span className="text-hi">{t.table}</span>
                        </span>
                        {t.passes > 1 && (
                          <span className="shrink-0 rounded-md bg-surface-2 px-1.5 py-px text-[11px] text-muted-foreground">
                            pass {t.passes}
                          </span>
                        )}
                      </div>
                      <span className="pl-4 text-xs text-muted-foreground">
                        started {format(since, 'HH:mm:ss')}
                      </span>
                    </div>
                    <span
                      className={cn(
                        'text-right font-mono text-sm font-medium',
                        tone === 'danger'
                          ? 'text-danger-fg'
                          : tone === 'warn'
                            ? 'text-warn-fg'
                            : 'text-link'
                      )}
                    >
                      {formatSpan(elapsed)}
                    </span>
                    {limit ? (
                      <div className="flex flex-col gap-1">
                        <div className="h-1.5 overflow-hidden rounded-full bg-surface-2">
                          <div
                            className={cn(
                              'progress-flow h-full min-w-1 rounded-full',
                              tone === 'danger'
                                ? 'bg-danger'
                                : tone === 'warn'
                                  ? 'bg-warn'
                                  : 'bg-primary'
                            )}
                            style={{
                              width: `${Math.min(100, ratio * 100)}%`,
                            }}
                          />
                        </div>
                        <span
                          className={cn(
                            'text-xs',
                            tone === 'danger'
                              ? 'text-danger-fg'
                              : tone === 'warn'
                                ? 'text-warn-fg'
                                : 'text-muted-foreground'
                          )}
                        >
                          {Math.floor(ratio * 100)} %
                          {tone === 'danger'
                            ? left > 0
                              ? ` · times out in ${formatSpan(left)}`
                              : ' · past the limit'
                            : ` of ${formatSpan(limit.ms)}`}
                        </span>
                      </div>
                    ) : (
                      <span />
                    )}
                    <button
                      type="button"
                      onClick={() => onViewSelectClicked(t.schema, t.table)}
                      aria-label={`Show the select query of ${t.key}`}
                      title="Show the select query"
                      className="grid size-8 cursor-pointer place-items-center rounded-lg text-muted-foreground hover:bg-surface-2 hover:text-foreground"
                    >
                      <CodeIcon className="size-3.5" />
                    </button>
                  </li>
                );
              })}
            </ul>
          </div>
        </div>
      )}

      {(waiting.length > 0 || ended.length > 0) && (
        <div className="grid grid-cols-[repeat(auto-fit,minmax(280px,1fr))] border-t border-line-soft bg-surface-2/40">
          <div className="border-line-soft px-5 py-4 [&:not(:last-child)]:border-r">
            <h3 className="m-0 text-[11px] font-normal uppercase tracking-[0.06em] text-muted-foreground">
              Up next
            </h3>
            {waiting.length === 0 ? (
              <p className="m-0 mt-2.5 text-xs text-muted-foreground">
                Every table has begun.
              </p>
            ) : (
              <div className="mt-2.5 flex flex-wrap gap-1.5">
                {waiting.slice(0, UP_NEXT).map((name) => (
                  <span
                    key={name}
                    className="rounded-md border border-dashed border-border px-2 py-0.5 font-mono text-xs text-muted-foreground"
                  >
                    {name}
                  </span>
                ))}
                {waiting.length > UP_NEXT && (
                  <span className="px-1 py-0.5 text-xs text-muted-foreground">
                    +{waiting.length - UP_NEXT} more
                  </span>
                )}
              </div>
            )}
          </div>
          <div className="px-5 py-4">
            <h3 className="m-0 text-[11px] font-normal uppercase tracking-[0.06em] text-muted-foreground">
              Just finished
            </h3>
            {ended.length === 0 ? (
              <p className="m-0 mt-2.5 text-xs text-muted-foreground">
                No table has finished yet.
              </p>
            ) : (
              <ul className="m-0 mt-2 flex list-none flex-col gap-1.5 p-0">
                {ended.map((t) => (
                  <li
                    key={t.key}
                    className={cn(
                      'flex items-center gap-2 font-mono text-xs',
                      t.status === 'failed' && 'text-danger-fg'
                    )}
                    title={t.error ? `${t.key}: ${t.error}` : t.key}
                  >
                    <StatusDot
                      tone={t.status === 'failed' ? 'danger' : 'quiet'}
                      label={t.status === 'failed' ? 'Failed' : 'Completed'}
                    />
                    <span className="min-w-0 flex-1 truncate">{t.key}</span>
                    <span>
                      {t.status === 'completed'
                        ? formatSpan(t.end.getTime() - t.start.getTime())
                        : t.status}
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </div>
      )}

      <div className="border-t border-line-soft px-5 py-3 text-[13px]">
        <a href="#run-activity" className="text-link hover:underline">
          See every table in the Activity timeline ↓
        </a>
      </div>
    </Panel>
  );
}

function share(count: number, total: number): string {
  return total > 0 ? `${(count / total) * 100}%` : '0%';
}

function Count(props: {
  fill: string;
  count: number;
  label: string;
  countClass?: string;
}): ReactElement {
  const { fill, count, label, countClass } = props;
  return (
    <li className="flex items-center gap-1.5">
      <span aria-hidden="true" className={cn('size-2 rounded-sm', fill)} />
      <span className={cn('font-semibold text-foreground', countClass)}>
        {count}
      </span>
      {label}
    </li>
  );
}
