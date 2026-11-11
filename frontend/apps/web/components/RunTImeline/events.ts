/**
 * Reading a run's events (the Temporal activities and child workflows that
 * getJobRunEvents returns): the state of each, when it began and ended, and how
 * they group into tables and phases. Nothing here is guessed beyond the events.
 */
import { Tone } from '@/components/ds';
import { Timestamp, timestampDate } from '@bufbuild/protobuf/wkt';
import {
  JobRunEvent,
  JobRunStatus,
  JobRunStatus as JobRunStatusEnum,
} from '@husonym/sdk';

export type RunStatus =
  'running' | 'completed' | 'failed' | 'canceled' | 'terminated';

export const RUN_STATUS_LABEL: Record<RunStatus, string> = {
  running: 'Running',
  completed: 'Completed',
  failed: 'Failed',
  canceled: 'Canceled',
  terminated: 'Terminated',
};

export const RUN_STATUS_TONE: Record<RunStatus, Tone> = {
  running: 'brand',
  completed: 'ok',
  failed: 'danger',
  canceled: 'warn',
  terminated: 'muted',
};

export function convertTimestampToDate(timestamp: Timestamp | undefined): Date {
  return timestamp ? timestampDate(timestamp) : new Date();
}

export function getScheduledTime(event: JobRunEvent): Timestamp | undefined {
  return (
    event.tasks.find(
      (st) =>
        st.type === 'ActivityTaskScheduled' ||
        st.type === 'StartChildWorkflowExecutionInitiated'
    )?.eventTime ?? event.startTime
  );
}

// handles getting the activity statuses by remaping the ActivityStatuses since (i think) we don't want to show all of them per activity
// the event types in the types field are just the stringified Temporal types
export function getTaskStatus(
  task: JobRunEvent,
  jobStatus: JobRunStatus | undefined
): RunStatus {
  let isCompleted = false;
  let isFailed = false;
  let isCanceled = false;

  for (const t of task.tasks) {
    switch (t.type) {
      case 'ActivityTaskCompleted':
      case 'ChildWorkflowExecutionCompleted':
        isCompleted = true;
        break;
      case 'ActivityTaskFailed':
      case 'ActivityTaskTimedOut':
      case 'ActivityTaskCanceled':
      case 'ActivityTaskTerminated':
      case 'ChildWorkflowExecutionFailed':
      case 'ChildWorkflowExecutionTimedOut':
      case 'ChildWorkflowExecutionTerminated':
      case 'StartChildWorkflowExecutionFailed':
        isFailed = true;
        break;
      case 'ActivityTaskCancelRequested':
        isCanceled = true;
        break;
      case 'ActivityTaskStarted':
      case 'ActivityTaskScheduled':
      case 'StartChildWorkflowExecutionInitiated':
        break;
    }

    if (t.error) {
      isFailed = true;
    }

    if (isCompleted) break;
  }

  if (isCompleted) return 'completed';
  if (isFailed) return 'failed';

  const isJobTerminated = jobStatus === JobRunStatusEnum.TERMINATED;
  if (isJobTerminated) return 'terminated';
  if (isCanceled) return 'canceled';

  // todo: it is completed but might not have completed successfully, and we need more task info to check that.
  if (task.closeTime) {
    return 'completed';
  }

  // the run is over but this activity never closed: it was stopped with the run
  if (
    jobStatus === JobRunStatusEnum.CANCELED ||
    jobStatus === JobRunStatusEnum.FAILED ||
    jobStatus === JobRunStatusEnum.ERROR ||
    jobStatus === JobRunStatusEnum.TIMED_OUT
  ) {
    return 'canceled';
  }

  return 'running';
}

