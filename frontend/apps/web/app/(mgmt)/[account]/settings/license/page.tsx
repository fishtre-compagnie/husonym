'use client';

import SubPageHeader from '@/components/headers/SubPageHeader';
import { useAccount } from '@/components/providers/account-provider';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Skeleton } from '@/components/ui/skeleton';
import { useUsageReporting } from '@/libs/hooks/useUsageReporting';
import { getErrorMessage } from '@/util/util';
import { useQuery } from '@connectrpc/connect-query';
import { UserAccountService } from '@husonym/sdk';
import { ReactElement } from 'react';
import LicenseFeaturesCard from './components/LicenseFeaturesCard';
import LicenseKeyCard from './components/LicenseKeyCard';
import LicenseLimitsCard from './components/LicenseLimitsCard';
import LicenseStatusCard from './components/LicenseStatusCard';
import UsageReportCard from './components/UsageReportCard';

export default function Page(): ReactElement {
  const { account } = useAccount();
  const accountId = account?.id ?? '';
  const systemInfo = useQuery(UserAccountService.method.getSystemInformation);
  const usage = useQuery(
    UserAccountService.method.getLicenseUsage,
    { accountId },
    { enabled: !!accountId, retry: false }
  );

  const reporting = useUsageReporting(accountId);

  if (!accountId) {
    return <Skeleton className="w-full h-12" />;
  }

  const license = systemInfo.data?.license;
  // The two cards that set the license against the usage wait for both. A usage that
  // cannot be read does not take the license down with it: each card says what it
  // misses.
  const isUsageLoading = systemInfo.isLoading || usage.isLoading;

  return (
    <div className="flex flex-col gap-5">
      <SubPageHeader
        header="License"
        description="The license of this instance, and what this account uses of it"
      />
      {systemInfo.error ? (
        <Alert variant="destructive">
          <AlertTitle>Unable to read the license of this instance</AlertTitle>
          <AlertDescription>
            {getErrorMessage(systemInfo.error)}
          </AlertDescription>
        </Alert>
      ) : (
        <>
          <LicenseStatusCard
            license={license}
            isLoading={systemInfo.isLoading}
            licenseReportingMode={reporting.data?.licenseMode}
          />
          <LicenseFeaturesCard
            license={license}
            usage={usage.data}
            isLoading={isUsageLoading}
            usageError={usage.error}
          />
          <LicenseLimitsCard
            license={license}
            usage={usage.data}
            isLoading={isUsageLoading}
            usageError={usage.error}
          />
        </>
      )}
      <LicenseKeyCard
        accountId={accountId}
        current={license}
        onInstalled={() =>
          Promise.all([
            systemInfo.refetch(),
            usage.refetch(),
            reporting.refetch(),
          ])
        }
      />
      <UsageReportCard
        accountId={accountId}
        reporting={reporting.data}
        isLoading={reporting.isLoading}
        error={reporting.error}
      />
    </div>
  );
}
