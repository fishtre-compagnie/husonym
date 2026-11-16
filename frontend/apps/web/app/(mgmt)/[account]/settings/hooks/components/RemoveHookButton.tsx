import DeleteConfirmationDialog from '@/components/DeleteConfirmationDialog';
import { Button } from '@/components/ui/button';
import { getErrorMessage } from '@/util/util';
import { useMutation } from '@connectrpc/connect-query';
import { AccountHook, AccountHookService } from '@husonym/sdk';
import { TrashIcon } from '@radix-ui/react-icons';
import { useTranslations } from 'next-intl';
import { ReactElement } from 'react';
import { toast } from 'sonner';

interface Props {
  onDeleted(): void;
  hook: Pick<AccountHook, 'id' | 'name'>;
}
export default function RemoveHookButton(props: Props): ReactElement {
  const { hook, onDeleted } = props;
  const t = useTranslations('settings.hooks.remove');
  const { mutateAsync: removeHook } = useMutation(
    AccountHookService.method.deleteAccountHook
  );

  async function onDelete(): Promise<void> {
    try {
      await removeHook({ id: hook.id });
      toast.success(t('success'));
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
      headerText={t('header', { name: hook.name })}
      description={t('description')}
      onConfirm={async () => onDelete()}
    />
  );
}
