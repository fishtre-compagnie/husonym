import { FindingItem, FindingLevel } from '@/components/findings/FindingList';
import { fromJsonString } from '@bufbuild/protobuf';
import {
  Job,
  JobEngine,
  PreflightFinding_Level,
  PreflightReport,
  PreflightReportSchema,
} from '@husonym/sdk';

// The key the run context of a run keeps its pre-flight report under.
export const PREFLIGHT_REPORT_EXTERNAL_ID = 'preflight-report';

// hasPreflight tells whether a job has a pre-flight check: a PII detection job writes
// nothing, and has none.
export function hasPreflight(job: Pick<Job, 'jobType'> | undefined): boolean {
  return !!job && job.jobType?.jobType.case !== 'piiDetect';
}

// A connection of the job, and the role it plays in it.
export interface JobConnection {
  id: string;
  name: string;
  role: 'Source' | 'Destination';
}

// A part of the report: what the plan tells, or what one connection tells.
export interface PreflightGroup {
  key: string;
  title: string;
  findings: FindingItem[];
}

export interface PreflightCounts {
  blocking: number;
  warnings: number;
  notes: number;
}

function levelOf(level: PreflightFinding_Level): FindingLevel {
  switch (level) {
    case PreflightFinding_Level.BLOCKING:
      return 'blocking';
    case PreflightFinding_Level.WARNING:
      return 'warning';
    default:
      return 'information';
  }
}

export function countReport(report: PreflightReport): PreflightCounts {
  const counts: PreflightCounts = { blocking: 0, warnings: 0, notes: 0 };
  for (const finding of report.findings) {
    switch (levelOf(finding.level)) {
      case 'blocking':
        counts.blocking++;
        break;
      case 'warning':
        counts.warnings++;
        break;
      default:
        counts.notes++;
    }
  }
  return counts;
}

// groupReport splits a report into what the plan tells, first, then what each connection
// tells, in the order of the job: its source, then its destinations.
export function groupReport(
  report: PreflightReport,
  connections: JobConnection[]
): PreflightGroup[] {
  const plan: FindingItem[] = [];
  const byConnection = new Map<string, FindingItem[]>();
  report.findings.forEach((finding, index) => {
    const item: FindingItem = {
      key: `${finding.kind}-${finding.table}-${finding.connectionId ?? ''}-${index}`,
      level: levelOf(finding.level),
      table: finding.table || undefined,
      columns: finding.columns,
      message: finding.message,
      missing: finding.missing,
      remedy: finding.remedy,
    };
    if (!finding.connectionId) {
      plan.push(item);
      return;
    }
    const items = byConnection.get(finding.connectionId) ?? [];
    items.push(item);
    byConnection.set(finding.connectionId, items);
  });

  const groups: PreflightGroup[] = [];
  if (plan.length > 0) {
    groups.push({ key: 'plan', title: 'Plan of the run', findings: plan });
  }
  for (const connection of connections) {
    const items = byConnection.get(connection.id);
    if (items) {
      groups.push({
        key: connection.id,
        title: `${connection.role} · ${connection.name}`,
        findings: items,
      });
      byConnection.delete(connection.id);
    }
  }
  // A connection the job no longer names, if the report is older than the job.
  for (const [id, items] of byConnection) {
    groups.push({ key: id, title: 'Connection', findings: items });
  }
  return groups;
}

// summarize says in a sentence what a report means for a run.
export function summarize(counts: PreflightCounts): string {
  if (counts.blocking > 0) {
    return `${plural(counts.blocking, 'blocking finding')}: the run will stop at its start.`;
  }
  if (counts.warnings > 0) {
    return `${plural(counts.warnings, 'warning')}: the run may stop, or copy wrong, depending on the rows.`;
  }
  if (counts.notes > 0) {
    return `Nothing stops the run. ${plural(counts.notes, 'note')} on what it does.`;
  }
  return 'Nothing to report: the run meets nothing the plan can tell.';
}

export function engineLabel(engine: JobEngine): string {
  switch (engine) {
    case JobEngine.ATHANOR:
      return 'Athanor';
    case JobEngine.BENTHOS:
      return 'Benthos';
    default:
      return 'default engine';
  }
}

// parseKeptReport reads the report a run kept in its run context, as the worker writes it.
export function parseKeptReport(
  value: Uint8Array
): PreflightReport | undefined {
  try {
    // A worker newer than this page may write fields it does not know.
    return fromJsonString(
      PreflightReportSchema,
      new TextDecoder().decode(value),
      { ignoreUnknownFields: true }
    );
  } catch {
    return undefined;
  }
}

function plural(count: number, noun: string): string {
  return `${count} ${noun}${count > 1 ? 's' : ''}`;
}
