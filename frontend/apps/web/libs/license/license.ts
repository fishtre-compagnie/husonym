import { timestampDate } from '@bufbuild/protobuf/wkt';
import { AccountRole } from '@husonym/sdk';
import type {
  GetLicenseUsageResponse,
  LicenseLimits,
  SystemLicense,
} from '@husonym/sdk';
import { ENGLISH_LICENSE_T, LicenseTranslator } from './i18n';

// The features a license key can allow, by the names the API uses, in the order they
// are shown. A test of the API (internal/license) reads this list and holds it to the
// one the API declares.
export const LICENSE_FEATURES = [
  'job_hooks',
  'account_hooks',
  'pii_text',
  'pii_detection',
  'custom_transformers',
  'subsetting',
  'scheduling',
  'mapping_review',
  'api_keys',
  'mcp',
  'rbac',
  'sso',
  'run_logs',
] as const;

// The name of a feature. A name that is not one of the list does not compile.
export type LicenseFeature = (typeof LICENSE_FEATURES)[number];

// The features whose use keeps a job from starting when the license does not include
// them: the ones the job gate of the API counts
// (backend/internal/licensegate/usage.go). The others, in use without being included,
// block nothing: what exists keeps working, and changes are refused.
const BLOCKING_FEATURES: readonly LicenseFeature[] = [
  'job_hooks',
  'pii_text',
  'pii_detection',
  'custom_transformers',
  'subsetting',
];

// The words of a helper come from the translator of settings.license it is given,
// from useTranslations('settings.license'); without one they are English.
export function featureLabel(
  name: LicenseFeature,
  t: LicenseTranslator = ENGLISH_LICENSE_T
): string {
  return t(`featureNames.${name}`);
}

// The gates of the license that are not features, by the names the API counts its
// refusals under (internal/license/refusal.go).
const LIMIT_GATES = [
  'license_not_in_force',
  'source_cap',
  'job_cap',
  'connection_cap',
  'connection_type',
] as const;

// What the license refused at a gate: a feature, by its label, or one of the limits.
// A gate this page does not know (an API newer than the page) reads as Other.
export function gateLabel(
  gate: string,
  t: LicenseTranslator = ENGLISH_LICENSE_T
): string {
  const feature = LICENSE_FEATURES.find((name) => name === gate);
  if (feature) {
    return featureLabel(feature, t);
  }
  const limit = LIMIT_GATES.find((name) => name === gate);
  return t(`gates.${limit ?? 'other'}`);
}

// Whether the license in force allows a feature. A key that is not valid allows
// nothing, whatever it lists.
function isFeatureAllowed(
  license: SystemLicense | undefined,
  name: LicenseFeature
): boolean {
  if (!license?.isValid) {
    return false;
  }
  // An API older than the feature list sends no state and no features: a valid license
  // unlocked everything there.
  if (license.state === '') {
    return true;
  }
  return license.allFeatures || license.features.includes(name);
}

// Whether the interface offers a feature. It greys an action only on what it has
// actually read: while the license is pending, or when it could not be read, the
// feature stays offered, and the API, which enforces the license, has the last word.
export function isFeatureAvailable(
  wasRead: boolean,
  license: SystemLicense | undefined,
  name: LicenseFeature
): boolean {
  return !wasRead || isFeatureAllowed(license, name);
}

// Why the actions of a feature are greyed out. With no license in force, none or a
// frozen one, no feature is included: saying that this one is missing from "your
// license" would name the wrong cause.
export function featureNoticeMessage(
  license: SystemLicense | undefined,
  t: LicenseTranslator = ENGLISH_LICENSE_T
): string {
  return license?.isValid ? t('notice.notIncluded') : t('notice.noneInForce');
}

// Whether the logs of a run are hidden. Reading keeps working when a license has lapsed
// or is absent, the logs like the rest: only a license that was read, is in force and
// does not include run_logs hides them, as the API refuses them then.
export function areRunLogsHidden(
  wasRead: boolean,
  license: SystemLicense | undefined
): boolean {
  if (!wasRead || !license?.isValid) {
    return false;
  }
  return !isFeatureAllowed(license, 'run_logs');
}

// Without the rbac feature, a member can be given the administrator role and no other.
export function isRoleSelectable(
  rbacAllowed: boolean,
  role: AccountRole
): boolean {
  return rbacAllowed || role === AccountRole.ADMIN;
}

// The role an invitation names. One that cannot be chosen is not named: the invitation then
// names no role, and the member is given the viewer role when accepting it.
export function invitationRole(
  rbacAllowed: boolean,
  role: AccountRole
): AccountRole {
  return isRoleSelectable(rbacAllowed, role) ? role : AccountRole.UNSPECIFIED;
}

export type FeatureRow = {
  name: LicenseFeature;
  allowed: boolean;
  inUse: boolean;
  // Used by the account, not allowed by the license, and one of the features that
  // keep a job from starting.
  blocking: boolean;
};

