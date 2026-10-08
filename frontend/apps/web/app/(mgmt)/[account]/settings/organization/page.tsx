'use client';

import SubPageHeader from '@/components/headers/SubPageHeader';
import { useAccount } from '@/components/providers/account-provider';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Skeleton } from '@/components/ui/skeleton';
import { useGetSystemAppConfig } from '@/libs/hooks/useGetSystemAppConfig';
import { getErrorMessage } from '@/util/util';
import { useQuery } from '@connectrpc/connect-query';
import { UserAccountService } from '@husonym/sdk';
import { ReactElement } from 'react';
import DesignateOrganizationCard from './components/DesignateOrganizationCard';

export default function Page(): ReactElement {
  const { account } = useAccount();
  const { data: systemAppConfig, isLoading: isSystemAppConfigLoading } =
    useGetSystemAppConfig();
  const systemInfo = useQuery(UserAccountService.method.getSystemInformation);
  const { data: accountsData } = useQuery(
    UserAccountService.method.getUserAccounts
  );

  if (!account?.id || systemInfo.isLoading || isSystemAppConfigLoading) {
    return <Skeleton className="w-full h-12" />;
  }

  const organizationId = systemInfo.data?.instanceOrganizationAccountId;

  return (
    <div className="flex flex-col gap-5">
      <SubPageHeader
        header="Organization"
        description="The account people who sign in to this instance work in"
      />
      {systemInfo.error ? (
        <Alert variant="destructive">
          <AlertTitle>
            Unable to read the organization of this instance
          </AlertTitle>
          <AlertDescription>
            {getErrorMessage(systemInfo.error)}
          </AlertDescription>
        </Alert>
      ) : organizationId ? (
        // The entry leaves the settings once an organization is retained; the page
        // still answers whoever comes to it by its address.
        <Alert>
          <AlertTitle>This instance has its organization</AlertTitle>
          <AlertDescription>
            {organizationStatement(
              organizationId,
              account.id,
              accountsData?.accounts.find((a) => a.id === organizationId)?.name
            )}{' '}
            People who sign in to this instance join it as viewers. This cannot
            be changed from the interface.
          </AlertDescription>
        </Alert>
      ) : !systemAppConfig?.isAuthEnabled ? (
        <Alert>
          <AlertTitle>Nothing to designate</AlertTitle>
          <AlertDescription>
            This instance runs without authentication: it has a single user, and
            no organization for others to join.
          </AlertDescription>
        </Alert>
      ) : (
        <DesignateOrganizationCard account={account} />
      )}
    </div>
  );
}

function organizationStatement(
  organizationId: string,
  activeAccountId: string,
  // Known only when the user is in the organization.
  organizationName: string | undefined
): string {
  if (organizationId === activeAccountId) {
    return 'This account is the organization of this instance.';
  }
  if (organizationName) {
    return `The account "${organizationName}" is the organization of this instance.`;
  }
  return 'Another account is the organization of this instance.';
}
