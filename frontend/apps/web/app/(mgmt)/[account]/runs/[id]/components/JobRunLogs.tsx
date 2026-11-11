import LicenseFeatureNotice from '@/components/LicenseFeatureNotice';
import { Panel, PanelHeader } from '@/components/ds';
import { Button } from '@/components/ui/button';
import { useRunLogsHidden } from '@/libs/hooks/useLicense';
import { cn } from '@/libs/utils';
import { timestampDate } from '@bufbuild/protobuf/wkt';
import { useQuery } from '@connectrpc/connect-query';
import {
  GetJobRunLogsResponse_LogLine,
  JobService,
  LogLevel,
  LogWindow,
} from '@husonym/sdk';
import { ReloadIcon } from '@radix-ui/react-icons';
import { format } from 'date-fns';
import { ReactElement, useEffect, useMemo, useRef, useState } from 'react';

interface JobRunLogsProps {
  accountId: string;
  runId: string;
  isRunning: boolean;
  className?: string;
}

const TEN_SECONDS = 5 * 1000;

const LEVELS: { value: LogLevel; label: string }[] = [
  { value: LogLevel.UNSPECIFIED, label: 'All' },
  { value: LogLevel.INFO, label: 'Info' },
  { value: LogLevel.WARN, label: 'Warn' },
  { value: LogLevel.ERROR, label: 'Error' },
];

/**
 * The run's log, read like a terminal. It keeps the dark terminal look in both
 * themes: the panel takes the dark tokens through the `dark` class.
 */
