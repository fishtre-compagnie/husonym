import { Panel, Pill, StatusDot } from '@/components/ds';
import { TableProgress } from '@/components/RunTImeline/events';
import { cn } from '@/libs/utils';
import { CodeIcon } from '@radix-ui/react-icons';
import Link from 'next/link';
import { ReactElement } from 'react';
import { useRunFormat } from '../../components/run-format';
import { format } from 'date-fns';
import { useTranslations } from 'next-intl';

interface Props {
  tables: TableProgress[];
  // tables the job maps that the run has not begun yet
  waiting: string[];
  // the time limit of the run (the job's run timeout), none when the job sets none
  runLimitMs?: number;
  // where the limit is changed: the job's page
  jobHref?: string;
  now: Date;
  onViewSelectClicked(schema: string, table: string): void;
}

// So many waiting tables are named, and so many finished ones listed.
const UP_NEXT = 4;
const JUST_FINISHED = 3;

const ROW_GRID = 'grid grid-cols-[minmax(0,1fr)_120px_32px] items-center gap-5';

/**
 * While the run goes on: the tables it is copying now and how long each has run. A table
 * has no time limit of its own: it is stopped when it no longer makes progress, and the
 * run's limit bounds them all. Below, what comes next and what just ended.
 */
export default function RunInProgressPanel(props: Props): ReactElement {
  const { tables, waiting, runLimitMs, jobHref, now, onViewSelectClicked } =
    props;
  const tr = useTranslations('runs.inProgress');
  const fmt = useRunFormat();

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
    <Panel aria-label={tr('label')}>
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2 border-b border-line-soft px-5 py-4">
        <h2 className="m-0 font-display text-base font-semibold">
          {tr('title')}
        </h2>
        <Pill tone="live">{tr('tables', { count: running.length })}</Pill>
        <span className="ml-auto text-[13px] text-muted-foreground">
          {runLimitMs ? (
            <>
              {tr('runLimit')}{' '}
              <span className="font-medium text-foreground">
                {fmt.span(runLimitMs)}
              </span>
            </>
          ) : (
            tr('noRunLimit')
          )}
          {jobHref && (
            <>
              {' · '}
              <Link href={jobHref} className="text-link hover:underline">
                {runLimitMs ? tr('change') : tr('setOne')}
              </Link>
            </>
          )}
        </span>
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
          <Count fill="bg-quiet" count={finished} label={tr('finished')} />
          <Count
            fill="bg-primary"
            count={running.length}
            label={tr('inProgress')}
          />
          {failed > 0 && (
            <Count
              fill="bg-danger"
              count={failed}
              label={tr('failed')}
              countClass="text-danger-fg"
            />
          )}
          <Count
            fill="border border-border bg-surface-2"
            count={waiting.length}
            label={tr('waiting')}
          />
        </ul>
      </div>

      {running.length === 0 ? (
        <p className="m-0 px-5 py-6 text-center text-muted-foreground">
          {tr('nothingRunning')}
        </p>
      ) : (
        <div className="overflow-x-auto">
          <div className="min-w-[420px]">
            <div
              className={cn(
                ROW_GRID,
                'px-5 pb-2 pt-4 text-[11px] uppercase tracking-[0.06em] text-muted-foreground'
              )}
            >
              <span>{tr('table')}</span>
              <span className="text-right">{tr('runningFor')}</span>
              <span className="sr-only">{tr('actions')}</span>
            </div>
            <ul className="m-0 list-none p-0">
              {running.map(({ t, since }) => {
                const elapsed = Math.max(0, now.getTime() - since.getTime());
                return (
                  <li
                    key={t.key}
                    className={cn(
                      ROW_GRID,
                      'border-t border-line-soft px-5 py-3'
                    )}
                  >
                    <div className="flex min-w-0 flex-col gap-0.5">
                      <div className="flex min-w-0 items-center gap-2">
                        <StatusDot tone="live" label={tr('running')} pulse />
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
                            {tr('pass', { count: t.passes })}
                          </span>
                        )}
                      </div>
                      <span className="pl-4 text-xs text-muted-foreground">
                        {tr('startedAt', { time: format(since, 'HH:mm:ss') })}
                      </span>
                    </div>
                    <span className="text-right font-mono text-sm font-medium text-link">
                      {fmt.span(elapsed)}
                    </span>
                    <button
                      type="button"
                      onClick={() => onViewSelectClicked(t.schema, t.table)}
                      aria-label={tr('showSelectOf', { table: t.key })}
                      title={tr('showSelect')}
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
              {tr('upNext')}
            </h3>
            {waiting.length === 0 ? (
              <p className="m-0 mt-2.5 text-xs text-muted-foreground">
                {tr('allBegun')}
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
                    {tr('more', { count: waiting.length - UP_NEXT })}
                  </span>
                )}
              </div>
            )}
          </div>
          <div className="px-5 py-4">
            <h3 className="m-0 text-[11px] font-normal uppercase tracking-[0.06em] text-muted-foreground">
              {tr('justFinished')}
            </h3>
            {ended.length === 0 ? (
              <p className="m-0 mt-2.5 text-xs text-muted-foreground">
                {tr('noneFinished')}
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
                      label={
                        t.status === 'failed' ? tr('failed') : tr('completed')
                      }
                    />
                    <span className="min-w-0 flex-1 truncate">{t.key}</span>
                    <span>
                      {t.status === 'completed'
                        ? fmt.span(t.end.getTime() - t.start.getTime())
                        : tr(`endedStatus.${t.status}`)}
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
          {tr('seeTimeline')}
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
