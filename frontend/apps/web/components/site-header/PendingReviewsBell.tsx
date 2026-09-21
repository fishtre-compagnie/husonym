'use client';

import { useAccount } from '@/components/providers/account-provider';
import { Button } from '@/components/ui/button';
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover';
import { useQuery } from '@connectrpc/connect-query';
import { JobService } from '@husonym/sdk';
import { BellIcon, CheckCircledIcon } from '@radix-ui/react-icons';
import Link from 'next/link';
import { ReactElement, useMemo, useState } from 'react';

interface JobPending {
  jobId: string;
  jobName: string;
  count: number;
  personalData: number;
}

// Tells whoever logs in whether something is waiting for them.
//
// A count of things to do, not a feed of things that happened: nothing to mark as read, and it
// drops to zero on its own when the last column is settled. A notification marked "read" while
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
    JobService.method.getPendingColumnReviews,
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
    for (const column of pendingData?.columns ?? []) {
      const entry = grouped.get(column.jobId) ?? {
        jobId: column.jobId,
        jobName: names.get(column.jobId) ?? column.jobId,
        count: 0,
        personalData: 0,
      };
      entry.count++;
      if (column.piiCategory) {
        entry.personalData++;
      }
      grouped.set(column.jobId, entry);
    }
    // The job leaking the most personal data first: that is the one to open.
    return [...grouped.values()].sort(
      (a, b) => b.personalData - a.personalData || b.count - a.count
    );
  }, [pendingData?.columns, jobsData?.jobs]);

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
              ? `${total} column${total === 1 ? '' : 's'} to review`
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
            Nothing to review: every column your jobs copy is mapped or has been
            reviewed.
          </div>
        ) : (
          <div className="flex flex-col gap-3">
            <p className="text-sm font-medium">
              {total} column{total === 1 ? '' : 's'} copied untransformed,
              waiting for a decision
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
                      {job.personalData > 0 && (
                        <span className="text-red-600 dark:text-red-400">
                          {' '}
                          · {job.personalData} personal
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
