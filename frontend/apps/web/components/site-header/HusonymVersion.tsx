'use client';
import { useQuery } from '@connectrpc/connect-query';
import { UserAccountService } from '@husonym/sdk';
import { cn } from '@/libs/utils';
import { ReactElement } from 'react';

export default function HusonymVersion(props: {
  className?: string;
}): ReactElement | null {
  const { data } = useQuery(UserAccountService.method.getSystemInformation);
  if (!data?.version) {
    return null;
  }
  return (
    <p className={cn('text-sm tracking-tight', props.className)}>
      {data.version}
    </p>
  );
}
