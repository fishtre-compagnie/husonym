'use client';

import Link from 'next/link';

import { cn } from '@/libs/utils';
import { useTheme } from 'next-themes';
import { usePathname } from 'next/navigation';
import { useAccount } from '../providers/account-provider';
import { MainLogo } from './MainLogo';
import { getPathNameHighlight } from './util';

export function MainNav({
  className,
  ...props
}: React.HTMLAttributes<HTMLElement>) {
  const pathname = usePathname();
  const { account, defaultAccountName } = useAccount();
  const { resolvedTheme } = useTheme();
  const accountName = account?.name ?? defaultAccountName;
  // Until an account is known the links lead to the root, which goes on to the
  // account once there is one.
  const to = (path: string): string =>
    accountName ? `/${accountName}${path}` : '/';

  return (
    <div className="mr-4 hidden lg:flex">
      <Link href="/" className="mr-6 flex items-center space-x-2">
        <MainLogo bg={resolvedTheme === 'dark' ? 'white' : '#272F30'} />
      </Link>
      <nav
        className={cn('flex items-center space-x-4 lg:space-x-6', className)}
        {...props}
      >
        <Link
          href={to('')}
          className={cn(
            'text-sm font-medium text-muted-foreground transition-colors hover:text-black dark:hover:text-white',
            !!accountName &&
              pathname === `/${accountName}` &&
              'text-black dark:text-white'
          )}
        >
          Home
        </Link>
        <Link
          href={to('/jobs')}
          className={cn(
            'text-sm font-medium text-muted-foreground transition-colors hover:text-black dark:hover:text-white',
            getPathNameHighlight('/job', pathname)
          )}
        >
          Jobs
        </Link>
        <Link
          href={to('/runs')}
          className={cn(
            'text-sm font-medium text-muted-foreground transition-colors hover:text-black dark:hover:text-white',
            getPathNameHighlight('/run', pathname)
          )}
        >
          Runs
        </Link>
        <Link
          href={to('/transformers')}
          className={cn(
            'text-sm font-medium text-muted-foreground transition-colors hover:text-black dark:hover:text-white',
            getPathNameHighlight('/transformer', pathname)
          )}
        >
          Transformers
        </Link>
        <Link
          href={to('/connections')}
          className={cn(
            'text-sm font-medium text-muted-foreground transition-colors hover:text-black dark:hover:text-white',
            getPathNameHighlight('connection', pathname)
          )}
        >
          Connections
        </Link>

        <Link
          href={to('/settings')}
          className={cn(
            'text-sm font-medium text-muted-foreground transition-colors hover:text-black dark:hover:text-white',
            getPathNameHighlight('/settings', pathname)
          )}
        >
          Settings
        </Link>
      </nav>
    </div>
  );
}
