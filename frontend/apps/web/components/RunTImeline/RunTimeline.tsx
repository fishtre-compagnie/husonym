import { StatusDot } from '@/components/ds';
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip';
import { cn } from '@/libs/utils';
import { Timestamp } from '@bufbuild/protobuf/wkt';
import { JobRunEvent, JobRunStatus as JobRunStatusEnum } from '@husonym/sdk';
import {
  ChevronDownIcon,
  Cross2Icon,
  MixerHorizontalIcon,
} from '@radix-ui/react-icons';
import { format } from 'date-fns';
import { ReactElement, useMemo, useState } from 'react';
import { Button } from '../ui/button';
import {
  convertTimestampToDate,
  getEventsSpan,
  getEventWindow,
  getScheduledTime,
  getSyncTable,
  humanizeActivityType,
  RUN_STATUS_LABEL,
  RUN_STATUS_TONE,
  RunStatus,
} from './events';
import {
  TIMELINE_GRID,
  TimelineAxis,
  TimelineBar,
  TimelineLegend,
} from './TimelineTrack';

interface Props {
  jobRunEvents: JobRunEvent[];
  jobStatus?: JobRunStatusEnum;
}

const INITIAL_STATUSES: RunStatus[] = [
  'running',
  'completed',
  'failed',
  'canceled',
  'terminated',
];

/** Every activity of the run on one time axis; a row opens on its steps. */
export default function RunTimeline(props: Props): ReactElement {
  const { jobRunEvents, jobStatus } = props;
  const [expandedTaskId, setExpandedTaskId] = useState<string | null>(null);
  const [selectedStatuses, setSelectedStatuses] =
    useState<RunStatus[]>(INITIAL_STATUSES);

  const span = useMemo(
    () => getEventsSpan(jobRunEvents, jobStatus),
    [jobRunEvents, jobStatus]
  );

  // handles filtering the tasks when the tasks or filters change
  const filteredJobRunEvents = useMemo(() => {
    return jobRunEvents.filter((jobRunEvent) => {
      const { status } = getEventWindow(jobRunEvent, jobStatus);
      return selectedStatuses.includes(status);
    });
  }, [jobRunEvents, selectedStatuses, jobStatus]);

  function handleStatusFilterChange(status: RunStatus, checked: boolean) {
    setSelectedStatuses((prev) =>
      checked ? [...prev, status] : prev.filter((s) => s !== status)
    );
  }

  function toggleExpandedRowBody(taskId: string) {
    setExpandedTaskId((prevId) => (prevId === taskId ? null : taskId));
  }

  const spanStart = span?.start ?? new Date();
  const spanTotal = span ? span.end.getTime() - span.start.getTime() : 0;

  return (
    <div className="flex flex-col">
      <div className="flex flex-wrap items-center gap-3 px-5 py-3">
        <span className="text-[13px] text-muted-foreground">
          {filteredJobRunEvents.length === jobRunEvents.length
            ? `${jobRunEvents.length} activities`
            : `${filteredJobRunEvents.length} of ${jobRunEvents.length} activities`}
        </span>
        <TimelineLegend className="hidden md:flex" />
        <div className="ml-auto">
          <StatusFilter
            selectedStatuses={selectedStatuses}
            onStatusChange={handleStatusFilterChange}
            onReset={() => setSelectedStatuses(INITIAL_STATUSES)}
            onClear={() => setSelectedStatuses([])}
          />
        </div>
      </div>
      <div className="max-h-[440px] overflow-auto border-t border-line-soft">
        <div className="min-w-[640px]">
          <TimelineAxis
            spanTotal={spanTotal}
            format={formatElapsed}
            trailing="duration"
            className="sticky top-0 z-[1] bg-card pt-2"
          />
          {filteredJobRunEvents.length === 0 && (
            <p className="px-5 py-6 text-center text-[13px] text-muted-foreground">
              No activity in the selected states.
            </p>
          )}
          <ul className="m-0 list-none p-0 pb-2">
            {filteredJobRunEvents.map((jobRunEvent) => (
              <TimelineRow
                key={String(jobRunEvent.id)}
                jobRunEvent={jobRunEvent}
                jobStatus={jobStatus}
                spanStart={spanStart}
                spanTotal={spanTotal}
                isExpanded={expandedTaskId === String(jobRunEvent.id)}
                onToggle={() => toggleExpandedRowBody(String(jobRunEvent.id))}
              />
            ))}
          </ul>
        </div>
      </div>
    </div>
  );
}

