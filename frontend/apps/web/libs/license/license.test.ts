import { create } from '@bufbuild/protobuf';
import { timestampFromDate } from '@bufbuild/protobuf/wkt';
import {
  AccountRole,
  GetLicenseUsageResponseSchema,
  LicenseLimitsSchema,
  SystemLicense,
  SystemLicenseSchema,
} from '@husonym/sdk';
import {
  areRunLogsHidden,
  featureLabel,
  featureNoticeMessage,
  featureRows,
  featureUseNote,
  gateLabel,
  invitationRole,
  isFeatureAvailable,
  isKeyAlreadyInForce,
  isRoleSelectable,
  LICENSE_FEATURES,
  LicenseFeature,
  missingKeyMessage,
  licenseState,
  limitsInForce,
  sourceUsage,
} from './license';

const DAY_MS = 24 * 60 * 60 * 1000;

function listLicense(features: string[]): SystemLicense {
  return create(SystemLicenseSchema, {
    isValid: true,
    state: 'valid',
    features,
  });
}

function fromNow(days: number) {
  return timestampFromDate(new Date(Date.now() + days * DAY_MS));
}

describe('LICENSE_FEATURES', () => {
  it('holds the thirteen feature names, in order', () => {
    expect(LICENSE_FEATURES).toEqual([
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
    ]);
  });
});

describe('featureLabel', () => {
  it('gives every known feature a label of its own', () => {
    const labels = LICENSE_FEATURES.map(featureLabel);
    expect(new Set(labels).size).toBe(LICENSE_FEATURES.length);
    for (const name of LICENSE_FEATURES) {
      expect(featureLabel(name)).not.toBe(name);
    }
    expect(featureLabel('sso')).toBe('Single sign-on (OIDC)');
  });

  it('takes the name of a feature and nothing else', () => {
    // @ts-expect-error a name that is not a feature does not compile
    featureLabel('time_travel');
  });
});

describe('gateLabel', () => {
  it('names the gate of a feature as the feature', () => {
    expect(gateLabel('rbac')).toBe('Member roles');
    for (const name of LICENSE_FEATURES) {
      expect(gateLabel(name)).toBe(featureLabel(name));
    }
  });

  it('names the gates that are not features', () => {
    expect(gateLabel('license_not_in_force')).toBe('No license in force');
    expect(gateLabel('source_cap')).toBe('Source limit');
    expect(gateLabel('job_cap')).toBe('Job limit');
    expect(gateLabel('connection_cap')).toBe('Connection limit');
    expect(gateLabel('connection_type')).toBe('Connection type');
  });

  it('reads a gate it does not know as Other', () => {
    expect(gateLabel('whatever')).toBe('Other');
    expect(gateLabel('')).toBe('Other');
    // The names every object answers to are no gates.
    expect(gateLabel('constructor')).toBe('Other');
    expect(gateLabel('toString')).toBe('Other');
  });
});

describe('a license that was read', () => {
  // What a license allows, asked the way the application asks it: of a license that
  // was read.
  const isFeatureAllowed = (
    license: SystemLicense | undefined,
    name: LicenseFeature
  ): boolean => isFeatureAvailable(true, license, name);

  it('allows nothing without a license', () => {
    expect(isFeatureAllowed(undefined, 'subsetting')).toBe(false);
  });

  it('allows nothing when the license is not valid, whatever the key lists', () => {
    const license = create(SystemLicenseSchema, {
      isValid: false,
      state: 'frozen',
      allFeatures: true,
      features: ['subsetting'],
    });
    expect(isFeatureAllowed(license, 'subsetting')).toBe(false);
  });

  it('allows the features the key lists, and only those', () => {
    const license = listLicense(['subsetting']);
    expect(isFeatureAllowed(license, 'subsetting')).toBe(true);
    expect(isFeatureAllowed(license, 'job_hooks')).toBe(false);
  });

  it('allows every feature when the key allows all of them', () => {
    const license = create(SystemLicenseSchema, {
      isValid: true,
      state: 'valid',
      allFeatures: true,
    });
    expect(isFeatureAllowed(license, 'job_hooks')).toBe(true);
  });

  it('follows isValid alone when the API sends no state', () => {
    // An API older than the feature list sends neither a state nor features: its valid
    // license unlocked everything.
    const valid = create(SystemLicenseSchema, { isValid: true });
    expect(isFeatureAllowed(valid, 'job_hooks')).toBe(true);
    const invalid = create(SystemLicenseSchema, { isValid: false });
    expect(isFeatureAllowed(invalid, 'job_hooks')).toBe(false);
  });
});

