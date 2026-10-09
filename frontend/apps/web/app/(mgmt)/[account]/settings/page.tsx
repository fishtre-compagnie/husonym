'use client';
import OverviewContainer from '@/components/containers/OverviewContainer';
import PageHeader from '@/components/headers/PageHeader';
import { useAccount } from '@/components/providers/account-provider';
import { useRouter } from 'next/navigation';
import { ReactElement, useEffect } from 'react';

export default function Settings(): ReactElement {
  const {
    account,
    isLoading: isAccountLoading,
    defaultAccountName,
  } = useAccount();
  const router = useRouter();
  const accountName = account?.name ?? defaultAccountName;
  useEffect(() => {
    // Without an account there is nowhere to go yet.
    if (isAccountLoading || !accountName) {
      return;
    }
    return router.push(`/${accountName}/settings/api-keys`);
  }, [accountName, isAccountLoading]);

  return (
    <OverviewContainer
      Header={<PageHeader header="Settings" />}
      containerClassName="settings-page"
    >
      <div />
    </OverviewContainer>
  );
}
