'use client';
import { useAccount } from '@/components/providers/account-provider';
import Link from 'next/link';
import { ReactElement } from 'react';
import { PlusIcon } from './icons';

/** The topbar's primary action: start the job wizard. */
export default function NewJobButton(): ReactElement | null {
  const { account } = useAccount();
  if (!account) {
    return null;
  }
  return (
    <Link
      href={`/${account.name}/new/job`}
      aria-label="New job"
      className="flex h-10 shrink-0 items-center gap-2 rounded-[10px] bg-primary px-3 font-semibold text-primary-foreground transition-[filter] hover:brightness-110 sm:px-4"
    >
      <PlusIcon />
      <span className="hidden sm:inline">New job</span>
    </Link>
  );
}
