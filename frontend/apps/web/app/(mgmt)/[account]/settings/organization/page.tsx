'use client';

import SubPageHeader from '@/components/headers/SubPageHeader';
import { useAccount } from '@/components/providers/account-provider';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Skeleton } from '@/components/ui/skeleton';
import { useGetSystemAppConfig } from '@/libs/hooks/useGetSystemAppConfig';
import { getErrorMessage } from '@/util/util';
import { useQuery } from '@connectrpc/connect-query';
import { UserAccountService } from '@husonym/sdk';
import { useTranslations } from 'next-intl';
import { ReactElement } from 'react';
import DesignateOrganizationCard from './components/DesignateOrganizationCard';

export default function Page(): ReactElement {
  const { account } = useAccount();
  const t = useTranslations('settings.organization');
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
      <SubPageHeader header={t('title')} description={t('description')} />
      {systemInfo.error ? (
        <Alert variant="destructive">
          <AlertTitle>{t('readError')}</AlertTitle>
          <AlertDescription>
            {getErrorMessage(systemInfo.error)}
          </AlertDescription>
        </Alert>
      ) : systemAppConfig?.isHusonymCloud ? (
        <Alert>
          <AlertTitle>{t('nothingToDesignate')}</AlertTitle>
          <AlertDescription>{t('selfHostedOnly')}</AlertDescription>
        </Alert>
      ) : organizationId ? (
        // The entry leaves the settings once an organization is retained; the page
        // still answers whoever comes to it by its address.
        <Alert>
          <AlertTitle>{t('hasOrganization')}</AlertTitle>
          <AlertDescription>
            {organizationStatement(
              organizationId,
              account.id,
              accountsData?.accounts.find((a) => a.id === organizationId)?.name,
              t
            )}{' '}
            {t('joinAsViewers')}
          </AlertDescription>
        </Alert>
      ) : !systemAppConfig?.isAuthEnabled ? (
        <Alert>
          <AlertTitle>{t('nothingToDesignate')}</AlertTitle>
          <AlertDescription>{t('noAuth')}</AlertDescription>
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
  organizationName: string | undefined,
  t: ReturnType<typeof useTranslations<'settings.organization'>>
): string {
  if (organizationId === activeAccountId) {
    return t('statementSelf');
  }
  if (organizationName) {
    return t('statementNamed', { name: organizationName });
  }
  return t('statementOther');
}