describe('isFeatureAvailable', () => {
  it('is available while the license is being read', () => {
    expect(isFeatureAvailable(false, undefined, 'subsetting')).toBe(true);
  });

  it('is available when the license could not be read', () => {
    // A failed request leaves nothing read, exactly as a pending one does: the API
    // enforces the license, the interface only greys what it knows to be refused.
    expect(isFeatureAvailable(false, undefined, 'job_hooks')).toBe(true);
  });

  it('is available when the license read includes the feature', () => {
    expect(
      isFeatureAvailable(true, listLicense(['subsetting']), 'subsetting')
    ).toBe(true);
  });

  it('is not available when the license read does not include the feature', () => {
    expect(
      isFeatureAvailable(true, listLicense(['subsetting']), 'job_hooks')
    ).toBe(false);
  });

  it('is not available when what was read holds no license in force', () => {
    expect(isFeatureAvailable(true, undefined, 'subsetting')).toBe(false);
    const frozen = create(SystemLicenseSchema, {
      isValid: false,
      state: 'frozen',
      allFeatures: true,
    });
    expect(isFeatureAvailable(true, frozen, 'subsetting')).toBe(false);
  });
});

describe('areRunLogsHidden', () => {
  it('shows the logs while the license is being read', () => {
    expect(areRunLogsHidden(false, undefined)).toBe(false);
  });

  it('shows the logs when the license could not be read', () => {
    // A failed request leaves nothing read: the API has the last word.
    expect(areRunLogsHidden(false, undefined)).toBe(false);
  });

  it('shows the logs under a license in force that includes run_logs', () => {
    expect(areRunLogsHidden(true, listLicense(['run_logs']))).toBe(false);
    const everything = create(SystemLicenseSchema, {
      isValid: true,
      state: 'valid',
      allFeatures: true,
    });
    expect(areRunLogsHidden(true, everything)).toBe(false);
  });

  it('hides the logs under a license in force that lacks run_logs', () => {
    expect(areRunLogsHidden(true, listLicense(['subsetting']))).toBe(true);
    expect(areRunLogsHidden(true, listLicense([]))).toBe(true);
  });

  it('shows the logs under a frozen license, whatever it listed', () => {
    // A license that has lapsed still allows consulting, the logs like the rest.
    const frozen = create(SystemLicenseSchema, {
      isValid: false,
      state: 'frozen',
      features: ['subsetting'],
    });
    expect(areRunLogsHidden(true, frozen)).toBe(false);
  });

  it('shows the logs when the instance has no license', () => {
    expect(areRunLogsHidden(true, undefined)).toBe(false);
    const none = create(SystemLicenseSchema, { isValid: false, state: 'none' });
    expect(areRunLogsHidden(true, none)).toBe(false);
  });
});

