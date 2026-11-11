'use client';
import { hasPreflight } from '@/components/jobs/preflight/report';
import { usePreflightThenRun } from '@/components/jobs/preflight/usePreflightThenRun';
import { useAccount } from '@/components/providers/account-provider';
import Spinner from '@/components/Spinner';
import { Button } from '@/components/ui/button';
import { getErrorMessage } from '@/util/util';
import { useMutation } from '@connectrpc/connect-query';
import { Job, JobService } from '@husonym/sdk';
import { useRouter } from 'next/navigation';
import { ReactElement, useState } from 'react';
import { toast } from 'sonner';

interface Props {
  job: Job;
  // Called once a run was started, so that the list reads its runs again.
  onStarted(): void;
}

// RunJobButton starts a run of the job, as Trigger Run does on the job's page: the
// pre-flight check goes first when the job has one.
export function RunJobButton(props: Props): ReactElement {
  const { job, onStarted } = props;
  const router = useRouter();
  const { account } = useAccount();
  const { mutateAsync: triggerJobRun } = useMutation(
    JobService.method.createJobRun
  );
  const [isStarting, setIsStarting] = useState(false);
  const { start: startWithPreflight, dialog } = usePreflightThenRun(
    job,
    onTriggerJobRun
  );

  async function onTriggerJobRun(): Promise<void> {
    setIsStarting(true);
    try {
      const { jobRun } = await triggerJobRun({ jobId: job.id });
      // A run that comes back without its start cannot be read yet: runs are found
      // through an index that sees a new one a moment late.
      const visible = !!jobRun?.startedAt;
      toast.success(`Run of ${job.name} started`, {
        action: visible
          ? {
              label: 'View run',
              onClick: () => router.push(`/${account?.name}/runs/${jobRun.id}`),
            }
          : undefined,
      });
      onStarted();
      if (!visible) {
        setTimeout(onStarted, 4000);
      }
    } catch (err) {
      console.error(err);
      toast.error('Unable to trigger job run', {
        description: getErrorMessage(err),
      });
    } finally {
      setIsStarting(false);
    }
  }

  return (
    <>
      <Button
        type="button"
        variant="outline"
        size="sm"
        className="h-[34px] gap-1.5 border-border px-2.5 text-[13px] font-normal"
        disabled={isStarting}
        aria-label={`Run ${job.name} now`}
        onClick={() =>
          hasPreflight(job) ? startWithPreflight() : onTriggerJobRun()
        }
      >
        {isStarting ? (
          <Spinner className="size-3" />
        ) : (
          <svg
            width="12"
            height="12"
            viewBox="0 0 24 24"
            fill="currentColor"
            aria-hidden="true"
          >
            <path d="M7 4v16l13-8z" />
          </svg>
        )}
        Run
      </Button>
      {dialog}
    </>
  );
}
