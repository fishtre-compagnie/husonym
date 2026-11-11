import { Panel, PanelHeader, StatusDot } from '@/components/ds';
import {
  RUN_STATUS_LABEL,
  RUN_STATUS_TONE,
  TableProgress,
} from '@/components/RunTImeline/events';
import {
  TIMELINE_GRID,
  TimelineAxis,
  TimelineBar,
  TimelineLegend,
} from '@/components/RunTImeline/TimelineTrack';
import { cn } from '@/libs/utils';
import { CodeIcon } from '@radix-ui/react-icons';
import { ReactElement } from 'react';
import { formatSpan } from '../../components/run-format';

interface Props {
  tables: TableProgress[];
  // tables the job maps that the run has not begun yet, while it goes on
  waiting: string[];
  span?: { start: Date; end: Date };
  onViewSelectClicked(schema: string, table: string): void;
  className?: string;
}

/** One line per table: its state and when it was copied, on the run's time axis. */
export default function RunTablesPanel(props: Props): ReactElement {
  const { tables, waiting, span, onViewSelectClicked, className } = props;
  const done = tables.filter((t) => t.status === 'completed').length;
  const running = tables.filter((t) => t.status === 'running').length;
  const failed = tables.filter((t) => t.status === 'failed').length;
  const total = tables.length + waiting.length;
  const spanStart = span?.start ?? new Date();
  const spanTotal = span ? span.end.getTime() - span.start.getTime() : 0;

  const summary = [
    `${done} / ${total} finished`,
    running > 0 ? `${running} in progress` : undefined,
    failed > 0 ? `${failed} failed` : undefined,
  ]
    .filter(Boolean)
    .join(' · ');

  return (
    <Panel aria-label="Tables" className={cn('min-w-0', className)}>
      <PanelHeader
        title="Tables"
        actions={<span className="text-muted-foreground">{summary}</span>}
        className="flex-wrap"
      />
      <div className="overflow-x-auto">
        <div className="min-w-[600px] pb-3 pt-2">
          <TimelineAxis
            spanTotal={spanTotal}
            format={(ms) =>
              ms < 1000 ? `${Math.round(ms)} ms` : formatSpan(ms)
            }
            trailing="time"
          />
          <ul className="m-0 max-h-[420px] list-none overflow-y-auto p-0">
            {tables.map((t) => (
              <li
                key={t.key}
                className={cn(
                  TIMELINE_GRID,
                  'group px-5 py-1.5 hover:bg-hover'
                )}
              >
                <span className="flex min-w-0 items-center gap-2">
                  <StatusDot
                    tone={RUN_STATUS_TONE[t.status]}
                    label={RUN_STATUS_LABEL[t.status]}
                    pulse={t.status === 'running'}
                  />
                  <span
                    className="truncate font-mono text-xs"
                    title={
                      t.error
                        ? `${t.schema}.${t.table}: ${t.error}`
                        : `${t.schema}.${t.table}${t.passes > 1 ? ` (${t.passes} passes)` : ''}`
                    }
                  >
                    {t.schema}.{t.table}
                  </span>
                  <button
                    type="button"
                    onClick={() => onViewSelectClicked(t.schema, t.table)}
                    aria-label={`Show the select query of ${t.schema}.${t.table}`}
                    title="Show the select query"
                    className="ml-auto grid size-6 shrink-0 cursor-pointer place-items-center rounded-md text-muted-foreground opacity-0 transition-opacity hover:bg-surface-2 hover:text-foreground focus-visible:opacity-100 group-hover:opacity-100"
                  >
                    <CodeIcon className="h-3.5 w-3.5" />
                  </button>
                </span>
                <TimelineBar
                  status={t.status}
                  start={t.start}
                  end={t.end}
                  spanStart={spanStart}
                  spanTotal={spanTotal}
                />
                <span
                  className={cn(
                    'text-right font-mono text-xs',
                    t.status === 'failed'
                      ? 'text-danger-fg'
                      : t.status === 'running'
                        ? 'text-link'
                        : 'text-foreground'
                  )}
                >
                  {formatSpan(t.end.getTime() - t.start.getTime())}
                </span>
              </li>
            ))}
            {waiting.map((name) => (
              <li
                key={name}
                className={cn(TIMELINE_GRID, 'px-5 py-1.5 hover:bg-hover')}
              >
                <span className="flex min-w-0 items-center gap-2">
                  <StatusDot tone="quiet" label="Waiting" />
                  <span className="truncate font-mono text-xs text-muted-foreground">
                    {name}
                  </span>
                </span>
                <span className="relative block h-[18px] rounded bg-[repeating-linear-gradient(90deg,var(--line-soft)_0_1px,transparent_1px_25%)]" />
                <span className="text-right font-mono text-xs text-muted-foreground">
                  waiting
                </span>
              </li>
            ))}
          </ul>
          <TimelineLegend className="px-5 pt-3" />
        </div>
      </div>
    </Panel>
  );
}