describe('featureRows', () => {
  it('gives one row per feature, in the order asked', () => {
    const rows = featureRows(LICENSE_FEATURES, undefined, []);
    expect(rows.map((row) => row.name)).toEqual(LICENSE_FEATURES);
  });

  it('does not block a feature that is used and allowed', () => {
    const rows = featureRows(['subsetting'], listLicense(['subsetting']), [
      'subsetting',
    ]);
    expect(rows).toEqual([
      { name: 'subsetting', allowed: true, inUse: true, blocking: false },
    ]);
  });

  it('blocks a feature that is used and not allowed', () => {
    const rows = featureRows(
      ['subsetting', 'job_hooks', 'sso'],
      listLicense(['subsetting']),
      ['job_hooks']
    );
    expect(rows).toEqual([
      { name: 'subsetting', allowed: true, inUse: false, blocking: false },
      { name: 'job_hooks', allowed: false, inUse: true, blocking: true },
      { name: 'sso', allowed: false, inUse: false, blocking: false },
    ]);
  });

  it('blocks nothing when the key allows every feature', () => {
    const license = create(SystemLicenseSchema, {
      isValid: true,
      state: 'valid',
      allFeatures: true,
    });
    const rows = featureRows(LICENSE_FEATURES, license, ['job_hooks', 'sso']);
    expect(rows.every((row) => row.allowed)).toBe(true);
    expect(rows.some((row) => row.blocking)).toBe(false);
    expect(rows.filter((row) => row.inUse).map((row) => row.name)).toEqual([
      'job_hooks',
      'sso',
    ]);
  });

  it('blocks for the five features that keep a job from starting', () => {
    const rows = featureRows(
      LICENSE_FEATURES,
      listLicense([]),
      LICENSE_FEATURES
    );
    expect(rows.filter((row) => row.blocking).map((row) => row.name)).toEqual([
      'job_hooks',
      'pii_text',
      'pii_detection',
      'custom_transformers',
      'subsetting',
    ]);
  });

  it('does not block for a feature in use that keeps no job from starting', () => {
    // What exists keeps working without these: only changes are refused.
    const inUse = [
      'scheduling',
      'account_hooks',
      'api_keys',
      'sso',
      'rbac',
    ] as const;
    const rows = featureRows(inUse, listLicense([]), [...inUse]);
    expect(rows.every((row) => row.inUse && !row.allowed)).toBe(true);
    expect(rows.some((row) => row.blocking)).toBe(false);
  });
});

describe('featureUseNote', () => {
  it('says nothing of a feature the account does not use', () => {
    expect(
      featureUseNote({
        name: 'sso',
        allowed: false,
        inUse: false,
        blocking: false,
      })
    ).toBe('');
  });

  it('says a feature is in use, when the license includes it or when it blocks', () => {
    expect(
      featureUseNote({
        name: 'subsetting',
        allowed: true,
        inUse: true,
        blocking: false,
      })
    ).toBe('In use by this account');
    expect(
      featureUseNote({
        name: 'job_hooks',
        allowed: false,
        inUse: true,
        blocking: true,
      })
    ).toBe('In use by this account');
  });

  it('says what happens to a feature in use that is not included and blocks nothing', () => {
    expect(
      featureUseNote({
        name: 'scheduling',
        allowed: false,
        inUse: true,
        blocking: false,
      })
    ).toBe(
      'In use, not included — what exists keeps working, changes are refused'
    );
  });
});

describe('featureNoticeMessage', () => {
  it('says the feature is not included under a license in force', () => {
    expect(featureNoticeMessage(listLicense(['subsetting']))).toBe(
      'This feature is not included in your license.'
    );
  });

  it('says no license is in force when there is none, or a frozen one', () => {
    expect(featureNoticeMessage(undefined)).toBe('No license is in force.');
    const none = create(SystemLicenseSchema, { isValid: false, state: 'none' });
    expect(featureNoticeMessage(none)).toBe('No license is in force.');
    const frozen = create(SystemLicenseSchema, {
      isValid: false,
      state: 'frozen',
      features: ['subsetting'],
    });
    expect(featureNoticeMessage(frozen)).toBe('No license is in force.');
  });
});

describe('missingKeyMessage', () => {
  it('invites to paste a key when the instance has none', () => {
    expect(missingKeyMessage(undefined)).toBe(
      'No license key is installed. You can paste one below.'
    );
    const none = create(SystemLicenseSchema, { state: 'none' });
    expect(missingKeyMessage(none)).toBe(
      'No license key is installed. You can paste one below.'
    );
  });

  it('does not claim that no key is installed when one could not be read', () => {
    const unread = create(SystemLicenseSchema, {
      state: 'none',
      problem: 'the license key is not valid base64',
    });
    expect(missingKeyMessage(unread)).toBe(
      'A license key was given and could not be read, so none is in force. You can paste one below.'
    );
  });
});

