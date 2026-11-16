import { RunErrorCategory, RunErrorStep } from '@husonym/sdk';
import { ENGLISH_USAGE_I18N, UsageI18n } from './i18n';

type CategoryKey =
  | 'connectionRefused'
  | 'authenticationRefused'
  | 'timeout'
  | 'constraintViolated'
  | 'insufficientPrivileges'
  | 'objectMissing'
  | 'typeMismatch'
  | 'resourcesExhausted'
  | 'canceled'
  | 'license'
  | 'other';

// What kept a run from completing, as the pages name it (the keys of
// settings.usage.errorCategories). A member added to the enum does not compile until
// it has its label here.
const ERROR_CATEGORY_KEYS: Record<RunErrorCategory, CategoryKey> = {
  [RunErrorCategory.UNSPECIFIED]: 'other',
  [RunErrorCategory.CONNECTION_REFUSED]: 'connectionRefused',
  [RunErrorCategory.AUTHENTICATION_REFUSED]: 'authenticationRefused',
  [RunErrorCategory.TIMEOUT]: 'timeout',
  [RunErrorCategory.CONSTRAINT_VIOLATED]: 'constraintViolated',
  [RunErrorCategory.INSUFFICIENT_PRIVILEGES]: 'insufficientPrivileges',
  [RunErrorCategory.OBJECT_MISSING]: 'objectMissing',
  [RunErrorCategory.TYPE_MISMATCH]: 'typeMismatch',
  [RunErrorCategory.RESOURCES_EXHAUSTED]: 'resourcesExhausted',
  [RunErrorCategory.CANCELED]: 'canceled',
  [RunErrorCategory.LICENSE]: 'license',
  [RunErrorCategory.OTHER]: 'other',
};

// A category this page does not know (an API newer than the page) reads as Other.
export function errorCategoryLabel(
  category: RunErrorCategory,
  i18n: UsageI18n = ENGLISH_USAGE_I18N
): string {
  return i18n.t(`errorCategories.${ERROR_CATEGORY_KEYS[category] ?? 'other'}`);
}

type StepKey =
  | 'preflight'
  | 'schemaInit'
  | 'tableSync'
  | 'hooks'
  | 'integrityCheck'
  | 'other';

// The step a run was at when it stopped, as the pages name it (the keys of
// settings.usage.errorSteps). As for the categories, a member added to the enum does
// not compile until it has its label here.
const ERROR_STEP_KEYS: Record<RunErrorStep, StepKey> = {
  [RunErrorStep.UNSPECIFIED]: 'other',
  [RunErrorStep.PREFLIGHT]: 'preflight',
  [RunErrorStep.SCHEMA_INIT]: 'schemaInit',
  [RunErrorStep.TABLE_SYNC]: 'tableSync',
  [RunErrorStep.HOOKS]: 'hooks',
  [RunErrorStep.INTEGRITY_CHECK]: 'integrityCheck',
  [RunErrorStep.OTHER]: 'other',
};

// A step this page does not know reads as Other.
export function errorStepLabel(
  step: RunErrorStep,
  i18n: UsageI18n = ENGLISH_USAGE_I18N
): string {
  return i18n.t(`errorSteps.${ERROR_STEP_KEYS[step] ?? 'other'}`);
}
