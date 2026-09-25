import { create, toJsonString } from '@bufbuild/protobuf';
import {
  JobEngine,
  JobTypeConfigSchema,
  PreflightFinding_Kind,
  PreflightFinding_Level,
  PreflightReportSchema,
} from '@husonym/sdk';
import {
  countReport,
  groupReport,
  hasPreflight,
  parseKeptReport,
  summarize,
} from './report';

const report = create(PreflightReportSchema, {
  engine: JobEngine.ATHANOR,
  findings: [
    {
      kind: PreflightFinding_Kind.READ_IN_ONE_STREAM,
      level: PreflightFinding_Level.INFORMATION,
      table: 'public.journal',
      message: 'public.journal is read in one stream',
    },
    {
      kind: PreflightFinding_Kind.WRITABLE,
      level: PreflightFinding_Level.BLOCKING,
      connectionId: 'dest',
      table: 'public.users',
      missing: ['INSERT'],
      message: 'cannot write',
      remedy: 'GRANT INSERT ON public.users TO etl;',
    },
    {
      kind: PreflightFinding_Kind.OUTPUT_TOO_LONG,
      level: PreflightFinding_Level.WARNING,
      connectionId: 'dest',
      table: 'public.users',
      columns: ['code'],
      message: 'too long',
    },
    {
      kind: PreflightFinding_Kind.READABLE,
      level: PreflightFinding_Level.BLOCKING,
      connectionId: 'gone',
      message: 'a connection the job no longer names',
    },
  ],
});

describe('countReport', () => {
  it('counts each level', () => {
    expect(countReport(report)).toEqual({ blocking: 2, warnings: 1, notes: 1 });
  });
});

describe('summarize', () => {
  it('says the most serious first', () => {
    expect(summarize({ blocking: 1, warnings: 2, notes: 0 })).toMatch(
      /^1 blocking finding: the run will stop/
    );
    expect(summarize({ blocking: 0, warnings: 2, notes: 0 })).toMatch(
      /^2 warnings/
    );
    expect(summarize({ blocking: 0, warnings: 0, notes: 3 })).toMatch(
      /^Nothing stops the run\. 3 notes/
    );
    expect(summarize({ blocking: 0, warnings: 0, notes: 0 })).toMatch(
      /^Nothing to report/
    );
  });
});

describe('groupReport', () => {
  it('puts the plan first, then the connections in the order of the job', () => {
    const groups = groupReport(report, [
      { id: 'src', name: 'prod', role: 'Source' },
      { id: 'dest', name: 'stage', role: 'Destination' },
    ]);
    expect(groups.map((g) => g.title)).toEqual([
      'Plan of the run',
      'Destination · stage',
      'Connection',
    ]);
    expect(groups[1].findings.map((f) => f.level)).toEqual([
      'blocking',
      'warning',
    ]);
    expect(groups[1].findings[0].remedy).toBe(
      'GRANT INSERT ON public.users TO etl;'
    );
    expect(groups[1].findings[1].columns).toEqual(['code']);
  });
});

describe('parseKeptReport', () => {
  it('reads the JSON the worker keeps', () => {
    const kept = new TextEncoder().encode(
      toJsonString(PreflightReportSchema, report)
    );
    expect(parseKeptReport(kept)?.findings).toHaveLength(4);
  });
  it('reads a report from a newer worker, with fields it does not know', () => {
    const json = JSON.parse(toJsonString(PreflightReportSchema, report));
    json.fromANewerWorker = true;
    const kept = new TextEncoder().encode(JSON.stringify(json));
    expect(parseKeptReport(kept)?.findings).toHaveLength(4);
  });
  it('gives nothing for what it cannot read', () => {
    expect(parseKeptReport(new TextEncoder().encode('{nope'))).toBeUndefined();
  });
});

describe('hasPreflight', () => {
  it('leaves out PII detection jobs, which write nothing', () => {
    expect(hasPreflight({ jobType: undefined })).toBe(true);
    expect(
      hasPreflight({
        jobType: create(JobTypeConfigSchema, {
          jobType: { case: 'piiDetect', value: {} },
        }),
      })
    ).toBe(false);
    expect(hasPreflight(undefined)).toBe(false);
  });
});
