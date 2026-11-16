import { Button } from '@/components/ui/button';
import { getErrorMessage } from '@/util/util';
import { useMutation } from '@connectrpc/connect-query';
import { AccountHook, AccountHookService } from '@husonym/sdk';
import { useTranslations } from 'next-intl';
import { ReactElement } from 'react';
import { toast } from 'sonner';

interface Props {
  onDisabled(): void;
  hook: Pick<AccountHook, 'id'>;
}

export default function DisableHookButton(props: Props): ReactElement {
  const { hook, onDisabled } = props;
  const t = useTranslations('settings.hooks.disable');
  const { mutateAsync: setEnabled, isPending } = useMutation(
    AccountHookService.method.setAccountHookEnabled
  );

  async function onDisable(): Promise<void> {
    try {
      await setEnabled({ id: hook.id, enabled: false });
      toast.success(t('success'));
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