describe('isKeyAlreadyInForce', () => {
  const storedAt = timestampFromDate(new Date('2026-09-01T10:00:00Z'));
  const held = create(SystemLicenseSchema, {
    isValid: true,
    state: 'valid',
    origin: 'interface',
    installedAt: storedAt,
  });

  it('is true when the answer describes the key the page already showed', () => {
    const again = create(SystemLicenseSchema, {
      isValid: true,
      state: 'valid',
      origin: 'interface',
      installedAt: timestampFromDate(new Date('2026-09-01T10:00:00Z')),
    });
    expect(isKeyAlreadyInForce(held, again)).toBe(true);
  });

  it('is false when the key was stored since', () => {
    const newer = create(SystemLicenseSchema, {
      isValid: true,
      state: 'valid',
      origin: 'interface',
      installedAt: timestampFromDate(new Date('2026-10-07T08:00:00Z')),
    });
    expect(isKeyAlreadyInForce(held, newer)).toBe(false);
  });

  it('is false when the key came another way', () => {
    const fromFile = create(SystemLicenseSchema, {
      isValid: true,
      state: 'valid',
      origin: 'file',
      installedAt: storedAt,
    });
    expect(isKeyAlreadyInForce(fromFile, held)).toBe(false);
  });

  it('is false when the page showed no key, or could not read one', () => {
    expect(isKeyAlreadyInForce(undefined, held)).toBe(false);
    const none = create(SystemLicenseSchema, { state: 'none' });
    expect(isKeyAlreadyInForce(none, held)).toBe(false);
    expect(isKeyAlreadyInForce(none, none)).toBe(false);
    expect(isKeyAlreadyInForce(held, undefined)).toBe(false);
  });
});

describe('sourceUsage', () => {
  function usageOf(sourcesInInstance: number) {
    return create(GetLicenseUsageResponseSchema, { sourcesInInstance });
  }
  function cappedAt(maxSources: number): SystemLicense {
    return create(SystemLicenseSchema, {
      isValid: true,
      state: 'valid',
      limits: { maxSources },
    });
  }

  it('claims no limit without a license: only the count is known', () => {
    expect(sourceUsage(undefined, usageOf(7))).toEqual({
      used: 7,
      cap: undefined,
      over: false,
      limit: 'none-in-force',
    });
  });

  it('claims no limit for a key that is no longer in force, whatever it caps', () => {
    const frozen = create(SystemLicenseSchema, {
      isValid: false,
      state: 'frozen',
      limits: { maxSources: 5 },
    });
    expect(sourceUsage(frozen, usageOf(7))).toEqual({
      used: 7,
      cap: undefined,
      over: false,
      limit: 'none-in-force',
    });
    const uncappedFrozen = create(SystemLicenseSchema, {
      isValid: false,
      state: 'frozen',
    });
    expect(sourceUsage(uncappedFrozen, usageOf(7)).limit).toBe('none-in-force');
  });

  it('is capped by a key in force that sets a cap', () => {
    expect(sourceUsage(cappedAt(5), usageOf(3)).limit).toBe('capped');
  });

  it('is uncapped under a key in force that sets no cap', () => {
    expect(sourceUsage(listLicense([]), usageOf(7))).toEqual({
      used: 7,
      cap: undefined,
      over: false,
      limit: 'uncapped',
    });
  });

  it('is not over under the cap, nor at it', () => {
    expect(sourceUsage(cappedAt(5), usageOf(3))).toEqual({
      used: 3,
      cap: 5,
      over: false,
      limit: 'capped',
    });
    expect(sourceUsage(cappedAt(5), usageOf(5)).over).toBe(false);
  });

  it('is over above the cap', () => {
    expect(sourceUsage(cappedAt(5), usageOf(6))).toEqual({
      used: 6,
      cap: 5,
      over: true,
      limit: 'capped',
    });
  });

  it('holds a cap of zero for a cap', () => {
    expect(sourceUsage(cappedAt(0), usageOf(1))).toEqual({
      used: 1,
      cap: 0,
      over: true,
      limit: 'capped',
    });
  });

  it('counts nothing while the usage is not known', () => {
    expect(sourceUsage(cappedAt(5), undefined)).toEqual({
      used: 0,
      cap: 5,
      over: false,
      limit: 'capped',
    });
  });
});

