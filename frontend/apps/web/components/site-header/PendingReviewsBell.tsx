'use client';

import { useAccount } from '@/components/providers/account-provider';
import { Button } from '@/components/ui/button';
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover';
import { isPassthrough } from '@/util/mapping-changes';
import { useQuery } from '@connectrpc/connect-query';
import { JobMappingChangeKind, JobService } from '@husonym/sdk';
import { BellIcon, CheckCircledIcon } from '@radix-ui/react-icons';
import Link from 'next/link';
import { ReactElement, useMemo, useState } from 'react';

interface JobPending {
  jobId: string;
  jobName: string;
  count: number;
  // Columns that read as personal data and that a run left in clear
  personalInClear: number;
}

// Tells whoever logs in whether something is waiting for them.
//
// A count of things to do, not a feed of things that happened: nothing to mark as read, and it
// drops to zero on its own when the last change is reviewed. A notification marked "read" while
// the column still ships in clear would be a lie; a count cannot be one, since it is only zero
// when there is nothing left.
//
// Failed runs are deliberately not here. They are events, and belong to notifications; this is
// the list of decisions somebody has to make.
export default function PendingReviewsBell(): ReactElement | null {
  const { account } = useAccount();
  const accountId = account?.id ?? '';
  const [open, setOpen] = useState(false);

  const { data: pendingData } = useQuery(
    JobService.method.getPendingMappingChanges,
    { accountId },
    { enabled: !!accountId }
  );
  const { data: jobsData } = useQuery(
    JobService.method.getJobs,
    { accountId },
    { enabled: !!accountId }
  );

  const byJob = useMemo((): JobPending[] => {
    const names = new Map(
      (jobsData?.jobs ?? []).map((job) => [job.id, job.name])
    );
    const grouped = new Map<string, JobPending>();
    for (const change of pendingData?.changes ?? []) {
      const entry = grouped.get(change.jobId) ?? {
        jobId: change.jobId,
        jobName: names.get(change.jobId) ?? change.jobId,
        count: 0,
        personalInClear: 0,
      };
      entry.count++;
      if (
        change.piiCategory &&
        change.kind === JobMappingChangeKind.ADDED &&
        isPassthrough(change)
      ) {
        entry.personalInClear++;
      }
      grouped.set(change.jobId, entry);
    }
    // The job shipping the most personal data in clear first: that is the one to open.
    return [...grouped.values()].sort(
      (a, b) => b.personalInClear - a.personalInClear || b.count - a.count
    );
  }, [pendingData?.changes, jobsData?.jobs]);

  if (!account) {
    return null;
  }

  const total = byJob.reduce((sum, job) => sum + job.count, 0);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="ghost"
          className="relative"
          aria-label={
            total > 0
              ? `${total} change${total === 1 ? '' : 's'} to review`
              : 'Nothing to review'
          }
        >
          <BellIcon className="w-4 h-4" />
          {total > 0 && (
            <span className="absolute top-1 right-1 min-w-4 h-4 px-1 rounded-full bg-red-600 text-white text-[10px] leading-4 text-center">
              {total > 99 ? '99+' : total}
            </span>
          )}
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-80">
        {total === 0 ? (
          <div className="flex flex-row items-center gap-2 text-sm">
            <CheckCircledIcon />
            Nothing to review: every change your jobs&apos; runs made has been
            reviewed.
          </div>
        ) : (
          <div className="flex flex-col gap-3">
            <p className="text-sm font-medium">
              {total} change{total === 1 ? '' : 's'} to your jobs&apos;
              mappings, waiting for review
            </p>
            <ul className="flex flex-col gap-1">
              {byJob.map((job) => (
                <li key={job.jobId}>
                  <Link
                    href={`/${account.name}/jobs/${job.jobId}/review`}
                    onClick={() => setOpen(false)}
                    className="flex flex-row items-center justify-between gap-2 rounded-md px-2 py-1 text-sm hover:bg-muted"
                  >
                    <span className="truncate">{job.jobName}</span>
                    <span className="shrink-0 text-xs text-muted-foreground">
                      {job.count}
                      {job.personalInClear > 0 && (
                        <span className="text-red-600 dark:text-red-400">
                          {' '}
                          · {job.personalInClear} personal in clear
                        </span>
                      )}
                    </span>
                  </Link>
                </li>
              ))}
            </ul>
          </div>
        )}
      </PopoverContent>
    </Popover>
  );
}