// calculates the last time if the job is not successful so we can give the timeline an end date
// TODO: this should be revisited
export function getCloseOrErrorOrCancelDate(
  jobRunEvent: JobRunEvent,
  runStatus: RunStatus
): Date {
  const errorTask = jobRunEvent.tasks.find((item) => item.error);
  const errorTime = errorTask ? errorTask.eventTime : undefined;
  const cancelTime = jobRunEvent.tasks.find(
    (t) =>
      t.type === 'ActivityTaskCancelRequested' ||
      t.type === 'ActivityTaskCanceled' ||
      t.type === 'ActivityTaskTimedOut' ||
      t.type === 'ActivityTaskTerminated' ||
      // t.type === 'ChildWorkflowExecutionCanceled' || // has issues with close with new?
      t.type === 'ChildWorkflowExecutionTerminated' ||
      t.type === 'ChildWorkflowExecutionTimedOut'
  )?.eventTime;
  if (
    !errorTime &&
    !cancelTime &&
    !jobRunEvent.closeTime &&
    runStatus !== 'running'
  ) {
    if (jobRunEvent.startTime) {
      return convertTimestampToDate(jobRunEvent.startTime);
    }
    return new Date(); // this will result in a constantly increasing time but the startTime should always be present
  }

  // a completed activity ends when it closed, even if an earlier attempt failed
  if (runStatus === 'completed' && jobRunEvent.closeTime) {
    return convertTimestampToDate(jobRunEvent.closeTime);
  }

  return errorTime
    ? convertTimestampToDate(errorTime)
    : cancelTime
      ? convertTimestampToDate(cancelTime)
      : convertTimestampToDate(jobRunEvent.closeTime);
}

export function isSyncActivity(task: JobRunEvent): boolean {
  return task.metadata?.metadata.case == 'syncMetadata';
}

export function getSyncTable(
  event: JobRunEvent
): { schema: string; table: string } | undefined {
  if (event.metadata?.metadata.case !== 'syncMetadata') {
    return undefined;
  }
  const { schema, table } = event.metadata.metadata.value;
  if (!schema || !table) {
    return undefined;
  }
  return { schema, table };
}

/** Where an event sits in time, with its state. */
export interface EventWindow {
  start: Date;
  end: Date;
  status: RunStatus;
}

export function getEventWindow(
  event: JobRunEvent,
  jobStatus: JobRunStatus | undefined
): EventWindow {
  const status = getTaskStatus(event, jobStatus);
  return {
    start: convertTimestampToDate(getScheduledTime(event)),
    end: getCloseOrErrorOrCancelDate(event, status),
    status,
  };
}

/** The first and last instants the events cover. */
export function getEventsSpan(
  events: JobRunEvent[],
  jobStatus: JobRunStatus | undefined
): { start: Date; end: Date } | undefined {
  if (events.length === 0) {
    return undefined;
  }
  let start = Infinity;
  let end = -Infinity;
  for (const e of events) {
    const w = getEventWindow(e, jobStatus);
    start = Math.min(start, w.start.getTime());
    end = Math.max(
      end,
      w.end.getTime(),
      convertTimestampToDate(e.closeTime || e.startTime).getTime()
    );
  }
  return { start: new Date(start), end: new Date(Math.max(end, start + 1)) };
}

/** The state of several events taken together: one failure fails them all. */
export function combineStatuses(statuses: RunStatus[]): RunStatus {
  if (statuses.includes('failed')) return 'failed';
  if (statuses.includes('running')) return 'running';
  if (statuses.includes('terminated')) return 'terminated';
  if (statuses.includes('canceled')) return 'canceled';
  return 'completed';
}

export interface TableProgress {
  key: string;
  schema: string;
  table: string;
  start: Date;
  end: Date;
  status: RunStatus;
  // the table was synced in more than one pass (e.g. a second pass for circular references)
  passes: number;
  error?: string;
}

/** One line per table the run synced, in the order it began them. */
export function getTableProgress(
  events: JobRunEvent[],
  jobStatus: JobRunStatus | undefined
): TableProgress[] {
  const byKey = new Map<string, TableProgress & { statuses: RunStatus[] }>();
  for (const e of events) {
    const t = getSyncTable(e);
    if (!t) continue;
    const key = `${t.schema}.${t.table}`;
    const w = getEventWindow(e, jobStatus);
    const error = e.tasks.find((task) => task.error)?.error?.message;
    const prev = byKey.get(key);
    if (!prev) {
      byKey.set(key, {
        key,
        ...t,
        start: w.start,
        end: w.end,
        status: w.status,
        statuses: [w.status],
        passes: 1,
        error: w.status === 'completed' ? undefined : error,
      });
    } else {
      prev.start = new Date(Math.min(prev.start.getTime(), w.start.getTime()));
      prev.end = new Date(Math.max(prev.end.getTime(), w.end.getTime()));
      prev.statuses.push(w.status);
      prev.status = combineStatuses(prev.statuses);
      prev.passes += 1;
      if (w.status !== 'completed' && error) prev.error = error;
    }
  }
  return Array.from(byKey.values())
    .map(({ statuses: _statuses, ...rest }) => rest)
    .sort((a, b) => a.start.getTime() - b.start.getTime());
}

