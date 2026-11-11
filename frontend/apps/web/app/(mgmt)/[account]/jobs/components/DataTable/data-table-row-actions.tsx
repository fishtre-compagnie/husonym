'use client';

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Job, JobService, JobStatus } from '@husonym/sdk';
import { useRouter } from 'next/navigation';

import DeleteConfirmationDialog from '@/components/DeleteConfirmationDialog';
import { useAccount } from '@/components/providers/account-provider';
import { Button } from '@/components/ui/button';
import { useLicenseFeature } from '@/libs/hooks/useLicense';
import { getErrorMessage } from '@/util/util';
import { useMutation } from '@connectrpc/connect-query';
import { DotsHorizontalIcon } from '@radix-ui/react-icons';
import { nanoid } from 'nanoid';
import { toast } from 'sonner';
import { getJobCloneUrlFromJob } from '../../[id]/components/JobCloneButton';
import { setDefaultNewJobFormValues } from '../../util';

interface DataTableRowActionsProps {
  job: Job;
  status: JobStatus;
  onDeleted(): void;
  onStatusChanged(): void;
}

export function DataTableRowActions({
  job,
  status,
  onDeleted,
  onStatusChanged,
}: DataTableRowActionsProps) {
  const router = useRouter();
  const { account } = useAccount();
  const { mutateAsync: removeJob } = useMutation(JobService.method.deleteJob);
  const { mutateAsync: setJobPaused, isPending: isPausing } = useMutation(
    JobService.method.pauseJob
  );
  const { allowed: canSchedule } = useLicenseFeature('scheduling');
  const isPaused = status === JobStatus.PAUSED;
  // Pausing stays possible without the scheduling feature, resuming does not.
  const isResumeRefused = isPaused && !canSchedule;

  async function onDelete(): Promise<void> {
    try {
      await removeJob({ id: job.id });
      toast.success('Job removed successfully!');
      onDeleted();
    } catch (err) {
      console.error(err);
      toast.error('Unable to remove job', {
        description: getErrorMessage(err),
      });
    }
  }

  async function onTogglePause(): Promise<void> {
    try {
      await setJobPaused({ id: job.id, pause: !isPaused });
      toast.success(`${job.name} ${isPaused ? 'resumed' : 'paused'}`);
      onStatusChanged();
    } catch (err) {
      console.error(err);
      toast.error('Unable to update job paused status', {
        description: getErrorMessage(err),
      });
    }
  }

  function onCloneClick(): void {
    if (!account) {
      return;
    }
    const sessionToken = nanoid();
    setDefaultNewJobFormValues(window.sessionStorage, job, sessionToken);
    router.push(getJobCloneUrlFromJob(account, job, sessionToken));
  }

  return (
    <DropdownMenu
      modal={false} // needed because otherwise this breaks after a single use in conjunction with the delete dialog
    >
      <DropdownMenuTrigger asChild>
        <Button
          type="button"
          variant="outline"
          className="h-[34px] w-[34px] border-border p-0 text-muted-foreground data-[state=open]:bg-hover"
          aria-label={`More actions for ${job.name}`}
        >
          <DotsHorizontalIcon className="h-4 w-4" aria-hidden="true" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuItem
          className="cursor-pointer"
          onClick={() => router.push(`/${account?.name}/jobs/${job.id}`)}
        >
          View
        </DropdownMenuItem>
        <DropdownMenuItem
          className="cursor-pointer"
          onClick={() => onCloneClick()}
        >
          Clone
        </DropdownMenuItem>
        {status !== JobStatus.UNSPECIFIED && (
          <DropdownMenuItem
            className="cursor-pointer"
            disabled={isPausing || isResumeRefused}
            title={
              isResumeRefused
                ? 'Your license does not include scheduling'
                : undefined
            }
            onClick={() => onTogglePause()}
          >
            {isPaused ? 'Resume schedule' : 'Pause schedule'}
          </DropdownMenuItem>
        )}
        <DropdownMenuSeparator />
        <DeleteConfirmationDialog
          trigger={
            <DropdownMenuItem
              className="cursor-pointer text-danger-fg focus:text-danger-fg"
              onSelect={(e) => e.preventDefault()} // needed for the delete modal to not automatically close
            >
              Delete
            </DropdownMenuItem>
          }
          headerText="Are you sure you want to delete this job?"
          description="Deleting this job will also delete all job runs."
          onConfirm={() => onDelete()}
        />
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
