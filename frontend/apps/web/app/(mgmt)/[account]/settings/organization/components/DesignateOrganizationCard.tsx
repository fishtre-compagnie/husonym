'use client';

import ConfirmationDialog from '@/components/ConfirmationDialog';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
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
import { getErrorMessage } from '@/util/util';
import { yupResolver } from '@/util/yup-form-resolver';
import { CreateTeamFormValues } from '@/yup-validations/account-switcher';
import { useMutation, useQuery } from '@connectrpc/connect-query';
import {
  ConnectError,
  UserAccount,
  UserAccountService,
  UserAccountType,
} from '@husonym/sdk';
import { useTranslations } from 'next-intl';
import { useRouter } from 'next/navigation';
import { ReactElement, useState } from 'react';
import { useForm } from 'react-hook-form';
import { toast } from 'sonner';

interface Props {
  // The account to make the organization of the instance: the active one.
  account: UserAccount;
}

export default function DesignateOrganizationCard(props: Props): ReactElement {
  const { account } = props;
  const isPersonal = account.type === UserAccountType.PERSONAL;
  const router = useRouter();
  const t = useTranslations('settings.organization.designate');
  const { mutateAsync: setInstanceOrganization } = useMutation(
    UserAccountService.method.setInstanceOrganization
  );
  const { refetch: refetchSystemInfo } = useQuery(
    UserAccountService.method.getSystemInformation
  );
  const { refetch: refetchAccounts } = useQuery(
    UserAccountService.method.getUserAccounts
  );
  // A team takes its name under the same rules wherever it is created.
  const form = useForm<CreateTeamFormValues>({
    mode: 'onChange',
    resolver: yupResolver(CreateTeamFormValues),
    defaultValues: { name: '', convertPersonalToTeam: false },
  });
  const [refusal, setRefusal] = useState<string>();

  async function onDesignate(): Promise<void> {
    setRefusal(undefined);
    let organizationId: string;
    try {
      const resp = await setInstanceOrganization({
        accountId: account.id,
        // Read for a personal account only: a team account keeps its name.
        name: isPersonal ? form.getValues('name') : '',
      });
      organizationId = resp.accountId;
    } catch (err) {
      // Kept on the page rather than in a toast, as a refused license key is: the
      // reason is worth reading twice.
      setRefusal(refusalMessage(err));
      return;
    }
    toast.success(t('success'));
    // Outside of what catches a refusal: the organization is retained by now. The
    // accounts are read again before the page moves, for the account may have changed
    // its name and the app must know it under the new one.
    const [, accounts] = await Promise.all([
      refetchSystemInfo(),
      refetchAccounts(),
    ]);
    const organization = accounts.data?.accounts.find(
      (a) => a.id === organizationId
    );
    if (!organization) {
      toast.error(t('followError'));
      return;
    }
    router.push(`/${organization.name}/jobs`);
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('title')}</CardTitle>
        <CardDescription>{t('description')}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {refusal && (
          <Alert variant="destructive">
            <AlertTitle>{t('refused')}</AlertTitle>
            <AlertDescription>{refusal}</AlertDescription>
          </Alert>
        )}
        <p className="text-sm">
          {t('joinAsViewers')}
          {isPersonal && t('personalBecomesTeam')} {t('cannotUndo')}
        </p>
        {isPersonal && (
          <Form {...form}>
            <FormField
              control={form.control}
              name="name"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('nameLabel')}</FormLabel>
                  <FormDescription>{t('nameDescription')}</FormDescription>
                  <FormControl>
                    <Input
                      autoCapitalize="off" // we don't allow capitals in team names
                      data-1p-ignore // tells 1password extension to not autofill this field
                      placeholder="acme"
                      className="max-w-sm"
                      {...field}
                    />
                  </FormControl>
                  <FormMessage className="mt-0" />
                </FormItem>
              )}
            />
          </Form>
        )}
        <div>
          <ConfirmationDialog
            trigger={
              <Button
                type="button"
                disabled={isPersonal && !form.formState.isValid}
              >
                {t('button')}
              </Button>
            }
            headerText={t('confirmHeader')}
            description={t('confirmDescription')}
            buttonText={t('button')}
            onConfirm={onDesignate}
          />
        </div>
      </CardContent>
    </Card>
  );
}

function refusalMessage(err: unknown): string {
  if (err instanceof ConnectError) {
    // The message of the API is shown as it is: it tells apart who may not set the
    // organization, and an instance that already has one.
    return err.rawMessage;
  }
  return getErrorMessage(err);
}