interface TimelineRowProps {
  jobRunEvent: JobRunEvent;
  jobStatus: JobRunStatusEnum | undefined;
  spanStart: Date;
  spanTotal: number;
  isExpanded: boolean;
  onToggle(): void;
}

function TimelineRow(props: TimelineRowProps): ReactElement {
  const { jobRunEvent, jobStatus, spanStart, spanTotal, isExpanded, onToggle } =
    props;
  const { start, end, status } = getEventWindow(jobRunEvent, jobStatus);
  const table = getSyncTable(jobRunEvent);
  const failedTask = jobRunEvent.tasks.find((item) => item.error);
  const stopped =
    status === 'failed' || status === 'canceled' || status === 'terminated';
  const bodyId = `activity-${jobRunEvent.id}-steps`;

  return (
    <li>
      <TooltipProvider delayDuration={150}>
        <Tooltip>
          <TooltipTrigger asChild>
            <button
              type="button"
              aria-expanded={isExpanded}
              aria-controls={bodyId}
              onClick={onToggle}
              className={cn(
                TIMELINE_GRID,
                'w-full cursor-pointer px-5 py-1.5 text-left transition-colors hover:bg-hover',
                isExpanded && 'bg-hover'
              )}
            >
              <span
                className={cn(
                  'flex min-w-0 items-center gap-2',
                  table && 'pl-3.5'
                )}
              >
                <StatusDot
                  tone={RUN_STATUS_TONE[status]}
                  label={RUN_STATUS_LABEL[status]}
                  pulse={status === 'running'}
                />
                {table ? (
                  <span className="truncate font-mono text-xs">
                    {table.schema}.{table.table}
                  </span>
                ) : (
                  <span className="truncate text-[13px]">
                    {humanizeActivityType(jobRunEvent.type)}
                  </span>
                )}
                <ChevronDownIcon
                  aria-hidden="true"
                  className={cn(
                    'ml-auto h-3.5 w-3.5 shrink-0 text-muted-foreground transition-transform',
                    isExpanded && 'rotate-180'
                  )}
                />
              </span>
              <TimelineBar
                status={status}
                start={start}
                end={end}
                spanStart={spanStart}
                spanTotal={spanTotal}
              />
              <span
                className={cn(
                  'text-right font-mono text-xs',
                  status === 'failed'
                    ? 'text-danger-fg'
                    : status === 'running'
                      ? 'text-link'
                      : 'text-muted-foreground'
                )}
              >
                {formatTaskDuration(getScheduledTime(jobRunEvent), end)}
              </span>
            </button>
          </TooltipTrigger>
          <TooltipContent
            align="center"
            className="flex max-w-[360px] flex-col gap-1 text-xs"
          >
            <span className="font-medium">
              {jobRunEvent.id.toString()}. {jobRunEvent.type}
            </span>
            {table && (
              <span className="font-mono">
                {table.schema}.{table.table}
              </span>
            )}
            <span>Start: {formatFullDate(start)}</span>
            <span>Finish: {stopped ? 'N/A' : formatFullDate(end)}</span>
            {failedTask && (
              <span className="text-danger-fg">
                Error: {failedTask.error?.message || 'Unknown error'}
              </span>
            )}
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>
      {isExpanded && (
        <div id={bodyId}>
          <ExpandedRowBody task={jobRunEvent} />
        </div>
      )}
    </li>
  );
}

interface StatusFilterProps {
  selectedStatuses: RunStatus[];
  onStatusChange: (status: RunStatus, checked: boolean) => void;
  onReset(): void;
  onClear(): void;
}

