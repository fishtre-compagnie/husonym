import { JobMappingChange, JobMappingChangeKind } from '@husonym/sdk';

export function columnName(c: JobMappingChange): string {
  return `${c.column?.schema}.${c.column?.table}.${c.column?.column}`;
}

export function changeLabel(c: JobMappingChange): string {
  switch (c.kind) {
    case JobMappingChangeKind.ADDED:
      return 'New column';
    case JobMappingChangeKind.REMOVED:
      return 'Column removed';
    case JobMappingChangeKind.TYPE_CHANGED:
      return `Type changed: ${c.previousDataType} → ${c.dataType}`;
    default:
      return 'Changed';
  }
}

// Whether the run left the column in clear.
export function isPassthrough(c: JobMappingChange): boolean {
  return c.transformer?.config?.config.case === 'passthroughConfig';
}

// The order in which changes are worth looking at: a column that reads as personal data and
// ships in clear, then a column whose type moved under its mapping, then any other column shipped
// in clear, then the columns the run anonymized, then the removals, which change nothing in
// what leaves the source.
export function urgency(c: JobMappingChange): number {
  if (c.kind === JobMappingChangeKind.ADDED && isPassthrough(c)) {
    return c.piiCategory ? 0 : 2;
  }
  if (c.kind === JobMappingChangeKind.TYPE_CHANGED) {
    return 1;
  }
  if (c.kind === JobMappingChangeKind.ADDED) {
    return 3;
  }
  return 4;
}
