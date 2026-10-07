import { timestampDate } from '@bufbuild/protobuf/wkt';
import type { GetLicenseUsageResponse, SystemLicense } from '@husonym/sdk';

// The features a license key can allow, by the names the API uses, in the order they
// are shown.
export const LICENSE_FEATURES: string[] = [
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
];

const FEATURE_LABELS: Record<string, string> = {
  job_hooks: 'Job hooks',
  account_hooks: 'Account hooks',
  pii_text: 'PII text anonymization',
  pii_detection: 'PII detection',
  custom_transformers: 'Custom transformers',
  subsetting: 'Subsetting',
  scheduling: 'Scheduling',
  mapping_review: 'Mapping review',
  api_keys: 'API keys',
  mcp: 'MCP server',
  rbac: 'Member roles',
  sso: 'Single sign-on (OIDC)',
  run_logs: 'Run logs',
};

// A name this version has no label for is shown as it is, rather than hidden.
export function featureLabel(name: string): string {
  return FEATURE_LABELS[name] ?? name;
}

// Whether the license in force allows a feature. A key that is not valid allows
// nothing, whatever it lists.
export function isFeatureAllowed(
  license: SystemLicense | undefined,
  name: string
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

export type FeatureRow = {
  name: string;
  allowed: boolean;
  inUse: boolean;
  // Used by the account and not allowed by the license: what keeps its jobs from
  // starting.
  blocking: boolean;
};

export function featureRows(
  all: string[],
  license: SystemLicense | undefined,
  inUse: string[]
): FeatureRow[] {
  return all.map((name) => {
    const allowed = isFeatureAllowed(license, name);
    const used = inUse.includes(name);
    return { name, allowed, inUse: used, blocking: used && !allowed };
  });
}

// The sources the instance counts, against the cap of its license key. An unset cap is
// no cap, which is distinct from a cap of zero.
export function sourceUsage(
  license: SystemLicense | undefined,
  usage: GetLicenseUsageResponse | undefined
): { used: number; cap?: number; over: boolean } {
  const used = usage?.sourcesInInstance ?? 0;
  const cap = license?.limits?.maxSources;
  return { used, cap, over: cap !== undefined && used > cap };
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

export function formatDate(date: Date): string {
  return date.toLocaleDateString(undefined, {
    year: 'numeric',
    month: 'long',
    day: 'numeric',
  });
}
