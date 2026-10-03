import { Button } from '@/components/ui/button';
import { getErrorMessage } from '@/util/util';
import { useMutation } from '@connectrpc/connect-query';
import { AccountHook, AccountHookService } from '@husonym/sdk';
import { ReactElement } from 'react';
import { toast } from 'sonner';

interface Props {
  onDisabled(): void;
  hook: Pick<AccountHook, 'id'>;
}

export default function DisableHookButton(props: Props): ReactElement {
  const { hook, onDisabled } = props;
  const { mutateAsync: setEnabled, isPending } = useMutation(
    AccountHookService.method.setAccountHookEnabled
  );

  async function onDisable(): Promise<void> {
    try {
      await setEnabled({ id: hook.id, enabled: false });
      toast.success('Successfully disabled account hook!');
      onDisabled();
    } catch (err) {
      console.error(err);
      toast.error('Unable to disable account hook', {
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
      Disable
    </Button>
  );
}
