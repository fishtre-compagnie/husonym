'use client';

import { useAccount } from '@/components/providers/account-provider';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Skeleton } from '@/components/ui/skeleton';
import { useGetSystemAppConfig } from '@/libs/hooks/useGetSystemAppConfig';
import { useTranslations } from 'next-intl';
import { ReactElement } from 'react';
import HooksCard from './components/HooksCard';

export default function Page(): ReactElement {
  const { account } = useAccount();
  const { data: configData, isLoading } = useGetSystemAppConfig();
  if (isLoading || !account?.id) {
    return <Skeleton className="w-full h-12" />;
  }
  if (!configData?.isAccountHooksEnabled) {
    return <HooksDisabledAlert />;
  }
  return (
    <div className="job-hooks-page-container">
      <HooksCard accountId={account.id} />
    </div>
  );
}

function HooksDisabledAlert(): ReactElement {
  const t = useTranslations('settings.hooks');
  return (
    <div>
      <Alert variant="warning">
        <AlertTitle>{t('disabledTitle')}</AlertTitle>
        <AlertDescription>{t('disabledDescription')}</AlertDescription>
      </Alert>
    </div>
  );
}