export type PhaseKey =
  | 'prepare'
  | 'preflight'
  | 'hooksBefore'
  | 'schema'
  | 'tables'
  | 'integrity'
  | 'hooksAfter'
  | 'wrapUp';

export const PHASE_LABEL: Record<PhaseKey, string> = {
  prepare: 'Preparation',
  preflight: 'Pre-flight',
  hooksBefore: 'Hooks before',
  schema: 'Schema',
  tables: 'Table copy',
  integrity: 'Integrity',
  hooksAfter: 'Hooks after',
  wrapUp: 'Wrap-up',
};

const PHASE_ORDER: PhaseKey[] = [
  'prepare',
  'preflight',
  'hooksBefore',
  'schema',
  'tables',
  'integrity',
  'hooksAfter',
  'wrapUp',
];

export interface Phase {
  key: PhaseKey;
  label: string;
  status: RunStatus;
  start: Date;
  end: Date;
  activities: number;
  tablesDone: number;
  tablesTotal: number;
}

/**
 * The steps of the run as its events show them. Each event goes to a step by
 * its type; hooks go before or after the tables by when they ran; a type not
 * known here stays with the step before it. Only steps the run reached appear.
 */
export function getPhases(
  events: JobRunEvent[],
  jobStatus: JobRunStatus | undefined
): Phase[] {
  const sorted = [...events].sort((a, b) => Number(a.id - b.id));
  const firstTableId = sorted.find((e) => isSyncActivity(e))?.id;
  const groups = new Map<PhaseKey, JobRunEvent[]>();
  let current: PhaseKey = 'prepare';
  for (const e of sorted) {
    const afterTables = firstTableId !== undefined && e.id > firstTableId;
    let key: PhaseKey | undefined;
    if (isSyncActivity(e) || e.type === 'RunPostTableSync') {
      key = 'tables';
    } else {
      switch (e.type) {
        case 'RecordRunStarted':
        case 'RetrieveActivityOptions':
        case 'CheckAccountStatus':
        case 'GenerateBenthosConfigs':
          key = 'prepare';
          break;
        case 'RunPreflight':
          key = 'preflight';
          break;
        case 'RunJobHooksByTiming':
          key = afterTables ? 'hooksAfter' : 'hooksBefore';
          break;
        case 'SchemaInit':
        case 'SuspendTriggers':
          key = 'schema';
          break;
        case 'CheckReferentialIntegrity':
        case 'RestoreTriggers':
          key = 'integrity';
          break;
        case 'ProcessAccountHook':
          key = afterTables ? 'wrapUp' : 'prepare';
          break;
        case 'RecordRunEnded':
          key = 'wrapUp';
          break;
      }
    }
    // a type not named above, or an activity polled all along (the account
    // check runs again during the copy), stays with the step it falls in
    if (!key || (key === 'prepare' && afterTables)) {
      key = current;
    }
    current = key;
    const list = groups.get(key) ?? [];
    list.push(e);
    groups.set(key, list);
  }

  return PHASE_ORDER.filter((k) => groups.has(k)).map((key) => {
    const list = groups.get(key) ?? [];
    const windows = list.map((e) => getEventWindow(e, jobStatus));
    const tables = key === 'tables' ? getTableProgress(list, jobStatus) : [];
    return {
      key,
      label: PHASE_LABEL[key],
      status: combineStatuses(windows.map((w) => w.status)),
      start: new Date(Math.min(...windows.map((w) => w.start.getTime()))),
      end: new Date(Math.max(...windows.map((w) => w.end.getTime()))),
      activities: list.length,
      tablesDone: tables.filter((t) => t.status === 'completed').length,
      tablesTotal: tables.length,
    };
  });
}

/** An activity's type in words: GenerateBenthosConfigs → Generate benthos configs. */
export function humanizeActivityType(type: string): string {
  const words = type
    .replace(/([a-z0-9])([A-Z])/g, '$1 $2')
    .replace(/([A-Z])([A-Z][a-z])/g, '$1 $2')
    .split(/\s+/)
    .filter(Boolean);
  if (words.length === 0) return type;
  return words
    .map((w, i) =>
      i === 0 ? w : /^[A-Z0-9]+$/.test(w) && w.length > 1 ? w : w.toLowerCase()
    )
    .join(' ');
}
