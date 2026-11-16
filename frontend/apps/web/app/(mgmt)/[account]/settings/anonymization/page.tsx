'use client';

import SubPageHeader from '@/components/headers/SubPageHeader';
import { useAccount } from '@/components/providers/account-provider';
import { Skeleton } from '@/components/ui/skeleton';
import { useTranslations } from 'next-intl';
import { ReactElement } from 'react';
import ConsistencyKeyCard from './components/ConsistencyKeyCard';

export default function Page(): ReactElement {
  const { account } = useAccount();
  const t = useTranslations('settings.anonymization');
  if (!account?.id) {
    return <Skeleton className="w-full h-12" />;
  }
  return (
    <div className="flex flex-col gap-5">
      <SubPageHeader header={t('title')} description={t('description')} />
      <ConsistencyKeyCard accountId={account.id} />
    </div>
  );
}
