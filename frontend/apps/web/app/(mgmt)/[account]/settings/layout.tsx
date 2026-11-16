'use client';
import ResourceId from '@/components/ResourceId';
import { PageBody, PageHeader } from '@/components/ds';
import { useAccount } from '@/components/providers/account-provider';
import { useGetSystemAppConfig } from '@/libs/hooks/useGetSystemAppConfig';
import { cn } from '@/libs/utils';
import { toTitleCase } from '@/util/util';
import { useQuery } from '@connectrpc/connect-query';
import { UserAccountService, UserAccountType } from '@husonym/sdk';
import Link from 'next/link';
import { useTranslations } from 'next-intl';
import { usePathname } from 'next/navigation';
import { ReactElement } from 'react';

type NavKey =
  | 'apiKeys'
  | 'temporal'
  | 'members'
  | 'organization'
  | 'billing'
  | 'usage'
  | 'hooks'
  | 'anonymization'
  | 'license';

interface Item {
  href: string;
  ref: string;
  titleKey: NavKey;
}

export default function SettingsLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const { account } = useAccount();
  const t = useTranslations('settings.layout');
  return (
    <PageBody>
      <div className="flex flex-col gap-1.5">
        <PageHeader
          title={t('title')}
          description={toTitleCase(account?.name ?? '')}
        />
        {/* The id holds a copy button, which a paragraph cannot hold. */}
        <div className="font-mono text-xs">
          <ResourceId
            labelText={account?.id ?? ''}
            copyText={account?.id ?? ''}
            onHoverText={t('copyAccountId')}
          />
        </div>
      </div>
      <div className="flex flex-col gap-6 lg:flex-row lg:gap-10">
        <NavSettings />
        <div className="min-w-0 flex-1">{children}</div>
      </div>
    </PageBody>
  );
}

function NavSettings(): ReactElement {
  const pathname = usePathname();
  const items = useGetNavSettings();
  const t = useTranslations('settings.layout.nav');
  return (
    <nav
      aria-label={t('label')}
      className="-mx-4 flex shrink-0 gap-1 overflow-x-auto px-4 pb-1 lg:mx-0 lg:w-52 lg:flex-col lg:overflow-visible lg:px-0 lg:pb-0"
    >
      {items.map((item) => {
        const isActive = refInPathName(item.ref, pathname);
        return (
          <Link
            key={item.href}
            href={item.href}
            aria-current={isActive ? 'page' : undefined}
            className={cn(
              'whitespace-nowrap rounded-lg px-2.5 py-2 text-sm transition-colors hover:no-underline',
              isActive
                ? 'bg-brand-soft font-medium text-hi'
                : 'text-muted-foreground hover:bg-hover hover:text-foreground'
            )}
          >
            {t(item.titleKey)}
          </Link>
        );
      })}
    </nav>
  );
}

function useGetNavSettings(): Item[] {
  const { account } = useAccount();
  const { data: systemAppConfigData, isLoading: isSystemConfigLoading } =
    useGetSystemAppConfig();
  const { data: systemInfo } = useQuery(
    UserAccountService.method.getSystemInformation
  );

  let items = getAllNavSettings(account?.name ?? '');
  items =
    (!isSystemConfigLoading &&
      systemAppConfigData?.isAuthEnabled &&
      // filter members page if account is not a team
      account?.type === UserAccountType.TEAM) ||
    account?.type === UserAccountType.ENTERPRISE
      ? items
      : items.filter((item) => item.ref !== 'members');
  // filter temporal page if app is in husonym cloud mode
  items =
    !isSystemConfigLoading && systemAppConfigData?.isHusonymCloud
      ? items.filter((item) => item.ref !== 'temporal')
      : items;
  // filter out billing for local
  items =
    !isSystemConfigLoading && systemAppConfigData?.isHusonymCloud
      ? items
      : items.filter((item) => item.ref !== 'billing');
  // filter out hooks if account hooks are not enabled
  items =
    !isSystemConfigLoading && !systemAppConfigData?.isAccountHooksEnabled
      ? items.filter((item) => item.ref !== 'hooks')
      : items;
  // the organization of the instance is designated once, and only where people sign in
  // to an instance of their own
  items =
    !isSystemConfigLoading &&
    systemAppConfigData?.isAuthEnabled &&
    !systemAppConfigData.isHusonymCloud &&
    systemInfo !== undefined &&
    !systemInfo.instanceOrganizationAccountId
      ? items
      : items.filter((item) => item.ref !== 'organization');

  return items;
}

function getAllNavSettings(accountName: string): Item[] {
  return [
    {
      href: `/${accountName}/settings/api-keys`,
      ref: 'api-keys',
      titleKey: 'apiKeys',
    },
    {
      href: `/${accountName}/settings/temporal`,
      ref: 'temporal',
      titleKey: 'temporal',
    },
    {
      href: `/${accountName}/settings/members`,
      ref: 'members',
      titleKey: 'members',
    },
    {
      href: `/${accountName}/settings/organization`,
      ref: 'organization',
      titleKey: 'organization',
    },
    {
      href: `/${accountName}/settings/billing`,
      ref: 'billing',
      titleKey: 'billing',
    },
    {
      href: `/${accountName}/settings/usage`,
      ref: 'usage',
      titleKey: 'usage',
    },
    {
      href: `/${accountName}/settings/hooks`,
      ref: 'hooks',
      titleKey: 'hooks',
    },
    {
      href: `/${accountName}/settings/anonymization`,
      ref: 'anonymization',
      titleKey: 'anonymization',
    },
    {
      href: `/${accountName}/settings/license`,
      ref: 'license',
      titleKey: 'license',
    },
  ];
}

function refInPathName(ref: string, pathname: string): boolean {
  const segments = pathname.split('/');
  return segments.includes(ref);
}
