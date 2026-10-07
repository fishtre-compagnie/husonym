import { create } from '@bufbuild/protobuf';
import { timestampFromDate } from '@bufbuild/protobuf/wkt';
import {
  GetLicenseUsageResponseSchema,
  SystemLicense,
  SystemLicenseSchema,
} from '@husonym/sdk';
import {
  featureLabel,
  featureRows,
  isFeatureAllowed,
  LICENSE_FEATURES,
  licenseState,
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

  it('shows an unknown name as it is', () => {
    expect(featureLabel('time_travel')).toBe('time_travel');
  });
});

describe('isFeatureAllowed', () => {
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

  it('has no cap when the key sets none', () => {
    expect(sourceUsage(listLicense([]), usageOf(7))).toEqual({
      used: 7,
      cap: undefined,
      over: false,
    });
    expect(sourceUsage(undefined, usageOf(7)).over).toBe(false);
  });

  it('is not over under the cap, nor at it', () => {
    expect(sourceUsage(cappedAt(5), usageOf(3))).toEqual({
      used: 3,
      cap: 5,
      over: false,
    });
    expect(sourceUsage(cappedAt(5), usageOf(5)).over).toBe(false);
  });

  it('is over above the cap', () => {
    expect(sourceUsage(cappedAt(5), usageOf(6))).toEqual({
      used: 6,
      cap: 5,
      over: true,
    });
  });

  it('holds a cap of zero for a cap', () => {
    expect(sourceUsage(cappedAt(0), usageOf(1))).toEqual({
      used: 1,
      cap: 0,
      over: true,
    });
  });

  it('counts nothing while the usage is not known', () => {
    expect(sourceUsage(cappedAt(5), undefined)).toEqual({
      used: 0,
      cap: 5,
      over: false,
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