describe('licenseState', () => {
  it('is none without a license', () => {
    expect(licenseState(undefined)).toBe('none');
  });

  it('is the state the API gives', () => {
    // The dates alone would read as a comfortably valid license.
    const license = create(SystemLicenseSchema, {
      isValid: true,
      state: 'grace',
      expiresAt: fromNow(365),
    });
    expect(licenseState(license)).toBe('grace');
  });

  describe('against an API that sends no state', () => {
    it('is none with no expiry and no valid license', () => {
      expect(licenseState(create(SystemLicenseSchema, {}))).toBe('none');
    });

    it('is valid far from the expiry', () => {
      const license = create(SystemLicenseSchema, {
        isValid: true,
        expiresAt: fromNow(365),
      });
      expect(licenseState(license)).toBe('valid');
    });

    it('is expiring close to the expiry', () => {
      const license = create(SystemLicenseSchema, {
        isValid: true,
        expiresAt: fromNow(10),
      });
      expect(licenseState(license)).toBe('expiring');
    });

    it('is in grace past the expiry while still valid', () => {
      const license = create(SystemLicenseSchema, {
        isValid: true,
        expiresAt: fromNow(-2),
      });
      expect(licenseState(license)).toBe('grace');
    });

    it('is frozen past the expiry once no longer valid', () => {
      const license = create(SystemLicenseSchema, {
        isValid: false,
        expiresAt: fromNow(-60),
      });
      expect(licenseState(license)).toBe('frozen');
    });
  });
});

describe('limitsInForce', () => {
  const limits = create(LicenseLimitsSchema, { maxSources: 3, maxJobs: 10 });

  it('gives the caps of a license in force', () => {
    const license = create(SystemLicenseSchema, {
      isValid: true,
      state: 'valid',
      limits,
    });
    expect(limitsInForce(license)).toBe(limits);
  });

  it('gives none for a key that is not in force, whatever it caps', () => {
    const frozen = create(SystemLicenseSchema, {
      isValid: false,
      state: 'frozen',
      limits,
    });
    expect(limitsInForce(frozen)).toBeUndefined();
    expect(limitsInForce(undefined)).toBeUndefined();
  });
});

describe('roles without the rbac feature', () => {
  it('lets only the administrator role be chosen', () => {
    expect(isRoleSelectable(false, AccountRole.ADMIN)).toBe(true);
    expect(isRoleSelectable(false, AccountRole.JOB_DEVELOPER)).toBe(false);
    expect(isRoleSelectable(false, AccountRole.JOB_EXECUTOR)).toBe(false);
    expect(isRoleSelectable(false, AccountRole.JOB_VIEWER)).toBe(false);
  });

  it('lets every role be chosen with the feature', () => {
    expect(isRoleSelectable(true, AccountRole.JOB_VIEWER)).toBe(true);
    expect(isRoleSelectable(true, AccountRole.ADMIN)).toBe(true);
  });

  it('names no role in an invitation for a role that cannot be chosen', () => {
    expect(invitationRole(false, AccountRole.JOB_VIEWER)).toBe(
      AccountRole.UNSPECIFIED
    );
    expect(invitationRole(false, AccountRole.ADMIN)).toBe(AccountRole.ADMIN);
    expect(invitationRole(true, AccountRole.JOB_VIEWER)).toBe(
      AccountRole.JOB_VIEWER
    );
  });
});
