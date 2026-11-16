import ButtonText from '@/components/ButtonText';
import DeleteConfirmationDialog from '@/components/DeleteConfirmationDialog';
import { useAccount } from '@/components/providers/account-provider';
import { Button } from '@/components/ui/button';
import { getErrorMessage } from '@/util/util';
import { useMutation } from '@connectrpc/connect-query';
import { ConnectionService } from '@husonym/sdk';
import { TrashIcon } from '@radix-ui/react-icons';
import { useRouter } from 'next/navigation';
import { useTranslations } from 'next-intl';
import { ReactElement } from 'react';
import { toast } from 'sonner';

interface Props {
  connectionId: string;
}

export default function RemoveConnectionButton(props: Props): ReactElement {
  const { connectionId } = props;
  const t = useTranslations('connections.delete');
  const router = useRouter();
  const account = useAccount();
  const { mutateAsync } = useMutation(
    ConnectionService.method.deleteConnection
  );

  async function onDelete(): Promise<void> {
    try {
      await mutateAsync({ id: connectionId });
      toast.success(t('removed'));
      router.push(`/${account.account?.name}/connections`);
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
        <Button variant="destructive">
          <ButtonText leftIcon={<TrashIcon />} text={t('confirmButton')} />
        </Button>
      }
      headerText={t('confirmTitle')}
      description={t('confirmDescription')}
      buttonText={t('confirmButton')}
      onConfirm={async () => onDelete()}
    />
  );
}
