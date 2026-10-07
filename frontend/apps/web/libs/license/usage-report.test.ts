import { create } from '@bufbuild/protobuf';
import {
  GetUsageReportingResponseSchema,
  UsageReportingMode,
  UsageReportStatus,
  UsageReportSummarySchema,
} from '@husonym/sdk';
import {
  licensedReportingLabel,
  nextReport,
  periodFileContent,
  periodFileName,
  prettyDocument,
  reportDayLabel,
  reportingLabel,
  reportingNotice,
  reportStatusLabel,
} from './usage-report';

const { UNSPECIFIED, ONLINE, OFFLINE_REPORT, NONE } = UsageReportingMode;

function reporting(fields: Parameters<typeof create>[1]) {
  return create(GetUsageReportingResponseSchema, fields as never);
}

describe('reportingLabel', () => {
  it.each([
    [ONLINE, ONLINE, false, 'Sent daily', 'as your license provides'],
    [
      OFFLINE_REPORT,
      ONLINE,
      true,
      'Not sent — a report file is expected',
      'set below what your license provides',
    ],
    [NONE, ONLINE, true, 'Not sent', 'set below what your license provides'],
    [
      OFFLINE_REPORT,
      OFFLINE_REPORT,
      false,
      'Not sent — a report file is expected',
      'as your license provides',
    ],
    [
      NONE,
      OFFLINE_REPORT,
      true,
      'Not sent',
      'set below what your license provides',
    ],
    [NONE, NONE, false, 'Not sent', 'as your license provides'],
  ])(
    'mode %s under license %s (below %s)',
    (mode, license, below, text, note) => {
      expect(reportingLabel(mode, license, below)).toEqual({ text, note });
    }
  );

  it('gives no note when the license mode is unspecified', () => {
    expect(reportingLabel(ONLINE, UNSPECIFIED, false)).toEqual({
      text: 'Sent daily',
    });
  });

  it('says no report is prepared, with no note, when no key is in force', () => {
    expect(reportingLabel(UNSPECIFIED, UNSPECIFIED, false)).toEqual({
      text: 'No report is prepared',
    });
  });
});

describe('licensedReportingLabel', () => {
  it.each([
    [ONLINE, 'Sent daily'],
    [OFFLINE_REPORT, 'Report file'],
    [NONE, 'None'],
    [UNSPECIFIED, undefined],
  ])('license mode %s', (mode, label) => {
    expect(licensedReportingLabel(mode)).toBe(label);
  });
});

describe('reportStatusLabel', () => {
  it.each([
    [UsageReportStatus.SENT, 'sent', 'success'],
    [UsageReportStatus.TO_BE_SENT, 'to be sent', 'secondary'],
    [UsageReportStatus.NOT_SENT, 'not sent', 'warning'],
    [UsageReportStatus.KEPT, 'kept', 'secondary'],
    [UsageReportStatus.UNSPECIFIED, 'unknown', 'secondary'],
  ])('status %s', (status, text, variant) => {
    expect(reportStatusLabel(status)).toEqual({ text, variant });
  });
});

describe('reportingNotice', () => {
  it('has none when the setting is not below the license and the instance is not silent', () => {
    expect(
      reportingNotice(reporting({ mode: ONLINE, licenseMode: ONLINE }))
    ).toBeUndefined();
  });

  it('has none without a key in force', () => {
    expect(reportingNotice(reporting({}))).toBeUndefined();
  });

  it('warns of a daily report the setting lowers', () => {
    expect(
      reportingNotice(
        reporting({ mode: NONE, licenseMode: ONLINE, belowLicense: true })
      )
    ).toEqual({
      title: 'Your license provides for a daily usage report',
      description:
        'Reporting is set to "Not sent" on this instance. Nothing is blocked.',
    });
  });

  it('warns of a report file the setting lowers', () => {
    expect(
      reportingNotice(
        reporting({
          mode: NONE,
          licenseMode: OFFLINE_REPORT,
          belowLicense: true,
        })
      )?.title
    ).toBe('Your license provides for a usage report file');
  });

  it('puts the silent days before a setting below the license', () => {
    expect(
      reportingNotice(
        reporting({
          mode: NONE,
          licenseMode: ONLINE,
          belowLicense: true,
          silent: true,
        })
      )?.title
    ).toBe('No usage report could be sent for 30 days');
  });

  it('warns of 30 silent days', () => {
    expect(
      reportingNotice(
        reporting({ mode: ONLINE, licenseMode: ONLINE, silent: true })
      )
    ).toEqual({
      title: 'No usage report could be sent for 30 days',
      description: 'The reports are kept and tried again. Nothing is blocked.',
    });
  });
});

