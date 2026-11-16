'use client';
import SubPageHeader from '@/components/headers/SubPageHeader';
import JobNotFoundAlert from '@/components/jobs/JobNotFoundAlert';
import { useAccount } from '@/components/providers/account-provider';
import { PageProps } from '@/components/types';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Skeleton } from '@/components/ui/skeleton';
import ReportingNotice from '@/components/usage/ReportingNotice';
import RowsPerDayChart from '@/components/usage/RowsPerDayChart';
import UsagePeriodSelect from '@/components/usage/UsagePeriodSelect';
import UsageSkeleton from '@/components/usage/UsageSkeleton';
import UsageTiles from '@/components/usage/UsageTiles';
import {
  browserTimeZone,
  DEFAULT_USAGE_PERIOD,
  periodRange,
  rangeLabel,
  UsagePeriod,
} from '@/libs/usage/period';
import { isUnknownJob, noRowsPerDayLine, noRunLine } from '@/libs/usage/totals';
import { getErrorMessage } from '@/util/util';
import { useQuery } from '@connectrpc/connect-query';
import { UsageService } from '@husonym/sdk';
import { ReactElement, use, useState } from 'react';
import { useUsageI18n } from '@/libs/usage/useUsageI18n';
import LatestRunsTable from './components/LatestRunsTable';
import { useTranslations } from 'next-intl';

// What a job ran over a period, from the counters of the instance. As on the Usage page
// of the account, the period is cut in the zone of the browser and the line under the
// title says the zone the API counted the days in.
export default function UsagePage(props: PageProps): ReactElement {
  const params = use(props.params);
  const id = params?.id ?? '';
  const t = useTranslations('jobs.usage');
  const usageI18n = useUsageI18n();
  const { account } = useAccount();
  const accountId = account?.id ?? '';
  const [period, setPeriod] = useState<UsagePeriod>(DEFAULT_USAGE_PERIOD);
  const timeZone = browserTimeZone();
  const range = periodRange(period, new Date(), timeZone);
  const { data, error } = useQuery(
    UsageService.method.getJobUsage,
    {
      accountId,
      jobId: id,
      fromDay: range.fromDay,
      toDay: range.toDay,
      timeZone,
    },
    { enabled: !!accountId && !!id, retry: false }
  );

  // The account has no such job: there is no usage to show, not an empty one.
  if (data && isUnknownJob(data.kind)) {
    return <JobNotFoundAlert />;
  }

  return (
    <div className="job-details-usage-container flex flex-col gap-5">
      <ReportingNotice accountId={accountId} />
      <SubPageHeader
        header={t('title')}
        description={t('description')}
        subHeadings={
          error ? (
            rangeLabel(range, undefined, usageI18n)
          ) : data ? (
            rangeLabel(range, data.timeZone, usageI18n)
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
          <AlertTitle>{t('error')}</AlertTitle>
          <AlertDescription>{getErrorMessage(error)}</AlertDescription>
        </Alert>
      ) : data ? (
        <>
          <UsageTiles
            totals={data.totals}
            kind={data.kind}
            duration={{
              label: t('medianDuration'),
              seconds: data.durationMedianSeconds,
            }}
          />
          <RowsPerDayChart
            days={data.days}
            emptyLine={noRowsPerDayLine(data.kind, data.totals, usageI18n)}
          />
          <LatestRunsTable
            accountName={account?.name ?? ''}
            usage={data}
            emptyLine={noRunLine(data.totals, usageI18n)}
          />
        </>
      ) : (
        <UsageSkeleton cards={['h-96', 'h-48']} />
      )}
    </div>
  );
}
