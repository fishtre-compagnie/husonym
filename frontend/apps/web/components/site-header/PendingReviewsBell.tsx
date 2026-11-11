'use client';

import { useAccount } from '@/components/providers/account-provider';
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover';
import { isPassthrough } from '@/util/mapping-changes';
import { useQuery } from '@connectrpc/connect-query';
import { JobMappingChangeKind, JobService } from '@husonym/sdk';
import { CheckCircledIcon } from '@radix-ui/react-icons';
import { cn } from '@/libs/utils';
import { BellIcon } from '../shell/icons';
import { topbarIconButtonClass } from '../shell/styles';
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
export interface PendingReviews {
  // Each job with changes waiting, the one shipping the most personal data in clear first
  byJob: JobPending[];
  total: number;
  // True until the first answer: an empty list then says nothing yet.
  isLoading: boolean;
}

// The changes waiting for review across the account's jobs. The sidebar and the bell
// share it, and the queries behind it, so both always say the same number.
export function usePendingReviews(): PendingReviews {
  const { account } = useAccount();
  const accountId = account?.id ?? '';

  const { data: pendingData, isLoading } = useQuery(
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

  return {
    byJob,
    total: byJob.reduce((sum, job) => sum + job.count, 0),
    isLoading: isLoading || !accountId,
  };
}

export default function PendingReviewsBell(): ReactElement | null {
  const { account } = useAccount();
  const [open, setOpen] = useState(false);
  const { byJob, total } = usePendingReviews();

  if (!account) {
    return null;
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          type="button"
          className={cn(topbarIconButtonClass, 'relative')}
          aria-label={
            total > 0
              ? `${total} change${total === 1 ? '' : 's'} to review`
              : 'Nothing to review'
          }
        >
          <BellIcon size={18} />
          {total > 0 && (
            <span className="absolute -top-1.5 -right-1.5 h-[18px] min-w-[18px] rounded-full bg-danger px-1 text-center text-[10px] font-semibold leading-[18px] text-background">
              {total > 99 ? '99+' : total}
            </span>
          )}
        </button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-80">
        {total === 0 ? (
          <div className="flex flex-row items-center gap-2 text-sm">
            <CheckCircledIcon className="shrink-0 text-ok" />
            Nothing to review: every change your jobs&apos; runs made has been
            reviewed.
          </div>
        ) : (
          <div className="flex flex-col gap-3">
            <p className="font-display text-sm font-semibold text-hi">
              {total} change{total === 1 ? '' : 's'} to your jobs&apos;
              mappings, waiting for review
            </p>
            <ul className="flex flex-col gap-1">
              {byJob.map((job) => (
                <li key={job.jobId}>
                  <Link
                    href={`/${account.name}/jobs/${job.jobId}/review`}
                    onClick={() => setOpen(false)}
                    className="flex flex-row items-center justify-between gap-2 rounded-lg px-2 py-1.5 text-sm transition-colors hover:bg-hover"
                  >
                    <span className="truncate">{job.jobName}</span>
                    <span className="shrink-0 text-xs text-muted-foreground">
                      {job.count}
                      {job.personalInClear > 0 && (
                        <span className="text-danger-fg">
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
