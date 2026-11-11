'use client';
import { Kbd } from '@/components/ds';
import { useAccount } from '@/components/providers/account-provider';
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from '@/components/ui/dialog';
import { cn } from '@/libs/utils';
import { useQuery } from '@connectrpc/connect-query';
import { ConnectionService, JobService } from '@husonym/sdk';
import { useRouter } from 'next/navigation';
import { ReactElement, ReactNode, useEffect, useState } from 'react';
import { SearchIcon } from './icons';

// What kind of connection, in a word, for the hint beside its name.
const CONNECTION_KINDS: Record<string, string> = {
  pgConfig: 'PostgreSQL',
  mysqlConfig: 'MySQL',
  mssqlConfig: 'SQL Server',
  mongoConfig: 'MongoDB',
  dynamodbConfig: 'DynamoDB',
  awsS3Config: 'AWS S3',
  gcpCloudstorageConfig: 'Cloud Storage',
  openaiConfig: 'OpenAI',
  localDirConfig: 'Local directory',
};

interface Entry {
  key: string;
  label: string;
  hint?: string;
  glyph: ReactNode;
  href: string;
}

/**
 * The search button of the topbar and the palette it opens (also on ⌘K / Ctrl+K):
 * the actions that create something, the account's jobs and connections, the pages.
 */
export default function CommandPalette(): ReactElement | null {
  const [open, setOpen] = useState(false);
  const router = useRouter();
  const { account } = useAccount();
  const accountId = account?.id ?? '';

  useEffect(() => {
    function onKeyDown(e: KeyboardEvent): void {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        setOpen((o) => !o);
      }
    }
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, []);

  // The jobs are already in the cache (sidebar); the connections are asked when it opens.
  const { data: jobsData } = useQuery(
    JobService.method.getJobs,
    { accountId },
    { enabled: !!accountId }
  );
  const { data: connectionsData } = useQuery(
    ConnectionService.method.getConnections,
    { accountId },
    { enabled: !!accountId && open }
  );

  if (!account) {
    return null;
  }
  const base = `/${account.name}`;

  const actions: Entry[] = [
    { key: 'new-job', label: 'New job', glyph: '+', href: `${base}/new/job` },
    {
      key: 'new-connection',
      label: 'New connection',
      glyph: '+',
      href: `${base}/new/connection`,
    },
    {
      key: 'new-transformer',
      label: 'New transformer',
      glyph: '+',
      href: `${base}/new/transformer`,
    },
  ];
  const jobs: Entry[] = (jobsData?.jobs ?? []).map((j) => ({
    key: `job-${j.id}`,
    label: j.name,
    glyph: 'J',
    href: `${base}/jobs/${j.id}`,
  }));
  const connections: Entry[] = (connectionsData?.connections ?? []).map(
    (c) => ({
      key: `connection-${c.id}`,
      label: c.name,
      hint: CONNECTION_KINDS[c.connectionConfig?.config.case ?? ''],
      glyph: 'C',
      href: `${base}/connections/${c.id}`,
    })
  );
  const pages: Entry[] = [
    { key: 'p-dashboard', label: 'Dashboard', href: base },
    { key: 'p-jobs', label: 'Jobs', href: `${base}/jobs` },
    { key: 'p-runs', label: 'Runs', href: `${base}/runs` },
    { key: 'p-connections', label: 'Connections', href: `${base}/connections` },
    {
      key: 'p-transformers',
      label: 'Transformers',
      href: `${base}/transformers`,
    },
    { key: 'p-settings', label: 'Settings', href: `${base}/settings` },
  ].map((p) => ({ ...p, glyph: '→' }));

  function go(href: string): void {
    setOpen(false);
    router.push(href);
  }

  const group = (heading: string, entries: Entry[]): ReactNode =>
    entries.length > 0 && (
      <CommandGroup heading={heading}>
        {entries.map((e) => (
          <CommandItem
            key={e.key}
            value={e.key}
            keywords={[e.label, ...(e.hint ? [e.hint] : []), heading]}
            onSelect={() => go(e.href)}
            className="cursor-pointer gap-3 rounded-[10px] p-2.5"
          >
            <span
              aria-hidden="true"
              className="grid size-7 shrink-0 place-items-center rounded-lg bg-surface-2 font-mono text-[11px] text-link"
            >
              {e.glyph}
            </span>
            <span className="flex-1 truncate">{e.label}</span>
            {e.hint && (
              <span className="shrink-0 text-xs text-muted-foreground">
                {e.hint}
              </span>
            )}
          </CommandItem>
        ))}
      </CommandGroup>
    );

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        aria-label="Search (Ctrl+K)"
        className={cn(
          'flex h-10 cursor-pointer items-center gap-2.5 rounded-[10px] border border-border bg-card px-3 text-muted-foreground transition-colors hover:bg-hover',
          'w-10 justify-center md:w-auto md:min-w-[300px] md:justify-start'
        )}
      >
        <SearchIcon className="shrink-0" />
        <span className="hidden flex-1 text-left text-sm md:block">
          Search a job, a connection…
        </span>
        <Kbd className="hidden md:inline">⌘K</Kbd>
      </button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent
          className={cn(
            'top-[max(16px,min(120px,12vh))] w-[calc(100%-32px)] max-w-[620px] translate-y-0 gap-0 overflow-hidden rounded-2xl p-0',
            // The palette closes on its Esc key, not on the dialog's cross.
            '[&>button:last-child]:hidden'
          )}
        >
          <DialogTitle className="sr-only">Command palette</DialogTitle>
          <DialogDescription className="sr-only">
            Search the account&apos;s jobs and connections, or start something
            new.
          </DialogDescription>
          <Command className="rounded-none bg-card [&_[cmdk-input-wrapper]]:gap-1 [&_[cmdk-input-wrapper]]:px-[18px] [&_[cmdk-input-wrapper]]:py-2 [&_[cmdk-input]]:h-10 [&_[cmdk-input]]:text-base">
            <div className="relative">
              <CommandInput
                placeholder="Type a command or a name…"
                aria-label="Search"
                className="pr-16"
              />
              <button
                type="button"
                onClick={() => setOpen(false)}
                className="absolute right-[18px] top-1/2 -translate-y-1/2 cursor-pointer"
                aria-label="Close"
              >
                <Kbd>Esc</Kbd>
              </button>
            </div>
            <CommandList className="max-h-[380px] p-2">
              <CommandEmpty className="py-8 text-sm text-muted-foreground">
                Nothing matches.
              </CommandEmpty>
              {group('Actions', actions)}
              {group('Jobs', jobs)}
              {group('Connections', connections)}
              {group('Pages', pages)}
            </CommandList>
            <div className="flex gap-4 border-t border-border px-[18px] py-2.5 text-xs text-muted-foreground">
              <span className="flex items-center gap-1.5">
                <Kbd>↑↓</Kbd> navigate
              </span>
              <span className="flex items-center gap-1.5">
                <Kbd>↵</Kbd> open
              </span>
            </div>
          </Command>
        </DialogContent>
      </Dialog>
    </>
  );
}
