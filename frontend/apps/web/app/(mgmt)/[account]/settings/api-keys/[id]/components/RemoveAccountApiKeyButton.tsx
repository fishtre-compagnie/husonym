'use client';
import ButtonText from '@/components/ButtonText';
import DeleteConfirmationDialog from '@/components/DeleteConfirmationDialog';
import { Button } from '@/components/ui/button';
import { getErrorMessage } from '@/util/util';
import { useMutation } from '@connectrpc/connect-query';
import { ApiKeyService } from '@husonym/sdk';
import { TrashIcon } from '@radix-ui/react-icons';
import { useTranslations } from 'next-intl';
import { ReactElement, ReactNode } from 'react';
import { toast } from 'sonner';

interface Props {
  id: string;
  trigger?: ReactNode;
  onDeleted?(): void;
}

export default function RemoveAccountApiKeyButton(props: Props): ReactElement {
  const { id, trigger, onDeleted } = props;
  const t = useTranslations('settings.apiKeys.remove');
  const { mutateAsync } = useMutation(ApiKeyService.method.deleteAccountApiKey);

  async function onDelete(): Promise<void> {
    try {
      await mutateAsync({ id: id });
      toast.success(t('success'));
      if (onDeleted) {
        onDeleted();
      }
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
        trigger ? (
          trigger
        ) : (
          <Button variant="destructive">
            <ButtonText leftIcon={<TrashIcon />} text={t('button')} />
          </Button>
        )
      }
      headerText={t('header')}
      description={t('description')}
      onConfirm={async () => onDelete()}
    />
  );
}