export function featureRows(
  all: readonly LicenseFeature[],
  license: SystemLicense | undefined,
  inUse: readonly string[]
): FeatureRow[] {
  return all.map((name) => {
    const allowed = isFeatureAllowed(license, name);
    const used = inUse.includes(name);
    return {
      name,
      allowed,
      inUse: used,
      blocking: used && !allowed && BLOCKING_FEATURES.includes(name),
    };
  });
}

// What a row says about the use the account makes of a feature. A feature in use that
// the license does not include, and that blocks no job, gets its own words: nothing
// stops, only changes are refused.
export function featureUseNote(
  row: FeatureRow,
  t: LicenseTranslator = ENGLISH_LICENSE_T
): string {
  if (!row.inUse) {
    return '';
  }
  if (!row.allowed && !row.blocking) {
    return t('features.inUseNotIncluded');
  }
  return t('features.inUse');
}

// What the Status card says in place of the details of a key. A key that was given and
// could not be read is not "no key installed".
export function missingKeyMessage(
  license: SystemLicense | undefined,
  t: LicenseTranslator = ENGLISH_LICENSE_T
): string {
  return license?.problem
    ? t('status.missingKeyUnreadable')
    : t('status.missingKeyNone');
}

// Whether the answer to installing a key describes the key the page already showed.
// The API answers the key in force pasted again as it answers a new one, with the
// description of the license: the key is the same when it was stored at the same
// moment, the same way.
export function isKeyAlreadyInForce(
  before: SystemLicense | undefined,
  after: SystemLicense | undefined
): boolean {
  if (!before?.installedAt || !after?.installedAt) {
    return false;
  }
  return (
    before.origin === after.origin &&
    before.installedAt.seconds === after.installedAt.seconds &&
    before.installedAt.nanos === after.installedAt.nanos
  );
}

// The caps of the license in force. A key that is not in force (frozen, or none at all)
// caps nothing, whatever it lists.
export function limitsInForce(
  license: SystemLicense | undefined
): LicenseLimits | undefined {
  return license?.isValid ? license.limits : undefined;
}

export type SourceUsage = {
  used: number;
  cap?: number;
  over: boolean;
  // What the count is set against. A key that is not in force neither caps nor
  // uncaps anything: there is only a count to show then, and no claim about a limit.
  limit: 'none-in-force' | 'capped' | 'uncapped';
};

// The sources the instance counts, against the cap of its license key. An unset cap is
// no cap, which is distinct from a cap of zero.
export function sourceUsage(
  license: SystemLicense | undefined,
  usage: GetLicenseUsageResponse | undefined
): SourceUsage {
  const used = usage?.sourcesInInstance ?? 0;
  if (!license?.isValid) {
    return { used, cap: undefined, over: false, limit: 'none-in-force' };
  }
  const cap = license.limits?.maxSources;
  if (cap === undefined) {
    return { used, cap, over: false, limit: 'uncapped' };
  }
  return { used, cap, over: used > cap, limit: 'capped' };
}

// Mirrors the backend lifecycle in internal/license.
export type LicenseState = 'none' | 'valid' | 'expiring' | 'grace' | 'frozen';

const LICENSE_STATES: LicenseState[] = [
  'none',
  'valid',
  'expiring',
  'grace',
  'frozen',
];

function isLicenseState(state: string): state is LicenseState {
  return LICENSE_STATES.some((known) => known === state);
}

const EXPIRING_WINDOW_DAYS = 30;

// Where the license stands in its lifecycle, as the API says it.
export function licenseState(license: SystemLicense | undefined): LicenseState {
  if (license && isLicenseState(license.state)) {
    return license.state;
  }
  return resolveState(
    license?.isValid,
    license?.expiresAt ? timestampDate(license.expiresAt) : undefined
  );
}

// An older API sends no state. It is derived then from what that API already exposed:
// isValid stays true throughout the grace period, so with expiresAt it pins down the
// state.
function resolveState(
  isValid: boolean | undefined,
  expiresAt: Date | undefined
): LicenseState {
  // No license at all: nothing to count down to.
  if (!expiresAt || expiresAt.getTime() === 0) {
    return isValid ? 'valid' : 'none';
  }
  // isValid covers the grace period, so a false here means grace is over too.
  if (!isValid) {
    return 'frozen';
  }
  const msLeft = expiresAt.getTime() - Date.now();
  if (msLeft <= 0) {
    return 'grace';
  }
  if (msLeft < EXPIRING_WINDOW_DAYS * 24 * 60 * 60 * 1000) {
    return 'expiring';
  }
  return 'valid';
}

// A day in the UI language (from useLocale), or the browser's language without one.
export function formatDate(date: Date, locale?: string): string {
  return date.toLocaleDateString(locale, {
    year: 'numeric',
    month: 'long',
    day: 'numeric',
  });
}
