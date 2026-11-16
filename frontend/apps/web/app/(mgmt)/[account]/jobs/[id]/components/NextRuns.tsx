'use client';
import { Alert, AlertTitle } from '@/components/ui/alert';
import { Skeleton } from '@/components/ui/skeleton';
import { Panel, PanelHeader, StatusDot } from '@/components/ds';
import { timestampDate } from '@bufbuild/protobuf/wkt';
import { useQuery } from '@connectrpc/connect-query';
import { JobService, JobStatus } from '@husonym/sdk';
import { useTranslations } from 'next-intl';
import { ReactElement } from 'react';
import { useFormatDateTime } from '@/i18n/date-fns';

interface Props {
  jobId: string;
  status?: JobStatus;
}

export default function JobNextRuns({ jobId, status }: Props): ReactElement {
  const t = useTranslations('jobs.overview.nextRuns');
  const formatDateTime = useFormatDateTime();
  const { data, isLoading, error } = useQuery(
    JobService.method.getJobNextRuns,
    { jobId },
    { enabled: !!jobId }
  );

  if (isLoading) {
    return <Skeleton className="h-full min-h-40 w-full rounded-2xl" />;
  }

  const isPaused = status === JobStatus.PAUSED;
  const nextRuns = data?.nextRuns?.nextRunTimes ?? [];

  return (
    <Panel className="h-full">
      <PanelHeader title={t('title')} />
      {!data?.nextRuns || error ? (
        <div className="p-4">
          <Alert variant="destructive">
            <AlertTitle>{t('error')}</AlertTitle>
          </Alert>
        </div>
      ) : isPaused || nextRuns.length === 0 ? (
        <p className="px-5 py-8 text-center text-sm text-muted-foreground">
          {isPaused ? t('paused') : t('none')}
        </p>
      ) : (
        <ul className="m-0 list-none p-0">
          {nextRuns.slice(0, 4).map((r, index) => (
            <li
              key={`${r}-${index}`}
              className="flex items-center gap-2.5 border-b border-line-soft px-5 py-3 text-sm tabular-nums last:border-b-0"
            >
              <StatusDot tone={index === 0 ? 'brand' : 'quiet'} />
              {formatDateTime(timestampDate(r))}
            </li>
          ))}
        </ul>
      )}
    </Panel>
  );
}
