import { Pill, StatusDot, Tone } from '@/components/ds';
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip';
import { cn } from '@/libs/utils';
import { JobRunStatus as JobRunStatusEnum } from '@husonym/sdk';
import { ReactElement } from 'react';

interface Props {
  status?: JobRunStatusEnum;
  containerClassName?: string;
  badgeClassName?: string;
}

export interface JobRunStatusMeta {
  label: string;
  tone: Tone;
  // the run is still going: its dot breathes
  live: boolean;
}

/** How a run's status reads: its words, its tone, whether it is still going. */
export function getJobRunStatusMeta(
  status?: JobRunStatusEnum
): JobRunStatusMeta {
  switch (status) {
    case JobRunStatusEnum.COMPLETE:
      return { label: 'Complete', tone: 'ok', live: false };
    case JobRunStatusEnum.RUNNING:
      return { label: 'Running', tone: 'brand', live: true };
    case JobRunStatusEnum.PENDING:
      return { label: 'Pending', tone: 'quiet', live: true };
    case JobRunStatusEnum.ERROR:
      return { label: 'Error', tone: 'danger', live: false };
    case JobRunStatusEnum.FAILED:
      return { label: 'Failed', tone: 'danger', live: false };
    case JobRunStatusEnum.TIMED_OUT:
      return { label: 'Timed out', tone: 'warn', live: false };
    case JobRunStatusEnum.CANCELED:
      return { label: 'Canceled', tone: 'muted', live: false };
    case JobRunStatusEnum.TERMINATED:
      return { label: 'Terminated', tone: 'muted', live: false };
    default:
      return { label: 'Unknown', tone: 'muted', live: false };
  }
}

export default function JobRunStatus(props: Props): ReactElement {
  const { status, containerClassName, badgeClassName } = props;
  const meta = getJobRunStatusMeta(status);

  const pill = (
    <div className={cn('flex', containerClassName)}>
      <Pill tone={meta.tone} className={badgeClassName}>
        <StatusDot tone={meta.tone} pulse={meta.live} />
        {meta.label}
      </Pill>
    </div>
  );

  if (meta.label !== 'Unknown') {
    return pill;
  }
  return (
    <TooltipProvider>
      <Tooltip delayDuration={200}>
        <TooltipTrigger asChild>{pill}</TooltipTrigger>
        <TooltipContent>
          <p>
            This run is un-started, archived by the system, or has been deleted.
          </p>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}
