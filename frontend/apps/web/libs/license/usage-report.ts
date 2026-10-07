import { UsageReportingMode, UsageReportStatus } from '@husonym/sdk';
import type {
  GetUsageReportingResponse,
  UsageReportSummary,
} from '@husonym/sdk';

type ReportDay = NonNullable<UsageReportSummary['day']>;

// How the mode in force reads on the page, and how it stands against what the license
// provides.
export function reportingLabel(
  mode: UsageReportingMode,
  licenseMode: UsageReportingMode,
  belowLicense: boolean
): { text: string; note?: string } {
  const text = modeLabel(mode);
  // Without a mode there is nothing to set against the license.
  if (mode === UsageReportingMode.UNSPECIFIED || !licenseMode) {
    return { text };
  }
  return {
    text,
    note: belowLicense
      ? 'set below what your license provides'
      : 'as your license provides',
  };
}

function modeLabel(mode: UsageReportingMode): string {
  switch (mode) {
    case UsageReportingMode.ONLINE:
      return 'Sent daily';
    case UsageReportingMode.OFFLINE_REPORT:
      return 'Not sent — a report file is expected';
    case UsageReportingMode.NONE:
      return 'Not sent';
    default:
      return 'No report is prepared';
  }
}

// What the license key provides for the usage report, for the Status card. Nothing
// without a key in force.
export function licensedReportingLabel(
  licenseMode: UsageReportingMode
): string | undefined {
  switch (licenseMode) {
    case UsageReportingMode.ONLINE:
      return 'Sent daily';
    case UsageReportingMode.OFFLINE_REPORT:
      return 'Report file';
    case UsageReportingMode.NONE:
      return 'None';
    default:
      return undefined;
  }
}

type ReportStatusLabel = {
  text: string;
  variant: 'success' | 'warning' | 'secondary';
};

export function reportStatusLabel(
  status: UsageReportStatus
): ReportStatusLabel {
  switch (status) {
    case UsageReportStatus.SENT:
      return { text: 'sent', variant: 'success' };
    case UsageReportStatus.TO_BE_SENT:
      return { text: 'to be sent', variant: 'secondary' };
    case UsageReportStatus.NOT_SENT:
      return { text: 'not sent', variant: 'warning' };
    case UsageReportStatus.KEPT:
      return { text: 'kept', variant: 'secondary' };
    default:
      return { text: 'unknown', variant: 'secondary' };
  }
}

// The banner above the card. Only one is shown: silence, if it holds, comes before a
// mode set below the license.
export function reportingNotice(
  reporting: Pick<
    GetUsageReportingResponse,
    'mode' | 'licenseMode' | 'belowLicense' | 'silent'
  >
): { title: string; description: string } | undefined {
  if (reporting.silent) {
    return {
      title: 'No usage report could be sent for 30 days',
      description: 'The reports are kept and tried again. Nothing is blocked.',
    };
  }
  if (reporting.belowLicense) {
    const provided =
      reporting.licenseMode === UsageReportingMode.OFFLINE_REPORT
        ? 'a usage report file'
        : 'a daily usage report';
    return {
      title: `Your license provides for ${provided}`,
      description: `Reporting is set to "${modeLabel(reporting.mode)}" on this instance. Nothing is blocked.`,
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