describe('nextReport', () => {
  const day = (d: number, status: UsageReportStatus) =>
    create(UsageReportSummarySchema, {
      day: { year: 2026, month: 10, day: d } as never,
      status,
    });

  it('is the newest report waiting or not sent', () => {
    const reports = [
      day(7, UsageReportStatus.TO_BE_SENT),
      day(6, UsageReportStatus.NOT_SENT),
      day(5, UsageReportStatus.SENT),
    ];
    expect(nextReport(reports)).toBe(reports[0]);
  });

  it('is nothing when every report is sent or kept', () => {
    expect(
      nextReport([
        day(7, UsageReportStatus.KEPT),
        day(6, UsageReportStatus.SENT),
      ])
    ).toBeUndefined();
  });
});

describe('reportDayLabel', () => {
  it('pads the month and the day', () => {
    expect(reportDayLabel({ year: 2026, month: 3, day: 9 } as never)).toBe(
      '2026-03-09'
    );
  });

  it('is empty for no day', () => {
    expect(reportDayLabel(undefined)).toBe('');
  });
});

describe('prettyDocument', () => {
  it('indents a JSON document', () => {
    expect(prettyDocument('{"a":1,"b":[2]}')).toBe(
      '{\n  "a": 1,\n  "b": [\n    2\n  ]\n}'
    );
  });

  it('gives a string that is not JSON back as it is', () => {
    expect(prettyDocument('{"a":')).toBe('{"a":');
    expect(prettyDocument('')).toBe('');
  });
});

const SEAL = 'ab01'.repeat(16);
const FINGERPRINT = '9f'.repeat(32);

describe('periodFileContent', () => {
  it('writes the document untouched, then the seal, one line each', () => {
    expect(periodFileContent('{"a":1}', SEAL, FINGERPRINT)).toBe(
      `{"a":1}\n{"seal":"${SEAL}","key_fingerprint":"${FINGERPRINT}"}\n`
    );
  });

  it.each([
    ['a quote', `${'a'.repeat(63)}"`],
    ['a backslash', `${'a'.repeat(63)}\\`],
    ['the wrong length', 'ab01'.repeat(15)],
    ['upper case', SEAL.toUpperCase()],
    ['nothing', ''],
  ])('refuses a seal with %s', (_, seal) => {
    expect(() => periodFileContent('{}', seal, FINGERPRINT)).toThrow(
      'the seal of the report is not in the expected form'
    );
  });

  it('refuses a fingerprint that is not 64 lowercase hexadecimal characters', () => {
    expect(() => periodFileContent('{}', SEAL, 'FP')).toThrow(
      'the seal of the report is not in the expected form'
    );
  });

  it('does not check the document', () => {
    expect(
      periodFileContent('not json', SEAL, FINGERPRINT).split('\n')[0]
    ).toBe('not json');
  });

  it('keeps escaped characters, spacing and key order of the document', () => {
    const document =
      '{"b":"a\\u00e9\\n\\"q\\"","a": 1.0,"big":12345678901234567890}';
    const content = periodFileContent(document, SEAL, FINGERPRINT);
    expect(content.split('\n')[0]).toBe(document);
    expect(content.endsWith('\n')).toBe(true);
    expect(content.split('\n')).toHaveLength(3);
  });
});

describe('periodFileName', () => {
  it('names the period', () => {
    expect(periodFileName('2026-08', '2026-10')).toBe(
      'usage-report-2026-08-2026-10.json'
    );
  });
});
