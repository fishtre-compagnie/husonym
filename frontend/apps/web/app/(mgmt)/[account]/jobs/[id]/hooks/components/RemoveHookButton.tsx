import DeleteConfirmationDialog from '@/components/DeleteConfirmationDialog';
import { Button } from '@/components/ui/button';
import { getErrorMessage } from '@/util/util';
import { useMutation } from '@connectrpc/connect-query';
import { JobHook, JobService } from '@husonym/sdk';
import { TrashIcon } from '@radix-ui/react-icons';
import { ReactElement } from 'react';
import { toast } from 'sonner';
import { useTranslations } from 'next-intl';

interface Props {
  onDeleted(): void;
  hook: Pick<JobHook, 'id' | 'name'>;
}
export default function RemoveHookButton(props: Props): ReactElement {
  const { hook, onDeleted } = props;
  const t = useTranslations('jobs.hooks.remove');
  const { mutateAsync: removeHook } = useMutation(
    JobService.method.deleteJobHook
  );

  async function onDelete(): Promise<void> {
    try {
      await removeHook({ id: hook.id });
      toast.success(t('removed'));
      onDeleted();
    } catch (err) {
      console.error(err);
      toast.error(t('error'), {
        description: getErrorMessage(err),
      });
    }
  }

  return (
    <DeleteConfirmationDialog
      trigger={
        <Button variant="destructive" type="button">
          <TrashIcon />
        </Button>
      }
      headerText={t('title', { name: hook.name })}
      description={t('description')}
      onConfirm={async () => onDelete()}
    />
  );
}
