import { isPassthrough } from '@/util/mapping-changes';
import { timestampDate } from '@bufbuild/protobuf/wkt';
import {
  Connection,
  Job,
  JobMappingChange,
  JobMappingChangeKind,
  JobRun,
  JobRunStatus,
  JobStatus,
} from '@husonym/sdk';
import { getConnectionIdFromSource } from '../[id]/source/components/util';
import { ScheduleLabel, scheduleLabel } from './schedule-label';

// The state a job is shown in: what its schedule says, unless a run tells more.
export type JobState =
  'running' | 'active' | 'failing' | 'paused' | 'disabled' | 'unknown';

export type JobFilter = 'all' | 'active' | 'paused' | 'failing';

export const JOB_STATE_LABEL: Record<JobState, string> = {
  running: 'Running',
  active: 'Active',
  failing: 'Last run failed',
  paused: 'Paused',
  disabled: 'Disabled',
  unknown: 'Status unknown',
};

// How many of the latest runs a row shows.
export const RECENT_RUNS = 10;

export interface JobRow {
  id: string;
  name: string;
  job: Job;
  status: JobStatus;
  state: JobState;
  type: string;
  schedule: ScheduleLabel;
  // The engine of the source, short (MySQL, PG…), when the source is a connection.
  engine?: string;
  source: string;
  destinations: string[];
  tables: number;
  columns: number;
  // The columns the job transforms: every one that is not passed through as it is.
  transformed: number;
  // The latest runs, oldest first.
  recentRuns: JobRun[];
  lastRunAt?: Date;
  // Changes the runs made to the job's mappings that nobody has reviewed, and how many of
  // them left a column that reads as personal data in clear.
  pendingReviews: number;
  pendingPersonal: number;
}

const ENGINE: Record<string, string> = {
  mysqlConfig: 'MySQL',
  pgConfig: 'PG',
  mssqlConfig: 'MSSQL',
  mongoConfig: 'Mongo',
  dynamodbConfig: 'DynamoDB',
  awsS3Config: 'S3',
  gcpCloudstorageConfig: 'GCS',
  openaiConfig: 'OpenAI',
  localDirConfig: 'Local',
};

function jobType(j: Job): string {
  if (j.source?.options?.config.case === 'generate') {
    return 'Generate';
  }
  if (j.source?.options?.config.case === 'aiGenerate') {
    return 'AI Generate';
  }
  if (j.jobType?.jobType.case === 'piiDetect') {
    return 'PII Detection';
  }
  return 'Sync';
}

export function isRunLive(s: JobRunStatus): boolean {
  return s === JobRunStatus.RUNNING || s === JobRunStatus.PENDING;
}

export function isRunFailed(s: JobRunStatus): boolean {
  return (
    s === JobRunStatus.ERROR ||
    s === JobRunStatus.FAILED ||
    s === JobRunStatus.TIMED_OUT
  );
}

function runTime(r: JobRun): number {
  return r.startedAt ? timestampDate(r.startedAt).getTime() : 0;
}

function jobState(status: JobStatus, latest: JobRun | undefined): JobState {
  if (latest && isRunLive(latest.status)) {
    return 'running';
  }
  if (status === JobStatus.PAUSED) {
    return 'paused';
  }
  if (status === JobStatus.DISABLED) {
    return 'disabled';
  }
  if (latest && isRunFailed(latest.status)) {
    return 'failing';
  }
  return status === JobStatus.ENABLED ? 'active' : 'unknown';
}

export function matchesFilter(state: JobState, filter: JobFilter): boolean {
  switch (filter) {
    case 'all':
      return true;
    case 'active':
      return state === 'active' || state === 'running';
    case 'paused':
      return state === 'paused';
    case 'failing':
      return state === 'failing' || state === 'disabled';
  }
}

// What the search box looks into: the job's name, its connections and its tables.
export function matchesQuery(row: JobRow, query: string): boolean {
  const q = query.trim().toLowerCase();
  if (!q) {
    return true;
  }
  if (
    row.name.toLowerCase().includes(q) ||
    row.source.toLowerCase().includes(q) ||
    row.destinations.some((d) => d.toLowerCase().includes(q))
  ) {
    return true;
  }
  return row.job.mappings.some((m) =>
    `${m.schema}.${m.table}`.toLowerCase().includes(q)
  );
}

export function buildJobRows(input: {
  jobs: Job[];
  statuses: Map<string, JobStatus>;
  runs: JobRun[];
  connections: Connection[];
  pendingChanges: JobMappingChange[];
}): JobRow[] {
  const { jobs, statuses, runs, connections, pendingChanges } = input;
  const connById = new Map(connections.map((c) => [c.id, c] as const));

  const runsByJob = new Map<string, JobRun[]>();
  for (const r of runs) {
    const list = runsByJob.get(r.jobId) ?? [];
    list.push(r);
    runsByJob.set(r.jobId, list);
  }

  const pendingByJob = new Map<string, { count: number; personal: number }>();
  for (const change of pendingChanges) {
    const entry = pendingByJob.get(change.jobId) ?? { count: 0, personal: 0 };
    entry.count++;
    if (
      change.piiCategory &&
      change.kind === JobMappingChangeKind.ADDED &&
      isPassthrough(change)
    ) {
      entry.personal++;
    }
    pendingByJob.set(change.jobId, entry);
  }

  return jobs.map((j) => {
    const status = statuses.get(j.id) ?? JobStatus.UNSPECIFIED;
    const latestFirst = [...(runsByJob.get(j.id) ?? [])].sort(
      (a, b) => runTime(b) - runTime(a)
    );
    const latest = latestFirst[0];
    const recentRuns = latestFirst.slice(0, RECENT_RUNS).reverse();

    const sourceCase = j.source?.options?.config.case;
    const sourceId = getConnectionIdFromSource(j.source);
    const sourceConn =
      sourceCase === 'generate' || sourceCase === 'aiGenerate'
        ? undefined
        : sourceId
          ? connById.get(sourceId)
          : undefined;
    let source: string;
    if (sourceCase === 'generate') {
      source = 'Generated data';
    } else if (sourceCase === 'aiGenerate') {
      source = 'AI-generated data';
    } else {
      source = sourceConn?.name ?? 'Unknown connection';
    }
    const engineCase = sourceConn?.connectionConfig?.config.case;

    const tables = new Set(j.mappings.map((m) => `${m.schema}.${m.table}`));
    const transformed = j.mappings.filter(
      (m) =>
        m.transformer?.config?.config.case !== undefined &&
        m.transformer.config.config.case !== 'passthroughConfig'
    ).length;

    return {
      id: j.id,
      name: j.name,
      job: j,
      status,
      state: jobState(status, latest),
      type: jobType(j),
      schedule: scheduleLabel(j.cronSchedule),
      engine: engineCase ? ENGINE[engineCase] : undefined,
      source,
      destinations: j.destinations.map(
        (d) => connById.get(d.connectionId)?.name ?? 'Unknown connection'
      ),
      tables: tables.size,
      columns: j.mappings.length,
      transformed,
      recentRuns,
      lastRunAt: latest?.startedAt
        ? timestampDate(latest.startedAt)
        : undefined,
      pendingReviews: pendingByJob.get(j.id)?.count ?? 0,
      pendingPersonal: pendingByJob.get(j.id)?.personal ?? 0,
    };
  });
}
