'use client';
import {
  PageBody,
  PageHeader,
  Panel,
  PanelHeader,
  Pill,
  rowClass,
} from '@/components/ds';
import { useAccount } from '@/components/providers/account-provider';
import { usePendingReviews } from '@/components/site-header/PendingReviewsBell';
import { Skeleton } from '@/components/ui/skeleton';
import { cn } from '@/libs/utils';
import { CheckCircledIcon, ChevronRightIcon } from '@radix-ui/react-icons';
import { useTranslations } from 'next-intl';
import Link from 'next/link';
import { ReactElement } from 'react';

// The review of the account: every job whose runs changed its mapping, with what waits
// for a decision. The decisions themselves are made on the job's Review tab.
export default function Page(): ReactElement {
  const t = useTranslations('dashboard.review');
  const { account } = useAccount();
  const { byJob, total, isLoading } = usePendingReviews();
  const inClear = byJob.reduce((n, j) => n + j.personalInClear, 0);

  return (
    <PageBody>
      <PageHeader title={t('title')} description={t('description')} />

      {isLoading ? (
        <Skeleton className="h-40 w-full rounded-2xl" />
      ) : total === 0 ? (
        <Panel className="flex flex-col items-center gap-3 px-6 py-12 text-center">
          <span className="grid size-11 place-items-center rounded-full bg-ok-soft text-ok">
            <CheckCircledIcon className="size-5" />
          </span>
          <h2 className="m-0 font-display text-lg font-semibold">
            {t('nothing')}
          </h2>
          <p className="m-0 max-w-md text-muted-foreground">
            {t('nothingText')}
          </p>
          <Link
            href={`/${account?.name}/jobs`}
            className="text-sm text-link hover:underline"
          >
            {t('goToJobs')}
          </Link>
        </Panel>
      ) : (
        <Panel>
          <PanelHeader
            title={t('total', { count: total })}
            actions={
              inClear > 0 ? (
                <Pill tone="danger">
                  {t('personalInClear', { count: inClear })}
                </Pill>
              ) : undefined
            }
          />
          <ul className="m-0 list-none p-0">
            {byJob.map((job) => (
              <li key={job.jobId} className={rowClass}>
                <Link
                  href={`/${account?.name}/jobs/${job.jobId}/review`}
                  className="flex flex-wrap items-center gap-x-4 gap-y-1 px-5 py-3.5"
                >
                  <span className="min-w-0 flex-1 truncate font-medium text-hi">
                    {job.jobName}
                  </span>
                  {job.personalInClear > 0 && (
                    <Pill tone="danger">
                      {t('jobInClear', { count: job.personalInClear })}
                    </Pill>
                  )}
                  <span
                    className={cn(
                      'text-sm tabular-nums',
                      job.personalInClear > 0
                        ? 'text-danger-fg'
                        : 'text-muted-foreground'
                    )}
                  >
                    {t('jobChanges', { count: job.count })}
                  </span>
                  <ChevronRightIcon
                    aria-hidden="true"
                    className="size-4 text-muted-foreground"
                  />
                </Link>
              </li>
            ))}
          </ul>
        </Panel>
      )}
    </PageBody>
  );
}
