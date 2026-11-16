'use client';

import ButtonText from '@/components/ButtonText';
import { CopyButton } from '@/components/CopyButton';
import LicenseFeatureNotice from '@/components/LicenseFeatureNotice';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog';
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form';
import { Input } from '@/components/ui/input';
import { useGetSystemAppConfig } from '@/libs/hooks/useGetSystemAppConfig';
import { useLicenseFeature } from '@/libs/hooks/useLicense';
import { useRoleControls } from '@/libs/hooks/useRoleControls';
import { invitationRole } from '@/libs/license/license';
import { getErrorMessage } from '@/util/util';
import { InviteMembersForm } from '@/yup-validations/invite-members';
import { useMutation } from '@connectrpc/connect-query';
import { yupResolver } from '@/util/yup-form-resolver';
import { AccountRole, UserAccountService } from '@husonym/sdk';
import { DialogClose } from '@radix-ui/react-dialog';
import { PlusIcon } from '@radix-ui/react-icons';
import { useTranslations } from 'next-intl';
import { ReactElement, useState } from 'react';
import { useForm } from 'react-hook-form';
import { toast } from 'sonner';
import SelectAccountRole from './SelectAccountRole';

interface Props {
  accountId: string;
  onInvited(): void;
}
export default function InviteUserForm(props: Props): ReactElement {
  const { accountId, onInvited } = props;
  const showRoles = useRoleControls();
  const { allowed: isRbacAllowed } = useLicenseFeature('rbac');
  const [showNewInviteDialog, setShowNewinviteDialog] = useState(false);
  const [newInviteToken, setNewInviteToken] = useState('');
  const [openInviteCreated, setOpenInviteCreated] = useState(false);
  const t = useTranslations('settings.members.invite');

  const form = useForm<InviteMembersForm>({
    resolver: yupResolver(InviteMembersForm),
    defaultValues: {
      email: '',
      role: AccountRole.JOB_VIEWER,
    },
  });
  const { mutateAsync } = useMutation(
    UserAccountService.method.inviteUserToTeamAccount
  );

  async function onSubmit(values: InviteMembersForm): Promise<void> {
    try {
      const invite = await mutateAsync({
        accountId: accountId,
        email: values.email,
        // A role the license does not allow is not named: the member is given the viewer
        // role when accepting the invitation.
        role: invitationRole(isRbacAllowed, values.role),
      });
      setShowNewinviteDialog(false);
      if (invite?.invite?.token) {
        setNewInviteToken(invite.invite.token);
        setOpenInviteCreated(true);
      }
      onInvited();
      toast.success(t('success'));
      form.reset();
    } catch (err) {
      console.error(err);
      toast.error(t('error'), {
        description: getErrorMessage(err),
      });
    }
  }

  function onOpenChange(open: boolean): void {
    setShowNewinviteDialog(open);
    form.reset();
  }

  return (
    <div>
      <Dialog open={showNewInviteDialog} onOpenChange={onOpenChange}>
        <DialogTrigger asChild>
          <Button type="button">
            <ButtonText leftIcon={<PlusIcon />} text={t('button')} />
          </Button>
        </DialogTrigger>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('title')}</DialogTitle>
            <DialogDescription>{t('description')}</DialogDescription>
          </DialogHeader>
          <Form {...form}>
            <form onSubmit={form.handleSubmit(onSubmit)} className="space-y-2">
              {showRoles && <LicenseFeatureNotice feature="rbac" />}
              <FormField
                control={form.control}
                name="email"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('email')}</FormLabel>
                    <FormDescription>{t('emailDescription')}</FormDescription>
                    <FormControl>
                      <Input
                        autoCapitalize="off"
                        data-1p-ignore // tells 1password extension to not autofill this field
                        type="email"
                        id="email"
                        placeholder="example@email.com"
                        {...field}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              {showRoles && (
                <FormField
                  control={form.control}
                  name="role"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('role')}</FormLabel>
                      <FormDescription>{t('roleDescription')}</FormDescription>
                      <FormControl>
                        <SelectAccountRole
                          role={field.value}
                          onChange={field.onChange}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              )}

              <DialogFooter>
                <Button
                  variant="outline"
                  onClick={() => setShowNewinviteDialog(false)}
                  type="button"
                >
                  {t('cancel')}
                </Button>
                <Button type="submit">{t('submit')}</Button>
              </DialogFooter>
            </form>
          </Form>
        </DialogContent>
      </Dialog>
      <InviteCreatedDialog
        open={openInviteCreated}
        setOpen={setOpenInviteCreated}
        token={newInviteToken}
      />
    </div>
  );
}
interface InviteCreatedDialogProps {
  open: boolean;
  setOpen: (value: boolean) => void;
  token: string;
}

function InviteCreatedDialog(props: InviteCreatedDialogProps): ReactElement {
  const { open, setOpen, token } = props;
  const { data: systemAppData } = useGetSystemAppConfig();
  const link = buildInviteLink(systemAppData?.publicAppBaseUrl ?? '', token);
  const t = useTranslations('settings.members.invite');
  const tInvites = useTranslations('settings.members.invites');

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent>
        <DialogHeader>
          <h3 className="font-semibold leading-none tracking-tight">
            {t('createdTitle')}
          </h3>
          <p className="text-sm text-muted-foreground">
            {t('createdDescription')}
          </p>
        </DialogHeader>
        <div className="flex flex-row gap-2">
          <Input value={link} readOnly />
          <CopyButton
            buttonVariant="outline"
            textToCopy={link}
            onCopiedText={tInvites('copied')}
            onHoverText={tInvites('copyLink')}
          />
        </div>
        <DialogFooter>
          <DialogClose asChild>
            <Button type="button">
              <ButtonText text={t('close')} />
            </Button>
          </DialogClose>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function buildInviteLink(baseUrl: string, token: string): string {
  return `${baseUrl}/invite?token=${token}`;
}
