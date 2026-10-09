import { RunErrorCategory, RunErrorStep } from '@husonym/sdk';

const OTHER = 'Other';

// What kept a run from completing, as the pages name it. A member added to the enum
// does not compile until it has its label here.
const ERROR_CATEGORY_LABELS: Record<RunErrorCategory, string> = {
  [RunErrorCategory.UNSPECIFIED]: OTHER,
  [RunErrorCategory.CONNECTION_REFUSED]: 'Connection refused',
  [RunErrorCategory.AUTHENTICATION_REFUSED]: 'Authentication refused',
  [RunErrorCategory.TIMEOUT]: 'Timeout',
  [RunErrorCategory.CONSTRAINT_VIOLATED]: 'Constraint violated',
  [RunErrorCategory.INSUFFICIENT_PRIVILEGES]: 'Insufficient privileges',
  [RunErrorCategory.OBJECT_MISSING]: 'Object missing',
  [RunErrorCategory.TYPE_MISMATCH]: 'Type mismatch',
  [RunErrorCategory.RESOURCES_EXHAUSTED]: 'Resources exhausted',
  [RunErrorCategory.CANCELED]: 'Canceled',
  [RunErrorCategory.LICENSE]: 'License',
  [RunErrorCategory.OTHER]: OTHER,
};

// A category this page does not know (an API newer than the page) reads as Other.
export function errorCategoryLabel(category: RunErrorCategory): string {
  return ERROR_CATEGORY_LABELS[category] ?? OTHER;
}

// The step a run was at when it stopped, as the pages name it. As for the categories,
// a member added to the enum does not compile until it has its label here.
const ERROR_STEP_LABELS: Record<RunErrorStep, string> = {
  [RunErrorStep.UNSPECIFIED]: OTHER,
  [RunErrorStep.PREFLIGHT]: 'Preflight',
  [RunErrorStep.SCHEMA_INIT]: 'Schema initialization',
  [RunErrorStep.TABLE_SYNC]: 'Table sync',
  [RunErrorStep.HOOKS]: 'Hooks',
  [RunErrorStep.INTEGRITY_CHECK]: 'Integrity check',
  [RunErrorStep.OTHER]: OTHER,
};

// A step this page does not know reads as Other.
export function errorStepLabel(step: RunErrorStep): string {
  return ERROR_STEP_LABELS[step] ?? OTHER;
}
