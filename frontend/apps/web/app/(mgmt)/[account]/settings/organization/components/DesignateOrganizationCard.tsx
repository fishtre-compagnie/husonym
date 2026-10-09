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
    toast.success('This account is now the organization of this instance');
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
      toast.error(
        'The organization was set, but the page could not follow it. Please try refreshing the page.'
      );
      return;
    }
    router.push(`/${organization.name}/jobs`);
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Make this account the organization</CardTitle>
        <CardDescription>
          This instance has no organization yet: each person who signs in works
          in an account of their own.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {refusal && (
          <Alert variant="destructive">
            <AlertTitle>The organization was not set</AlertTitle>
            <AlertDescription>{refusal}</AlertDescription>
          </Alert>
        )}
        <p className="text-sm">
          People who sign in to this instance will join this account as viewers.
          {isPersonal &&
            ' This personal account becomes a team account under the name given below, and keeps its connections, jobs and API keys.'}{' '}
          This cannot be undone from the interface.
        </p>
        {isPersonal && (
          <Form {...form}>
            <FormField
              control={form.control}
              name="name"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Organization name</FormLabel>
                  <FormDescription>
                    The name this account takes as a team account.
                  </FormDescription>
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
                Make it the organization
              </Button>
            }
            headerText="Make this account the organization of this instance?"
            description="People who sign in to this instance will join it as viewers. This cannot be undone from the interface."
            buttonText="Make it the organization"
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
