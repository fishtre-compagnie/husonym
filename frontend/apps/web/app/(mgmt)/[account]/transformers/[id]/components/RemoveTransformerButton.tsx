import ButtonText from '@/components/ButtonText';
import DeleteConfirmationDialog from '@/components/DeleteConfirmationDialog';
import { useAccount } from '@/components/providers/account-provider';
import { Button } from '@/components/ui/button';
import { getErrorMessage } from '@/util/util';
import { useMutation } from '@connectrpc/connect-query';
import { TransformersService } from '@husonym/sdk';
import { TrashIcon } from '@radix-ui/react-icons';
import { useRouter } from 'next/navigation';
import { ReactElement } from 'react';
import { useTranslations } from 'next-intl';
import { toast } from 'sonner';

interface Props {
  transformerID: string;
}

export default function RemoveTransformerButton(props: Props): ReactElement {
  const { transformerID } = props;
  const t = useTranslations('connections.transformers.delete');
  const router = useRouter();
  const { account } = useAccount();
  const { mutateAsync } = useMutation(
    TransformersService.method.deleteUserDefinedTransformer
  );

  async function deleteTransformer(): Promise<void> {
    try {
      await mutateAsync({ transformerId: transformerID });
      toast.success(t('removed'));
      router.push(`/${account?.name}/transformers`);
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
          <ButtonText leftIcon={<TrashIcon />} text={t('deleteButton')} />
        </Button>
      }
      headerText={t('confirmTitle')}
      description={t('confirmDescriptionDetail')}
      buttonText={t('confirmButton')}
      onConfirm={async () => deleteTransformer()}
    />
  );
}
