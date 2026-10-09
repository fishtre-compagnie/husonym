'use client';

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { useUsageReporting } from '@/libs/hooks/useUsageReporting';
import { reportingNotice } from '@/libs/license/usage-report';
import { GetUsageReportingResponse } from '@husonym/sdk';
import { ReactElement } from 'react';

// The notice about the usage report, when there is one: at most one, and it blocks
// nothing. The License page and the Usage pages show the same.
export function ReportingNoticeAlert({
  reporting,
}: {
  reporting?: GetUsageReportingResponse;
}): ReactElement | null {
  const notice = reporting ? reportingNotice(reporting) : undefined;
  if (!notice) {
    return null;
  }
  return (
    <Alert variant="warning">
      <AlertTitle>{notice.title}</AlertTitle>
      <AlertDescription>{notice.description}</AlertDescription>
    </Alert>
  );
}

// The notice on a page that reads the reporting for nothing else. While it is read, or
// when it cannot be, there is nothing to say.
export default function ReportingNotice({
  accountId,
}: {
  accountId: string;
}): ReactElement | null {
  const { data } = useUsageReporting(accountId);
  return <ReportingNoticeAlert reporting={data} />;
}
