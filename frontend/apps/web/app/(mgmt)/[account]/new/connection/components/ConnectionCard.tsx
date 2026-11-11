'use client';
import ConnectionIcon from '@/components/connections/ConnectionIcon';
import { Pill } from '@/components/ds';
import { useAccount } from '@/components/providers/account-provider';
import { cn } from '@/libs/utils';
import { useQuery } from '@connectrpc/connect-query';
import { UserAccountService } from '@husonym/sdk';
import NextLink from 'next/link';
import { useSearchParams } from 'next/navigation';
import { ReactElement, ReactNode } from 'react';
import { ConnectionMeta } from '../../../connections/util';

interface Props {
  connection: ConnectionMeta;
}

export default function ConnectionCard(props: Props): ReactElement {
  const { connection } = props;
  const { account } = useAccount();
  const searchParams = useSearchParams();
  const { data: systemInfo } = useQuery(
    UserAccountService.method.getSystemInformation
  );
  const hasValidLicense = systemInfo?.license?.isValid ?? false;
  const isClickable =
    !connection.isLicenseOnly || (connection.isLicenseOnly && hasValidLicense);

  const content: ReactNode = (
    <>
      <span className="grid size-10 shrink-0 place-items-center rounded-[10px] bg-surface-2 text-link [&_svg]:size-6!">
        <ConnectionIcon
          connectionType={connection.connectionType}
          connectionTypeVariant={connection.connectionTypeVariant}
        />
      </span>
      <span className="flex min-w-0 flex-1 flex-col gap-1">
        <span className="flex flex-wrap items-center gap-2">
          <span className="text-[15px] font-semibold">{connection.name}</span>
          {connection.isExperimental ? (
            <Pill tone="warn">Experimental</Pill>
          ) : null}
          {connection.isLicenseOnly ? (
            <Pill tone="brand">Enterprise</Pill>
          ) : null}
        </span>
        <span className="text-[13px] leading-snug text-muted-foreground">
          {connection.description}
        </span>
      </span>
    </>
  );

  const className =
    'flex h-full items-start gap-3.5 rounded-[14px] border border-border bg-card p-4 text-left text-foreground transition-colors';

  if (!isClickable) {
    return (
      <div
        aria-disabled="true"
        title="Requires an Enterprise license"
        className={cn(className, 'cursor-not-allowed opacity-50')}
      >
        {content}
      </div>
    );
  }

  return (
    <NextLink
      href={`/${account?.name}/new/connection/${
        connection.urlSlug
      }?${searchParams.toString()}`}
      className={cn(
        className,
        'hover:border-primary hover:bg-hover hover:text-foreground hover:no-underline'
      )}
    >
      {content}
    </NextLink>
  );
}
