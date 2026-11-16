import { timestampDate } from '@bufbuild/protobuf/wkt';
import { UsageReportingMode, UsageReportStatus } from '@husonym/sdk';
import type {
  GetUsageReportingResponse,
  UsageReportSummary,
} from '@husonym/sdk';
import { ENGLISH_LICENSE_T, LicenseTranslator } from './i18n';

type ReportDay = NonNullable<UsageReportSummary['day']>;

// How the mode in force reads on the page, and how it stands against what the license
// provides.
export function reportingLabel(
  mode: UsageReportingMode,
  licenseMode: UsageReportingMode,
  belowLicense: boolean,
  t: LicenseTranslator = ENGLISH_LICENSE_T
): { text: string; note?: string } {
  const text = modeLabel(mode, t);
  // Without a mode there is nothing to set against the license.
  if (mode === UsageReportingMode.UNSPECIFIED || !licenseMode) {
    return { text };
  }
  return {
    text,
    note: belowLicense ? t('report.belowLicense') : t('report.asLicense'),
  };
}

function modeLabel(mode: UsageReportingMode, t: LicenseTranslator): string {
  switch (mode) {
    case UsageReportingMode.ONLINE:
      return t('report.modes.online');
    case UsageReportingMode.OFFLINE_REPORT:
      return t('report.modes.offline');
    case UsageReportingMode.NONE:
      return t('report.modes.none');
    default:
      return t('report.modes.unspecified');
  }
}

// What the license key provides for the usage report, for the Status card. Nothing
// without a key in force.
export function licensedReportingLabel(
  licenseMode: UsageReportingMode,
  t: LicenseTranslator = ENGLISH_LICENSE_T
): string | undefined {
  switch (licenseMode) {
    case UsageReportingMode.ONLINE:
      return t('report.licensedModes.online');
    case UsageReportingMode.OFFLINE_REPORT:
      return t('report.licensedModes.offline');
    case UsageReportingMode.NONE:
      return t('report.licensedModes.none');
    default:
      return undefined;
  }
}

type ReportStatusLabel = {
  text: string;
  variant: 'success' | 'warning' | 'secondary';
};

export function reportStatusLabel(
  status: UsageReportStatus,
  t: LicenseTranslator = ENGLISH_LICENSE_T
): ReportStatusLabel {
  switch (status) {
    case UsageReportStatus.SENT:
      return { text: t('report.statuses.sent'), variant: 'success' };
    case UsageReportStatus.TO_BE_SENT:
      return { text: t('report.statuses.toBeSent'), variant: 'secondary' };
    case UsageReportStatus.NOT_SENT:
      return { text: t('report.statuses.notSent'), variant: 'warning' };
    case UsageReportStatus.KEPT:
      return { text: t('report.statuses.kept'), variant: 'secondary' };
    default:
      return { text: t('report.statuses.unknown'), variant: 'secondary' };
  }
}

// The banner above the card. Only one is shown: silence, if it holds, comes before a
// mode set below the license.
export function reportingNotice(
  reporting: Pick<
    GetUsageReportingResponse,
    'mode' | 'licenseMode' | 'belowLicense' | 'silent'
  >,
  t: LicenseTranslator = ENGLISH_LICENSE_T
): { title: string; description: string } | undefined {
  if (reporting.silent) {
    return {
      title: t('report.notice.silentTitle'),
      description: t('report.notice.silentDescription'),
    };
  }
  if (reporting.belowLicense) {
    return {
      title:
        reporting.licenseMode === UsageReportingMode.OFFLINE_REPORT
          ? t('report.notice.providesFile')
          : t('report.notice.providesDaily'),
      description: t('report.notice.belowDescription', {
        mode: modeLabel(reporting.mode, t),
      }),
    };
  }
  return undefined;
}

// The report to come: the newest one that is waiting to be sent or was tried and not
// sent. The reports come newest first.
export function nextReport(
  reports: readonly UsageReportSummary[]
): UsageReportSummary | undefined {
  return reports.find(
    (report) =>
      report.status === UsageReportStatus.TO_BE_SENT ||
      report.status === UsageReportStatus.NOT_SENT
  );
}

// The UTC day a report counts, as YYYY-MM-DD.
export function reportDayLabel(day: ReportDay | undefined): string {
  if (!day) {
    return '';
  }
  const pad = (n: number, width: number): string =>
    String(n).padStart(width, '0');
  return `${pad(day.year, 4)}-${pad(day.month, 2)}-${pad(day.day, 2)}`;
}

// Indentation for the screen only: the document is sealed as one line, and what is
// copied or downloaded is the string received. A document that is not JSON is shown
// as it is.
export function prettyDocument(document: string): string {
  try {
    return JSON.stringify(JSON.parse(document), null, 2);
  } catch {
    return document;
  }
}

// The seal and the fingerprint of a key are 64 lowercase hexadecimal characters.
const SEAL_FORM = /^[0-9a-f]{64}$/;

// The file of a period: the document exactly as received, then its seal, one line
// each. The document goes through no parser, so its bytes stay the ones that were
// sealed. The seal and the fingerprint are written between quotes without escaping,
// so anything but their form is refused.
export function periodFileContent(
  document: string,
  seal: string,
  keyFingerprint: string
): string {
  if (!SEAL_FORM.test(seal) || !SEAL_FORM.test(keyFingerprint)) {
    throw new Error('the seal of the report is not in the expected form');
  }
  return `${document}\n{"seal":"${seal}","key_fingerprint":"${keyFingerprint}"}\n`;
}

export function periodFileName(fromMonth: string, toMonth: string): string {
  return `usage-report-${fromMonth}-${toMonth}.json`;
}

// The moment the first report may leave, while it is ahead. The field is the earliest
// moment the oldest report still to be sent may leave: once past, the report has not
// left yet, and a date under "First report" would read as the day it did. Only an
// instance that sends has one.
export function firstReportAt(
  reporting: Pick<GetUsageReportingResponse, 'mode' | 'firstSendAt'>,
  now: Date
): Date | undefined {
  if (reporting.mode !== UsageReportingMode.ONLINE || !reporting.firstSendAt) {
    return undefined;
  }
  const at = timestampDate(reporting.firstSendAt);
  return at.getTime() > now.getTime() ? at : undefined;
}
