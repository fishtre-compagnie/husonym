import { Button } from '@/components/ui/button';
import { getErrorMessage } from '@/util/util';
import { useMutation } from '@connectrpc/connect-query';
import { JobHook, JobService } from '@husonym/sdk';
import { ReactElement } from 'react';
import { toast } from 'sonner';
import { useTranslations } from 'next-intl';

interface Props {
  onDisabled(): void;
  hook: Pick<JobHook, 'id'>;
}

// Turns a job hook off. It is never greyed: turning a hook off stays possible without
// the job_hooks feature, and is what lets a job start again without losing the SQL of
// its hook, which removing it would.
export default function DisableHookButton(props: Props): ReactElement {
  const { hook, onDisabled } = props;
  const t = useTranslations('jobs.hooks.disable');
  const { mutateAsync: setEnabled, isPending } = useMutation(
    JobService.method.setJobHookEnabled
  );

  async function onDisable(): Promise<void> {
    try {
      await setEnabled({ id: hook.id, enabled: false });
      toast.success(t('disabled'));
      onDisabled();
    } catch (err) {
      console.error(err);
      toast.error(t('error'), {
        description: getErrorMessage(err),
      });
    }
  }

  return (
    <Button
      variant="outline"
      type="button"
      disabled={isPending}
      onClick={() => void onDisable()}
    >
      {t('button')}
    </Button>
  );
}
