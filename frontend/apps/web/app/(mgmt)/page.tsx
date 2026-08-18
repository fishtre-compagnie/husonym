'use client';
import { useAccount } from '@/components/providers/account-provider';
import { Skeleton } from '@/components/ui/skeleton';
import { useRouter } from 'next/navigation';
import { ReactElement, useEffect } from 'react';

// Racine "/" : redirige vers la home du compte (/{account}), qui affiche le
// tableau de bord. (Avant : redirigeait directement vers /{account}/jobs.)
export default function Home(): ReactElement {
  const router = useRouter();
  const { account, isLoading } = useAccount();

  useEffect(() => {
    if (isLoading || !account?.name) {
      return;
    }
    router.replace(`/${account.name}`);
  }, [isLoading, account?.name]);

  return <Skeleton className="w-full h-full py-2" />;
}
