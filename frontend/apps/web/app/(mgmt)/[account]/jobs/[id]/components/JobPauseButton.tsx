'use client';
import ButtonText from '@/components/ButtonText';
import Spinner from '@/components/Spinner';
import { Button } from '@/components/ui/button';
import { useLicenseFeature } from '@/libs/hooks/useLicense';
import { featureLabel, featureNoticeMessage } from '@/libs/license/license';
import { getErrorMessage } from '@/util/util';
import { useMutation } from '@connectrpc/connect-query';
import { JobService, JobStatus } from '@husonym/sdk';
import { PauseIcon, PlayIcon } from '@radix-ui/react-icons';
import { useTranslations } from 'next-intl';
import { ReactElement, useEffect, useState, type JSX } from 'react';
import { toast } from 'sonner';

interface Props {
  jobId: string;
  status?: JobStatus;
  onNewStatus(status: JobStatus): void;
}

export default function JobPauseButton({
  status,
  onNewStatus,
  jobId,
}: Props): ReactElement {
  const t = useTranslations('jobs.job.pause');
  const { allowed, license } = useLicenseFeature('scheduling');
  // Pausing stays possible without the feature, resuming does not.
  const isResumeRefused = !allowed && status === JobStatus.PAUSED;
  const { mutateAsync: setJobPaused } = useMutation(JobService.method.pauseJob);
  const [buttonIcon, setButtonIcon] = useState<JSX.Element>(
    status === JobStatus.PAUSED ? <PlayIcon /> : <PauseIcon />
  );
  const [isTrying, setIsTrying] = useState<boolean>(false);

  useEffect(() => {
    if (isTrying) {
      setButtonIcon(<Spinner />);
    } else {
      setButtonIcon(status === JobStatus.PAUSED ? <PlayIcon /> : <PauseIcon />);
    }
  }, [status, isTrying]);

  async function updateJobStatus(isPaused: boolean): Promise<void> {
    if (isTrying) {
      return;
    }
    try {
      setIsTrying(true);
      await setJobPaused({
        id: jobId,
        pause: isPaused,
      });
      toast.success(isPaused ? t('paused') : t('resumed'));
      onNewStatus(isPaused ? JobStatus.PAUSED : JobStatus.ENABLED);
    } catch (err) {
      console.error(err);
      toast.error(t('error'), {
        description: getErrorMessage(err),
      });
    } finally {
      setIsTrying(false);
    }
  }

  return (
    <div className="max-w-[300px]">
      <Button
        variant="outline"
        disabled={isResumeRefused}
        // A greyed button says why.
        title={
          isResumeRefused
            ? t('resumeRefused', {
                feature: featureLabel('scheduling'),
                notice: featureNoticeMessage(license),
              })
            : undefined
        }
        onClick={async () => {
          const isCurrentlyPaused = status === JobStatus.PAUSED;
          updateJobStatus(!isCurrentlyPaused);
        }}
      >
        <ButtonText
          leftIcon={buttonIcon}
          text={status === JobStatus.PAUSED ? t('resume') : t('pause')}
        />
      </Button>
    </div>
  );
}
