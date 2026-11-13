import { Panel, PanelHeader } from '@/components/ds';
import { TableProgress } from '@/components/RunTImeline/events';
import { cn } from '@/libs/utils';
import { CodeIcon } from '@radix-ui/react-icons';
import { ReactElement } from 'react';
import { formatSpan } from '../../components/run-format';

interface Props {
  tables: TableProgress[];
  onViewSelectClicked(schema: string, table: string): void;
}

// So many tables are listed.
const SHOWN = 5;

const ROW_GRID =
  'grid grid-cols-[24px_minmax(0,1fr)_minmax(140px,280px)_96px_32px] items-center gap-4';

/**
 * Once the run is over: the tables that took it longest, where a filter or an index on
 * the source saves the most time on the next one.
 */
export default function SlowestTablesPanel(props: Props): ReactElement {
  const { tables, onViewSelectClicked } = props;
  const timed = tables.map((t) => ({
    t,
    ms: t.end.getTime() - t.start.getTime(),
  }));
  const slowest = [...timed].sort((a, b) => b.ms - a.ms).slice(0, SHOWN);
  const longest = slowest[0]?.ms ?? 0;
  const all = timed.reduce((sum, { ms }) => sum + ms, 0);
  const top = slowest.reduce((sum, { ms }) => sum + ms, 0);

  return (
    <Panel aria-label="Slowest tables">
      <PanelHeader
        title="Slowest tables"
        className="flex-wrap"
        actions={
          all > 0 && tables.length > SHOWN ? (
            <span className="text-muted-foreground">
              {tables.length} tables · the {SHOWN} longest take{' '}
              {Math.round((top / all) * 100)} % of the time spent copying
            </span>
          ) : undefined
        }
      />
      <div className="overflow-x-auto">
        <div className="min-w-[600px]">
          <div
            className={cn(
              ROW_GRID,
              'px-5 pb-2 pt-4 text-[11px] uppercase tracking-[0.06em] text-muted-foreground'
            )}
          >
            <span>#</span>
            <span>Table</span>
            <span>Compared with the longest</span>
            <span className="text-right">Duration</span>
            <span className="sr-only">Actions</span>
          </div>
          <ol className="m-0 list-none p-0">
            {slowest.map(({ t, ms }, i) => (
              <li
                key={t.key}
                className={cn(
                  ROW_GRID,
                  'border-t border-line-soft px-5 py-2.5'
                )}
              >
                <span className="font-mono text-xs text-muted-foreground">
                  {i + 1}
                </span>
                <span
                  className={cn(
                    'min-w-0 truncate font-mono text-[13px]',
                    t.status === 'failed' && 'text-danger-fg'
                  )}
                  title={t.error ? `${t.key}: ${t.error}` : t.key}
                >
                  <span className="text-muted-foreground">{t.schema}.</span>
                  <span className={t.status === 'failed' ? '' : 'text-hi'}>
                    {t.table}
                  </span>
                  {t.passes > 1 && (
                    <span className="ml-2 rounded-md bg-surface-2 px-1.5 py-px font-sans text-[11px] text-muted-foreground">
                      {t.passes} passes
                    </span>
                  )}
                </span>
                <div className="h-1.5 overflow-hidden rounded-full bg-surface-2">
                  <div
                    className={cn(
                      'h-full rounded-full',
                      t.status === 'failed' ? 'bg-danger' : 'bg-quiet'
                    )}
                    style={{
                      width: `${longest > 0 ? (ms / longest) * 100 : 0}%`,
                    }}
                  />
                </div>
                <span className="text-right font-mono text-[13px] font-medium">
                  {formatSpan(ms)}
                </span>
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
            ))}
          </ol>
        </div>
      </div>
      <div className="border-t border-line-soft px-5 py-3 text-[13px]">
        <a href="#run-activity" className="text-link hover:underline">
          See every table in the Activity timeline ↓
        </a>
      </div>
    </Panel>
  );
}
