'use client';
import { PageBody } from '@/components/ds';
import { useAccount } from '@/components/providers/account-provider';
import { ReactElement } from 'react';
import HomeDashboard from './HomeDashboard';

export default function AccountPage(): ReactElement {
  const { account } = useAccount();
  return (
    <PageBody className="home-page">
      {account?.id && (
        <HomeDashboard accountId={account.id} accountName={account.name} />
      )}
    </PageBody>
  );
}
