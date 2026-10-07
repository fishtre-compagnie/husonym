'use client';

import { siteConfig } from '@/app/config/site';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Skeleton } from '@/components/ui/skeleton';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { formatDate } from '@/libs/license/license';
import {
  nextReport,
  periodFileContent,
  periodFileName,
  prettyDocument,
  reportDayLabel,
  reportingLabel,
  reportingNotice,
  reportStatusLabel,
} from '@/libs/license/usage-report';
import { getErrorMessage } from '@/util/util';
import { Timestamp, timestampDate } from '@bufbuild/protobuf/wkt';
import { useMutation, useQuery } from '@connectrpc/connect-query';
import {
  Code,
  ConnectError,
  GetUsageReportingResponse,
  UsageReportingMode,
  UsageReportStatus,
  UsageReportSummary,
  UsageService,
} from '@husonym/sdk';
import Link from 'next/link';
import { Fragment, ReactElement, useState } from 'react';
import { toast } from 'sonner';

interface Props {
  accountId: string;
  reporting?: GetUsageReportingResponse;
  isLoading: boolean;
  // Why the reporting could not be read. The other cards are not affected.
  error: unknown;
}

const WARNING_BORDER = 'border-yellow-300 dark:border-orange-500';

export default function UsageReportCard(props: Props): ReactElement {
  const { accountId, reporting, isLoading, error } = props;

  if (isLoading) {
    return <Skeleton className="w-full h-48" />;
  }

  const notice = reporting ? reportingNotice(reporting) : undefined;

  return (
    <div className="flex flex-col gap-5">
      {notice && (
        <Alert variant="warning">
          <AlertTitle>{notice.title}</AlertTitle>
          <AlertDescription>{notice.description}</AlertDescription>
        </Alert>
      )}
      <Card>
        <CardHeader>
          <CardTitle>Usage report</CardTitle>
          <CardDescription>
            Once a day, this instance prepares a report made of counts. It never
            holds a name, a query or an error message.{' '}
            <Link
              href={`${siteConfig.links.docs}/deploy/usage-report`}
              target="_blank"
              rel="noopener noreferrer"
              className="underline"
            >
              What it contains
            </Link>
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-6">
          {error ? (
            <Alert variant="warning">
              <AlertTitle>Unable to read the usage report</AlertTitle>
              <AlertDescription>{getErrorMessage(error)}</AlertDescription>
            </Alert>
          ) : reporting && reporting.mode !== UsageReportingMode.UNSPECIFIED ? (
            <>
              <ReportingDetails accountId={accountId} reporting={reporting} />
              <ReportsTable accountId={accountId} reports={reporting.reports} />
              <PeriodDownload accountId={accountId} />
            </>
          ) : (
            <p className="text-sm">
              No report is prepared: no license key is in force.
            </p>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

function ReportingDetails({
  accountId,
  reporting,
}: {
  accountId: string;
  reporting: GetUsageReportingResponse;
}): ReactElement {
  const label = reportingLabel(
    reporting.mode,
    reporting.licenseMode,
    reporting.belowLicense
  );
  const sends = reporting.mode === UsageReportingMode.ONLINE;
  const next = nextReport(reporting.reports);
  const firstAt = formatTimestamp(reporting.firstSendAt);

  return (
    <dl className="grid grid-cols-[max-content_1fr] gap-x-8 gap-y-2 text-sm">
      <dt className="text-muted-foreground">Reporting</dt>
      <dd>
        {label.text}
        {label.note && (
          <span className="text-muted-foreground"> — {label.note}</span>
        )}
      </dd>
      {sends && (
        <>
          <dt className="text-muted-foreground">Last sent</dt>
          <dd>{formatTimestamp(reporting.lastSentAt) ?? 'Never'}</dd>
        </>
      )}
      {sends && next && (
        <>
          <dt className="text-muted-foreground">Next report</dt>
          <dd>
            <ReportLine accountId={accountId} report={next} />
          </dd>
        </>
      )}
      {/* The API sets first_send_at only while it is in the future, in the first 24
          hours of sending: its presence is the whole condition. */}
      {sends && firstAt && (
        <>
          <dt className="text-muted-foreground">First report</dt>
          <dd>{firstAt}</dd>
        </>
      )}
    </dl>
  );
}

// A report with its status and the link that opens its document.
function ReportLine({
  accountId,
  report,
}: {
  accountId: string;
  report: UsageReportSummary;
}): ReactElement {
  const [open, setOpen] = useState(false);
  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-row items-center gap-3">
        <span>{reportDayLabel(report.day)}</span>
        <StatusBadge status={report.status} />
        <ShowButton open={open} onClick={() => setOpen(!open)} />
      </div>
      {open && <ReportDocument accountId={accountId} report={report} />}
    </div>
  );
}

function ReportsTable({
  accountId,
  reports,
}: {
  accountId: string;
  reports: UsageReportSummary[];
}): ReactElement {
  const [openDay, setOpenDay] = useState<string>();

  return (
    <div className="flex flex-col gap-2">
      <span className="text-sm font-medium">Last 30 days</span>
      {reports.length === 0 ? (
        <p className="text-muted-foreground text-sm">No report yet.</p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Day</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Sent</TableHead>
              <TableHead />
            </TableRow>
          </TableHeader>
          <TableBody>
            {reports.map((report) => {
              const day = reportDayLabel(report.day);
              const isOpen = openDay === day;
              return (
                <Fragment key={day}>
                  <TableRow>
                    <TableCell>{day}</TableCell>
                    <TableCell>
                      <StatusBadge status={report.status} />
                    </TableCell>
                    <TableCell>{sentColumn(report)}</TableCell>
                    <TableCell className="text-right">
                      <ShowButton
                        open={isOpen}
                        onClick={() => setOpenDay(isOpen ? undefined : day)}
                      />
                    </TableCell>
                  </TableRow>
                  {isOpen && (
                    <TableRow>
                      <TableCell colSpan={4}>
                        <ReportDocument accountId={accountId} report={report} />
                      </TableCell>
                    </TableRow>
                  )}
                </Fragment>
              );
            })}
          </TableBody>
        </Table>
      )}
    </div>
  );
}

// The date a report was sent on, or how many times it was tried when it was not.
function sentColumn(report: UsageReportSummary): string {
  const sentAt = formatTimestamp(report.sentAt);
  if (sentAt) {
    return sentAt;
  }
  if (report.status === UsageReportStatus.NOT_SENT && report.attempts > 0) {
    return `${report.attempts} ${report.attempts === 1 ? 'attempt' : 'attempts'}`;
  }
  return '';
}

function StatusBadge({ status }: { status: UsageReportStatus }): ReactElement {
  const { text, variant } = reportStatusLabel(status);
  return variant === 'warning' ? (
    <Badge variant="outline" className={WARNING_BORDER}>
      {text}
    </Badge>
  ) : (
    <Badge variant={variant}>{text}</Badge>
  );
}

function ShowButton({
  open,
  onClick,
}: {
  open: boolean;
  onClick(): void;
}): ReactElement {
  return (
    <Button
      type="button"
      variant="link"
      size="sm"
      className="h-auto p-0"
      onClick={onClick}
    >
      {open ? 'Hide' : 'Show'}
    </Button>
  );
}

// Mounted only while a report is open: the document is read when it is asked for.
function ReportDocument({
  accountId,
  report,
}: {
  accountId: string;
  report: UsageReportSummary;
}): ReactElement {
  const { data, isLoading, error } = useQuery(
    UsageService.method.getUsageReport,
    { accountId, day: report.day },
    { retry: false }
  );

  if (isLoading) {
    return <Skeleton className="w-full h-32" />;
  }
  if (error || !data) {
    return (
      <Alert variant="warning">
        <AlertTitle>Unable to read this report</AlertTitle>
        <AlertDescription>{refusalMessage(error)}</AlertDescription>
      </Alert>
    );
  }

  // The string received, not the indented copy.
  const received = data.document;

  async function onCopy(): Promise<void> {
    try {
      await navigator.clipboard.writeText(received);
      toast.success('Report copied');
    } catch (err) {
      toast.error('Unable to copy the report', {
        description: getErrorMessage(err),
      });
    }
  }

  return (
    <div className="flex flex-col gap-2">
      <pre className="bg-muted max-h-96 overflow-auto rounded-md p-3 text-xs">
        {prettyDocument(received)}
      </pre>
      <p className="text-muted-foreground text-xs">
        Indented for reading. The report is sealed as one line.
      </p>
      <div>
        <Button type="button" variant="outline" size="sm" onClick={onCopy}>
          Copy
        </Button>
      </div>
    </div>
  );
}

function PeriodDownload({ accountId }: { accountId: string }): ReactElement {
  const { mutateAsync: getPeriodReport, isPending } = useMutation(
    UsageService.method.getUsagePeriodReport
  );
  const [open, setOpen] = useState(false);
  const [fromMonth, setFromMonth] = useState('');
  const [toMonth, setToMonth] = useState('');
  const [refusal, setRefusal] = useState<string>();

  async function onDownload(): Promise<void> {
    setRefusal(undefined);
    try {
      const resp = await getPeriodReport({ accountId, fromMonth, toMonth });
      downloadFile(
        periodFileContent(resp.document, resp.seal, resp.keyFingerprint),
        periodFileName(fromMonth, toMonth)
      );
    } catch (err) {
      setRefusal(refusalMessage(err));
    }
  }

  if (!open) {
    return (
      <div>
        <Button type="button" variant="outline" onClick={() => setOpen(true)}>
          Download a report for a period…
        </Button>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      {refusal && (
        <Alert variant="destructive">
          <AlertTitle>The report could not be downloaded</AlertTitle>
          <AlertDescription>{refusal}</AlertDescription>
        </Alert>
      )}
      <div className="flex flex-row flex-wrap items-end gap-4">
        <MonthField
          id="usage-report-from"
          label="From"
          onChange={setFromMonth}
        />
        <MonthField id="usage-report-to" label="To" onChange={setToMonth} />
        <Button
          type="button"
          disabled={!fromMonth || !toMonth || isPending}
          onClick={onDownload}
        >
          Download
        </Button>
      </div>
    </div>
  );
}

function MonthField({
  id,
  label,
  onChange,
}: {
  id: string;
  label: string;
  onChange(value: string): void;
}): ReactElement {
  return (
    <div className="flex flex-col gap-2">
      <Label htmlFor={id}>{label}</Label>
      <Input id={id} type="month" onChange={(e) => onChange(e.target.value)} />
    </div>
  );
}

// Create and download the file of the period.
function downloadFile(content: string, filename: string): void {
  const blob = new Blob([content], { type: 'application/json;charset=utf-8;' });
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.setAttribute('href', url);
  link.setAttribute('download', filename);
  link.style.visibility = 'hidden';
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  // Once the click is handled, not in the middle of it.
  setTimeout(() => URL.revokeObjectURL(url), 0);
}

// The message of the API is written to be read as it is.
function refusalMessage(err: unknown): string {
  if (err instanceof ConnectError && err.code !== Code.PermissionDenied) {
    return err.rawMessage;
  }
  return getErrorMessage(err);
}

function formatTimestamp(timestamp: Timestamp | undefined): string | undefined {
  if (!timestamp) {
    return undefined;
  }
  const date = timestampDate(timestamp);
  return date.getTime() === 0 ? undefined : formatDate(date);
}
