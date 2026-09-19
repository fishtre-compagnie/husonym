'use client';
import OverviewContainer from '@/components/containers/OverviewContainer';
import PageHeader from '@/components/headers/PageHeader';
import { useAccount } from '@/components/providers/account-provider';
import { ReactElement } from 'react';
import HomeDashboard from './HomeDashboard';

export default function AccountPage(): ReactElement {
  const { account } = useAccount();
  return (
    <OverviewContainer
      Header={<PageHeader header={`Home - ${account?.name ?? ''}`} />}
      containerClassName="home-page"
    >
      <div className="flex flex-col gap-4">
        {account?.id && <HomeDashboard accountId={account.id} />}
      </div>
    </OverviewContainer>
  );
}
