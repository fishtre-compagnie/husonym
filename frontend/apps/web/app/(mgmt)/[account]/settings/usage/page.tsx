'use client';
import SubPageHeader from '@/components/headers/SubPageHeader';
import { useAccount } from '@/components/providers/account-provider';
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
import { errorRows, errorsEmptyLine, refusalRows } from '@/libs/usage/rows';
import { noRunLine } from '@/libs/usage/totals';
import { getErrorMessage } from '@/util/util';
import { useUsageI18n } from '@/libs/usage/useUsageI18n';
import { useTranslations } from 'next-intl';
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
  const i18n = useUsageI18n();
  const { t } = i18n;
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
        header={t('page.title')}
        description={t('page.description')}
        subHeadings={
          error ? (
            rangeLabel(range, undefined, i18n)
          ) : data ? (
            rangeLabel(range, data.timeZone, i18n)
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
          <AlertTitle>{t('page.readError')}</AlertTitle>
          <AlertDescription>{getErrorMessage(error)}</AlertDescription>
        </Alert>
      ) : data ? (
        <AccountUsage accountName={account?.name ?? ''} usage={data} />
      ) : (
        <UsageSkeleton cards={['h-96', 'h-48', 'h-32']} />
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
  const i18n = useUsageI18n();
  const { t } = i18n;
  const tLicense = useTranslations('settings.license');
  const refusals = refusalRows(usage.refusals, i18n.locale, tLicense);
  return (
    <>
      <UsageTiles
        totals={usage.totals}
        duration={{
          label: t('page.runTime'),
          seconds: usage.durationTotalSeconds,
        }}
      />
      <RowsPerDayChart
        days={usage.days}
        emptyLine={noRunLine(usage.totals, i18n)}
      />
      <JobsUsageTable
        accountName={accountName}
        jobs={usage.jobs}
        totals={usage.totals}
      />
      <CountsCard
        title={t('page.errorsTitle')}
        columns={[t('page.category'), t('page.runs')]}
        rows={errorRows(usage.errors, i18n)}
        emptyLine={errorsEmptyLine(usage.totals, i18n)}
      />
      {refusals.length > 0 && (
        <CountsCard
          title={t('page.refusalsTitle')}
          description={t('page.refusalsDescription')}
          columns={[t('page.refused'), t('page.times')]}
          rows={refusals}
        />
      )}
    </>
  );
}