export default function JobRunLogs({
  accountId,
  runId,
  isRunning,
  className,
}: JobRunLogsProps): ReactElement {
  const hidden = useRunLogsHidden();
  const [selectedLogLevel, setSelectedLogLevel] = useState<LogLevel>(
    LogLevel.UNSPECIFIED
  );
  const [follow, setFollow] = useState(true);
  const scrollRef = useRef<HTMLDivElement>(null);

  const {
    data: logsData,
    isLoading: isLogsLoading,
    isFetching: isLogsValidating,
    error: logsError,
    refetch: logsMutate,
  } = useQuery(
    JobService.method.getJobRunLogs,
    {
      accountId,
      jobRunId: runId,
      logLevels: [selectedLogLevel],
      maxLogLines: BigInt(5000),
      window: LogWindow.ONE_DAY,
    },
    {
      // The API refuses the logs under a license in force that lacks the feature: do
      // not ask then. Under a license that has lapsed, or none, it serves them.
      enabled: !!runId && !!accountId && !hidden,
      refetchInterval(query) {
        return query.state.data && isRunning ? TEN_SECONDS : 0;
      },
    }
  );
  const lines = useMemo(
    () => (logsData?.logLines ?? []).map(toDisplayLine),
    [logsData?.logLines]
  );
  const errorCount = useMemo(
    () => lines.filter((l) => l.level === 'ERROR').length,
    [lines]
  );

  // follows the end of the log as lines come in
  useEffect(() => {
    if (follow && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [lines, follow]);

  function onRefreshClick(): void {
    if (!isLogsValidating) {
      logsMutate();
    }
  }

  if (hidden) {
    return (
      <Panel aria-label="Logs" className={className}>
        <PanelHeader title="Logs" />
        <div className="p-5">
          <LicenseFeatureNotice feature="run_logs" />
        </div>
      </Panel>
    );
  }

  return (
    <section
      aria-label="Logs"
      className={cn(
        'dark flex flex-col overflow-hidden rounded-2xl border border-line-soft bg-background text-foreground',
        className
      )}
    >
      <div className="flex flex-wrap items-center gap-2 border-b border-line-soft bg-card px-3.5 py-3">
        <h2 className="m-0 mr-1.5 font-display text-base font-semibold">
          Logs
        </h2>
        <div
          role="group"
          aria-label="Log level"
          className="flex flex-wrap gap-1.5"
        >
          {LEVELS.map((l) => {
            const pressed = l.value === selectedLogLevel;
            return (
              <button
                key={l.value}
                type="button"
                aria-pressed={pressed}
                onClick={() => setSelectedLogLevel(l.value)}
                className={cn(
                  'cursor-pointer rounded-full border px-2.5 py-0.5 text-xs transition-colors',
                  pressed
                    ? 'border-primary bg-brand-soft text-foreground'
                    : 'border-border text-muted-foreground hover:text-foreground'
                )}
              >
                {l.label}
              </button>
            );
          })}
        </div>
        <div className="ml-auto flex items-center gap-2">
          <label className="flex cursor-pointer items-center gap-1.5 text-xs text-muted-foreground">
            <input
              type="checkbox"
              checked={follow}
              onChange={(e) => setFollow(e.target.checked)}
              className="accent-primary"
            />
            Follow
          </label>
          <Button
            disabled={isLogsValidating}
            variant="ghost"
            size="icon"
            className="h-8 w-8"
            aria-label="Refresh logs"
            onClick={() => onRefreshClick()}
          >
            <ReloadIcon
              className={cn('h-4 w-4', isLogsValidating && 'animate-spin')}
            />
          </Button>
        </div>
      </div>
      {(logsError || errorCount > 0) && (
        <div className="border-b border-line-soft bg-danger-soft px-3.5 py-2 text-xs text-danger-fg">
          {logsError
            ? logsError.message
            : `${errorCount} error ${errorCount === 1 ? 'line' : 'lines'} in this log: filter by Error to see them alone.`}
        </div>
      )}
      <div
        ref={scrollRef}
        role="log"
        aria-live="polite"
        aria-busy={isLogsLoading}
        className="flex max-h-[460px] min-h-[200px] flex-col gap-1 overflow-y-auto px-3.5 py-3 font-mono text-xs leading-relaxed"
      >
        {isLogsLoading && (
          <span className="text-muted-foreground">Loading the log…</span>
        )}
        {!isLogsLoading && lines.length === 0 && !logsError && (
          <span className="text-muted-foreground">
            No log line
            {selectedLogLevel !== LogLevel.UNSPECIFIED
              ? ' at this level'
              : ' yet'}
            .
          </span>
        )}
        {lines.map((l, i) => (
          <div
            key={i}
            className="grid grid-cols-[64px_44px_minmax(0,1fr)] gap-2"
            title={l.labels || undefined}
          >
            <span className="text-muted-foreground">{l.time}</span>
            <span className={levelClass(l.level)}>{l.level}</span>
            <span className="whitespace-pre-wrap break-words text-foreground">
              {l.message}
            </span>
          </div>
        ))}
        {isRunning && (
          <span
            aria-hidden="true"
            className="animate-[pulse-soft_1s_steps(2)_infinite] text-link"
          >
            ▍
          </span>
        )}
      </div>
    </section>
  );
}

interface DisplayLine {
  time: string;
  level: string;
  message: string;
  labels: string;
}

function levelClass(level: string): string {
  switch (level) {
    case 'ERROR':
      return 'text-danger-fg';
    case 'WARN':
      return 'text-warn-fg';
    case 'INFO':
      return 'text-ok';
    default:
      return 'text-muted-foreground';
  }
}

/** A raw log line split into its time, level and message, as far as it tells them. */
function toDisplayLine(line: GetJobRunLogsResponse_LogLine): DisplayLine {
  const raw = line.logLine;
  let level = '';
  let message = raw;
  try {
    const parsed: unknown = JSON.parse(raw);
    if (parsed && typeof parsed === 'object') {
      const obj = parsed as Record<string, unknown>;
      if (typeof obj.level === 'string') level = obj.level;
      if (typeof obj.msg === 'string') {
        // the message first, then the fields that came with it
        const rest = Object.entries(obj)
          .filter(([k]) => !['time', 'level', 'msg'].includes(k))
          .map(
            ([k, v]) => `${k}=${typeof v === 'string' ? v : JSON.stringify(v)}`
          );
        message = [obj.msg, ...rest].join('  ');
      }
    }
  } catch {
    const match = raw.match(/^\s*\[?(DEBUG|INFO|WARN(?:ING)?|ERROR)\]?\s*/i);
    if (match) {
      level = match[1];
      message = raw.slice(match[0].length);
    }
  }
  if (!level && line.labels?.level) {
    level = line.labels.level;
  }
  level = level.toUpperCase().replace('WARNING', 'WARN');
  const labelEntries = Object.entries(line.labels ?? {});
  return {
    time: line.timestamp
      ? format(timestampDate(line.timestamp), 'HH:mm:ss')
      : '',
    level,
    message,
    labels: labelEntries.map(([k, v]) => `${k}=${v}`).join('  '),
  };
}