function StatusFilter({
  selectedStatuses,
  onStatusChange,
  onReset,
  onClear,
}: StatusFilterProps) {
  const uniqueSelectedStatuses = new Set(selectedStatuses);

  const isFiltered = selectedStatuses.length < INITIAL_STATUSES.length;
  return (
    <div className="flex flex-row items-center gap-2">
      {isFiltered && (
        <Button variant="ghost" size="sm" onClick={onReset} className="h-8">
          Reset
          <Cross2Icon className="h-3.5 w-3.5" />
        </Button>
      )}
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="outline" size="sm" type="button" className="h-8">
            <MixerHorizontalIcon className="h-3.5 w-3.5" />
            Status
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent className="w-56" align="end">
          <DropdownMenuItem
            onSelect={(e) => {
              e.preventDefault();
              onReset();
            }}
          >
            Select all
          </DropdownMenuItem>
          <DropdownMenuItem
            onSelect={(e) => {
              e.preventDefault();
              onClear();
            }}
          >
            Deselect all
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          {INITIAL_STATUSES.map((s) => (
            <DropdownMenuCheckboxItem
              key={s}
              checked={uniqueSelectedStatuses.has(s)}
              onCheckedChange={(checked) => onStatusChange(s, checked)}
              onSelect={(e) => e.preventDefault()}
            >
              <span className="flex items-center gap-2">
                <StatusDot tone={RUN_STATUS_TONE[s]} />
                {RUN_STATUS_LABEL[s]}
              </span>
            </DropdownMenuCheckboxItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}

function formatFullDate(date: Date | undefined): string {
  if (!date) return 'N/A';
  return format(date, 'MM/dd/yyyy HH:mm:ss.SSS');
}

/** Elapsed time on the axis: 120 ms, 4.2 s, 2:05, 1:02:05. */
function formatElapsed(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)} ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(ms < 10_000 ? 1 : 0)} s`;
  const total = Math.round(ms / 1000);
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = String(total % 60).padStart(2, '0');
  return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${s}` : `${m}:${s}`;
}

function formatTaskDuration(
  s: Timestamp | undefined,
  end: Date | undefined
): string {
  if (!s || !end) return 'N/A';
  const ms = end.getTime() - convertTimestampToDate(s).getTime();
  return formatElapsedLong(ms);
}

function formatElapsedLong(ms: number): string {
  if (ms < 0) return 'N/A';
  if (ms < 1000) return `${Math.round(ms)} ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(ms < 10_000 ? 1 : 0)} s`;
  const total = Math.floor(ms / 1000);
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  return h > 0 ? `${h} h ${m} min` : `${m} min ${s} s`;
}

function getStepLabel(type: string): string {
  switch (type) {
    case 'ActivityTaskScheduled':
    case 'StartChildWorkflowExecutionInitiated':
      return 'Scheduled';
    case 'ActivityTaskStarted':
    case 'ChildWorkflowExecutionStarted':
      return 'Started';
    case 'ActivityTaskCompleted':
    case 'ChildWorkflowExecutionCompleted':
      return 'Completed';
    case 'ActivityTaskFailed':
    case 'ChildWorkflowExecutionFailed':
    case 'StartChildWorkflowExecutionFailed':
      return 'Failed';
    case 'ActivityTaskTimedOut':
    case 'ChildWorkflowExecutionTimedOut':
      return 'Timed out';
    case 'ActivityTaskCancelRequested':
      return 'Cancel requested';
    case 'ActivityTaskTerminated':
    case 'ChildWorkflowExecutionTerminated':
      return 'Terminated';
    default:
      return type;
  }
}

function ExpandedRowBody(props: { task: JobRunEvent }): ReactElement {
  const { task: jobRunEvent } = props;
  return (
    <div className="mx-5 mb-2 rounded-lg border border-line-soft bg-surface-2/60 px-4 py-3">
      <div className="mb-2 text-xs text-muted-foreground">
        {jobRunEvent.id.toString()}. {jobRunEvent.type}
      </div>
      <ol className="m-0 flex list-none flex-col gap-1.5 p-0 text-[13px]">
        {jobRunEvent.tasks.map((subtask, index) => (
          <li
            key={String(subtask.id)}
            className="grid grid-cols-[120px_minmax(0,auto)_1fr] items-baseline gap-3"
          >
            <span className="font-medium">{getStepLabel(subtask.type)}</span>
            <span className="font-mono text-xs">
              {formatFullDate(
                subtask.eventTime
                  ? convertTimestampToDate(subtask.eventTime)
                  : undefined
              )}
            </span>
            <span className="font-mono text-xs text-muted-foreground">
              {index > 0
                ? `+${formatTaskDuration(
                    jobRunEvent.tasks[index - 1].eventTime,
                    convertTimestampToDate(subtask.eventTime)
                  )}`
                : ''}
            </span>
            {subtask.error && (
              <span className="col-span-3 whitespace-pre-wrap break-words rounded-md bg-danger-soft px-2 py-1 text-xs text-danger-fg">
                {subtask.error.message}
                {subtask.error.retryState
                  ? ` (retry state: ${subtask.error.retryState})`
                  : ''}
              </span>
            )}
          </li>
        ))}
      </ol>
    </div>
  );
}
