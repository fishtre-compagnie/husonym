import { create } from '@bufbuild/protobuf';
import { timestampFromMs } from '@bufbuild/protobuf/wkt';
import { JobRunEvent, JobRunEventSchema, JobRunStatus } from '@husonym/sdk';
import {
  getPhases,
  getTableProgress,
  getTaskStatus,
  humanizeActivityType,
} from './events';

const T0 = Date.UTC(2026, 10, 10, 14, 0, 0);

function activity(
  id: number,
  type: string,
  startS: number,
  endS?: number,
  table?: string,
  error?: string
): JobRunEvent {
  const child = type === 'TableSync' || type === 'SchemaInit';
  const tasks = [
    {
      id: BigInt(id),
      type: child
        ? 'StartChildWorkflowExecutionInitiated'
        : 'ActivityTaskScheduled',
      eventTime: timestampFromMs(T0 + startS * 1000),
    },
  ];
  if (error) {
    tasks.push({
      id: BigInt(id + 1),
      type: 'ActivityTaskFailed',
      eventTime: timestampFromMs(T0 + (endS ?? startS) * 1000),
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      error: { message: error, retryState: '' } as any,
    } as (typeof tasks)[number]);
  } else if (endS !== undefined) {
    tasks.push({
      id: BigInt(id + 2),
      type: child ? 'ChildWorkflowExecutionCompleted' : 'ActivityTaskCompleted',
      eventTime: timestampFromMs(T0 + endS * 1000),
    });
  }
  return create(JobRunEventSchema, {
    id: BigInt(id),
    type,
    startTime: timestampFromMs(T0 + startS * 1000),
    closeTime:
      endS !== undefined && !error
        ? timestampFromMs(T0 + endS * 1000)
        : undefined,
    metadata: table
      ? {
          metadata: {
            case: 'syncMetadata',
            value: { schema: 'public', table },
          },
        }
      : undefined,
    tasks,
  });
}

const events = [
  activity(13, 'RecordRunStarted', 0, 1),
  activity(45, 'GenerateBenthosConfigs', 1, 5),
  activity(51, 'RunPreflight', 5, 6),
  activity(57, 'RunJobHooksByTiming', 6, 7),
  activity(63, 'SchemaInit', 7, 10),
  activity(80, 'TableSync', 10, 20, 'users'),
  activity(81, 'TableSync', 10, undefined, 'orders'),
  activity(93, 'RunPostTableSync', 20, 21),
  activity(100, 'TableSync', 21, 25, 'invoices', 'lock wait timeout'),
  activity(120, 'TableSync', 30, 32, 'users'),
];

describe('getTableProgress', () => {
  it('gives one line per table, merging its passes', () => {
    const tables = getTableProgress(events, JobRunStatus.RUNNING);
    expect(tables.map((t) => t.key)).toEqual([
      'public.users',
      'public.orders',
      'public.invoices',
    ]);
    const users = tables[0];
    expect(users.passes).toBe(2);
    expect(users.status).toBe('completed');
    expect(users.end.getTime()).toBe(T0 + 32_000);
    expect(tables[1].status).toBe('running');
    expect(tables[2].status).toBe('failed');
    expect(tables[2].error).toBe('lock wait timeout');
  });
});

describe('getPhases', () => {
  it('orders the steps the run reached and counts its tables', () => {
    const phases = getPhases(events, JobRunStatus.RUNNING);
    expect(phases.map((p) => p.key)).toEqual([
      'prepare',
      'preflight',
      'hooksBefore',
      'schema',
      'tables',
    ]);
    const tables = phases[phases.length - 1];
    expect(tables.tablesDone).toBe(1);
    expect(tables.tablesTotal).toBe(3);
    expect(tables.status).toBe('failed');
  });

  it('puts hooks after the tables once tables began', () => {
    const phases = getPhases(
      [...events.slice(0, 6), activity(358, 'RunJobHooksByTiming', 40, 41)],
      JobRunStatus.COMPLETE
    );
    expect(phases.map((p) => p.key)).toContain('hooksAfter');
  });
});

describe('getTaskStatus', () => {
  it('reads an activity left open by a canceled run as canceled', () => {
    expect(getTaskStatus(events[6], JobRunStatus.CANCELED)).toBe('canceled');
    expect(getTaskStatus(events[6], JobRunStatus.RUNNING)).toBe('running');
  });
});

describe('humanizeActivityType', () => {
  it('splits a type into words', () => {
    expect(humanizeActivityType('GenerateBenthosConfigs')).toBe(
      'Generate benthos configs'
    );
    expect(humanizeActivityType('RunJobHooksByTiming')).toBe(
      'Run job hooks by timing'
    );
  });
});
