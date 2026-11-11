'use client';
import { StatusDot } from '@/components/ds';
import { useAccount } from '@/components/providers/account-provider';
import AccountSwitcher from '@/components/site-header/AccountSwitcher';
import HusonymVersion from '@/components/site-header/HusonymVersion';
import { MainLogo } from '@/components/site-header/MainLogo';
import { usePendingReviews } from '@/components/site-header/PendingReviewsBell';
import { UserNav } from '@/components/site-header/UserNav';
import { cn } from '@/libs/utils';
import { useQuery } from '@connectrpc/connect-query';
import { JobRunStatus, JobService } from '@husonym/sdk';
import Link from 'next/link';
import { usePathname } from 'next/navigation';
import { ReactElement, ReactNode } from 'react';
import { AccountCard } from './AccountCard';
import {
  ConnectionsIcon,
  DashboardIcon,
  JobsIcon,
  ReviewIcon,
  RunsIcon,
  SettingsIcon,
  TransformersIcon,
} from './icons';
import { NavKey, accountPath, activeNav } from './nav';
import { navItemClass } from './styles';

interface Props {
  isAuthEnabled: boolean;
  // Called when an entry is followed: the mobile drawer closes on it.
  onNavigate?(): void;
}

function isLive(status: JobRunStatus): boolean {
  return status === JobRunStatus.RUNNING || status === JobRunStatus.PENDING;
}

/** The app's left column: the logo, the account, the sections, the user. */
export default function Sidebar(props: Props): ReactElement {
  const { isAuthEnabled, onNavigate } = props;
  const pathname = usePathname() ?? '';
  const { account, defaultAccountName } = useAccount();
  const accountName = account?.name ?? defaultAccountName;
  const accountId = account?.id ?? '';
  const active = activeNav(pathname, accountName);
  const to = (path: string): string => accountPath(accountName, path);

  // The same queries the jobs list and the dashboard make: they share the cache.
  const { data: jobsData } = useQuery(
    JobService.method.getJobs,
    { accountId },
    { enabled: !!accountId }
  );
  const { data: runsData } = useQuery(
    JobService.method.getJobRuns,
    { id: { case: 'accountId', value: accountId } },
    {
      enabled: !!accountId,
      // While a run is going, look again now and then to see it end.
      refetchInterval: (query) =>
        query.state.data?.jobRuns.some((r) => isLive(r.status))
          ? 30_000
          : false,
    }
  );
  const runsLive = (runsData?.jobRuns ?? []).filter((r) =>
    isLive(r.status)
  ).length;
  const { total: pendingTotal } = usePendingReviews();
  // The page that lists, job by job, the changes waiting; it says so when there are none.
  const reviewHref = to('/review');

  const item = (
    key: NavKey,
    href: string,
    label: string,
    icon: ReactNode,
    trailing?: ReactNode
  ): ReactElement => {
    const isActive = active === key;
    return (
      <Link
        href={href}
        onClick={onNavigate}
        aria-current={isActive ? 'page' : undefined}
        className={cn(
          navItemClass,
          isActive
            ? 'bg-brand-soft font-medium text-hi [&>svg]:text-link'
            : 'text-muted-foreground hover:bg-hover hover:text-foreground'
        )}
      >
        {icon}
        <span className="flex-1 truncate">{label}</span>
        {trailing}
      </Link>
    );
  };

  return (
    <div className="flex h-full flex-col gap-5 px-3.5 py-5">
      <div className="flex items-center justify-between px-1.5">
        <Link
          href="/"
          onClick={onNavigate}
          className="rounded-md text-hi"
          aria-label="Husonym home"
        >
          <MainLogo bg="currentColor" className="h-[22px] w-auto" />
        </Link>
        <HusonymVersion className="font-mono text-[11px] tracking-normal text-muted-foreground" />
      </div>

      {isAuthEnabled ? (
        <AccountSwitcher />
      ) : (
        account && <AccountCard account={account} />
      )}

      <nav aria-label="Main navigation" className="flex flex-col gap-0.5">
        <GroupLabel first>Operate</GroupLabel>
        {item('dashboard', to(''), 'Dashboard', <DashboardIcon />)}
        {item(
          'jobs',
          to('/jobs'),
          'Jobs',
          <JobsIcon />,
          jobsData && (
            <span className="text-xs tabular-nums">{jobsData.jobs.length}</span>
          )
        )}
        {item(
          'runs',
          to('/runs'),
          'Runs',
          <RunsIcon />,
          runsLive > 0 && (
            <StatusDot
              tone="brand"
              pulse
              label={`${runsLive} run${runsLive === 1 ? '' : 's'} in progress`}
            />
          )
        )}
        <GroupLabel>Configure</GroupLabel>
        {item(
          'connections',
          to('/connections'),
          'Connections',
          <ConnectionsIcon />
        )}
        {item(
          'transformers',
          to('/transformers'),
          'Transformers',
          <TransformersIcon />
        )}
        <GroupLabel>Governance</GroupLabel>
        {item(
          'review',
          reviewHref,
          'PII review',
          <ReviewIcon />,
          pendingTotal > 0 && (
            <span
              className="rounded-full bg-danger-soft px-[7px] py-px text-[11px] font-semibold text-danger-fg"
              aria-label={`${pendingTotal} change${pendingTotal === 1 ? '' : 's'} to review`}
            >
              {pendingTotal > 99 ? '99+' : pendingTotal}
            </span>
          )
        )}
      </nav>

      <div className="mt-auto flex flex-col gap-0.5">
        {item('settings', to('/settings'), 'Settings', <SettingsIcon />)}
        <div className="mt-2 border-t border-line-soft pt-2 empty:hidden">
          <UserNav variant="row" />
        </div>
      </div>
    </div>
  );
}

function GroupLabel(props: {
  children: ReactNode;
  first?: boolean;
}): ReactElement {
  return (
    <span
      className={cn(
        'px-2.5 pb-1.5 text-[11px] uppercase tracking-[0.08em] text-muted-foreground',
        props.first ? 'pt-1.5' : 'pt-3.5'
      )}
    >
      {props.children}
    </span>
  );
}
