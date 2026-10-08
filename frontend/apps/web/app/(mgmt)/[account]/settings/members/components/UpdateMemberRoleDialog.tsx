import { useAccount } from '@/components/providers/account-provider';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog';
import { getErrorMessage } from '@/util/util';
import { UpdateMemberRoleFormValues } from '@/yup-validations/invite-members';
import { useMutation } from '@connectrpc/connect-query';
import { AccountUser, ConnectError, UserAccountService } from '@husonym/sdk';
import { ReactElement, ReactNode, useState } from 'react';
import { toast } from 'sonner';
import UpdateMemberRoleForm from './UpdateMemberRoleForm';

interface Props {
  member: Pick<AccountUser, 'id' | 'name' | 'role' | 'email'>;
  onUpdated(): void;
  dialogButton: ReactNode;
}

export default function UpdateMemberRoleDialog(props: Props): ReactElement {
  const { member, onUpdated, dialogButton } = props;
  const { mutateAsync: updateUserRole } = useMutation(
    UserAccountService.method.setUserRole
  );
  const { account } = useAccount();
  const [open, setOpen] = useState(false);
  const [refusal, setRefusal] = useState<string>();

  async function onUpdate(values: UpdateMemberRoleFormValues): Promise<void> {
    if (!account) {
      return;
    }
    setRefusal(undefined);
    try {
      await updateUserRole({
        userId: member.id,
        role: values.role,
        accountId: account.id,
      });
      toast.success('Successfully updated user role!');
      onUpdated();
      setOpen(false);
    } catch (err) {
      console.error(err);
      // Kept in the dialog rather than in a toast, as a refused license key is: the API
      // says which roles the license of the instance allows, in words to be read as
      // they are.
      setRefusal(
        err instanceof ConnectError ? err.rawMessage : getErrorMessage(err)
      );
    }
  }

  function onOpenChange(value: boolean): void {
    setOpen(value);
    setRefusal(undefined);
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogTrigger asChild>{dialogButton}</DialogTrigger>
      <DialogContent
        onPointerDownOutside={(e) => e.preventDefault()}
        className="max-h-[85vh] overflow-y-auto"
      >
        <DialogHeader>
          <DialogTitle>
            Update role for {member.name} ({member.email})
          </DialogTitle>
          <DialogDescription>Change the role of the user.</DialogDescription>
        </DialogHeader>
        {refusal && (
          <Alert variant="destructive">
            <AlertTitle>The role was not updated</AlertTitle>
            <AlertDescription>{refusal}</AlertDescription>
          </Alert>
        )}
        <UpdateMemberRoleForm
          key={member.id}
          member={member}
          onSubmit={onUpdate}
          onCancel={() => onOpenChange(false)}
        />
      </DialogContent>
    </Dialog>
  );
}
