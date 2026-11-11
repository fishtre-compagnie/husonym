'use client';
import { useAccount } from '@/components/providers/account-provider';
import { useQuery } from '@connectrpc/connect-query';
import { ConnectionService, JobService } from '@husonym/sdk';
import Link from 'next/link';
import { usePathname } from 'next/navigation';
import { Fragment, ReactElement } from 'react';

const SECTIONS: Record<string, string> = {
  jobs: 'Jobs',
  runs: 'Runs',
  connections: 'Connections',
  transformers: 'Transformers',
  settings: 'Settings',
  review: 'PII review',
};

const NEW_THINGS: Record<string, string> = {
  job: 'New job',
  connection: 'New connection',
  transformer: 'New transformer',
  'api-key': 'New API key',
};

interface Crumb {
  label: string;
  href?: string;
  mono?: boolean;
}

function titleCase(segment: string): string {
  const words = segment.replace(/[-_]+/g, ' ').trim();
  return words.charAt(0).toUpperCase() + words.slice(1);
}

/** Where the page is: the account, its section and, on a detail page, the item. */
export default function Breadcrumb(): ReactElement | null {
  const pathname = usePathname() ?? '';
  const { account } = useAccount();
  const accountId = account?.id ?? '';
  const segments = pathname.split('/').filter(Boolean);
  const [accountSegment, section, itemId] = segments;
  const isAccountPath = !!account && accountSegment === account.name;

  // Already in the cache for the sidebar's count and the palette: a name, not a call.
  const { data: jobsData } = useQuery(
    JobService.method.getJobs,
    { accountId },
    { enabled: !!accountId && section === 'jobs' && !!itemId }
  );
  const { data: connectionsData } = useQuery(
    ConnectionService.method.getConnections,
    { accountId },
    { enabled: !!accountId && section === 'connections' && !!itemId }
  );

  if (!isAccountPath) {
    return null;
  }
  const base = `/${account.name}`;
  const crumbs: Crumb[] = [];
  if (!section) {
    crumbs.push({ label: 'Dashboard' });
  } else if (section === 'new') {
    crumbs.push({
      label: NEW_THINGS[itemId ?? ''] ?? titleCase(itemId ?? 'New'),
    });
  } else {
    crumbs.push({
      label: SECTIONS[section] ?? titleCase(section),
      href: itemId ? `${base}/${section}` : undefined,
    });
    if (itemId) {
      if (section === 'jobs') {
        const job = jobsData?.jobs.find((j) => j.id === itemId);
        crumbs.push({ label: job?.name ?? 'Job' });
      } else if (section === 'connections') {
        const c = connectionsData?.connections.find((x) => x.id === itemId);
        crumbs.push({ label: c?.name ?? 'Connection' });
      } else if (section === 'runs') {
        crumbs.push({ label: itemId, mono: true });
      } else if (section === 'transformers') {
        crumbs.push({ label: 'Transformer' });
      } else {
        crumbs.push({ label: titleCase(itemId) });
      }
    }
  }

  return (
    <nav aria-label="Breadcrumb" className="min-w-0">
      <ol className="flex min-w-0 items-center gap-2 text-sm">
        <li className="hidden shrink-0 sm:block">
          <Link
            href={base}
            className="text-muted-foreground transition-colors hover:text-foreground"
          >
            {account.name}
          </Link>
        </li>
        {crumbs.map((c, i) => {
          const last = i === crumbs.length - 1;
          return (
            <Fragment key={i}>
              <li
                aria-hidden="true"
                className={
                  i === 0 ? 'hidden text-border sm:block' : 'text-border'
                }
              >
                /
              </li>
              <li className={last ? 'min-w-0' : 'shrink-0'}>
                {c.href && !last ? (
                  <Link
                    href={c.href}
                    className="text-muted-foreground transition-colors hover:text-foreground"
                  >
                    {c.label}
                  </Link>
                ) : (
                  <span
                    aria-current={last ? 'page' : undefined}
                    className={
                      c.mono
                        ? 'block max-w-[220px] truncate font-mono text-[13px] font-medium text-foreground'
                        : 'block truncate font-medium text-foreground'
                    }
                  >
                    {c.label}
                  </span>
                )}
              </li>
            </Fragment>
          );
        })}
      </ol>
    </nav>
  );
}
