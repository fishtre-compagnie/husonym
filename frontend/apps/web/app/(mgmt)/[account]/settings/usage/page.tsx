'use client';
import SubPageHeader from '@/components/headers/SubPageHeader';
import { useAccount } from '@/components/providers/account-provider';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Skeleton } from '@/components/ui/skeleton';
import ReportingNotice from '@/components/usage/ReportingNotice';
import RowsPerDayChart from '@/components/usage/RowsPerDayChart';
import UsagePeriodSelect from '@/components/usage/UsagePeriodSelect';
import UsageTiles from '@/components/usage/UsageTiles';
import {
  browserTimeZone,
  DEFAULT_USAGE_PERIOD,
  periodRange,
  rangeLabel,
  UsagePeriod,
} from '@/libs/usage/period';
import { errorRows, errorsEmptyLine, refusalRows } from '@/libs/usage/rows';
import { noRunLine } from '@/libs/usage/totals';
import { getErrorMessage } from '@/util/util';
import { useQuery } from '@connectrpc/connect-query';
import { GetAccountUsageResponse, UsageService } from '@husonym/sdk';
import { ReactElement, useState } from 'react';
import CountsCard from './components/CountsCard';
import JobsUsageTable from './components/JobsUsageTable';

// What the account ran over a period, from the counters of the instance. The days are
// the viewer's: the period is cut in the zone of the browser, and the line under the
// title says the zone the API counted the days in, which is UTC when it does not know
// the one it was asked.
export default function UsagePage(): ReactElement {
  const { account } = useAccount();
  const accountId = account?.id ?? '';
  const [period, setPeriod] = useState<UsagePeriod>(DEFAULT_USAGE_PERIOD);
  const timeZone = browserTimeZone();
  const range = periodRange(period, new Date(), timeZone);
  const { data, error } = useQuery(
    UsageService.method.getAccountUsage,
    { accountId, fromDay: range.fromDay, toDay: range.toDay, timeZone },
    { enabled: !!accountId, retry: false }
  );

  return (
    <div className="flex flex-col gap-5">
      <ReportingNotice accountId={accountId} />
      <SubPageHeader
        header="Usage"
        description="What this account ran, from the counters of this instance"
        subHeadings={
          error ? (
            rangeLabel(range, undefined)
          ) : data ? (
            rangeLabel(range, data.timeZone)
          ) : (
            <Skeleton className="h-5 w-72" />
          )
        }
        extraHeading={
          <UsagePeriodSelect period={period} setPeriod={setPeriod} />
        }
      />
      {error ? (
        <Alert variant="destructive">
          <AlertTitle>Unable to read the usage of this account</AlertTitle>
          <AlertDescription>{getErrorMessage(error)}</AlertDescription>
        </Alert>
      ) : data ? (
        <AccountUsage accountName={account?.name ?? ''} usage={data} />
      ) : (
        <UsageSkeleton />
      )}
    </div>
  );
}

function AccountUsage({
  accountName,
  usage,
}: {
  accountName: string;
  usage: GetAccountUsageResponse;
}): ReactElement {
  const refusals = refusalRows(usage.refusals);
  return (
    <>
      <UsageTiles
        totals={usage.totals}
        duration={{ label: 'Run time', seconds: usage.durationTotalSeconds }}
      />
      <RowsPerDayChart days={usage.days} emptyLine={noRunLine(usage.totals)} />
      <JobsUsageTable
        accountName={accountName}
        jobs={usage.jobs}
        totals={usage.totals}
      />
      <CountsCard
        title="Errors by category"
        columns={['Category', 'Runs']}
        rows={errorRows(usage.errors)}
        emptyLine={errorsEmptyLine(usage.totals)}
      />
      {refusals.length > 0 && (
        <CountsCard
          title="License refusals"
          description="Counted by UTC day, when a call reaches the API. A screen that is greyed out makes no call."
          columns={['Refused', 'Times']}
          rows={refusals}
        />
      )}
    </>
  );
}

// The shape of the page while it is read: the tiles, then a card each.
function UsageSkeleton(): ReactElement {
  return (
    <>
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} className="h-24 w-full" />
        ))}
      </div>
      <Skeleton className="h-96 w-full" />
      <Skeleton className="h-48 w-full" />
      <Skeleton className="h-32 w-full" />
    </>
  );
}
