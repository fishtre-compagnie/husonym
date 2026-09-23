'use client';

import SubPageHeader from '@/components/headers/SubPageHeader';
import { useAccount } from '@/components/providers/account-provider';
import { Skeleton } from '@/components/ui/skeleton';
import { ReactElement } from 'react';
import ConsistencyKeyCard from './components/ConsistencyKeyCard';

export default function Page(): ReactElement {
  const { account } = useAccount();
  if (!account?.id) {
    return <Skeleton className="w-full h-12" />;
  }
  return (
    <div className="flex flex-col gap-5">
      <SubPageHeader
        header="Anonymization"
        description="What this account's anonymization derives from"
      />
      <ConsistencyKeyCard accountId={account.id} />
    </div>
  );
}
