import { TrashIcon } from '@radix-ui/react-icons';
import { ReactElement } from 'react';
import ConfirmationDialog, {
  Props as ConfirmationDialogProps,
} from './ConfirmationDialog';
import { useTranslations } from 'next-intl';

type Props = ConfirmationDialogProps;

export default function DeleteConfirmationDialog(props: Props): ReactElement {
  const t = useTranslations('common');
  return (
    <ConfirmationDialog
      buttonText={t('delete')}
      buttonIcon={<TrashIcon />}
      buttonVariant="destructive"
      {...props}
    />
  );
}
