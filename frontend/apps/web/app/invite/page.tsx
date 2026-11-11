'use client';
import Spinner from '@/components/Spinner';
import { useAccount } from '@/components/providers/account-provider';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Panel } from '@/components/ds';
import { MainLogo } from '@/components/site-header/MainLogo';
import { useGetSystemAppConfig } from '@/libs/hooks/useGetSystemAppConfig';
import { useHusonymUser } from '@/libs/hooks/useHusonymUser';
import { getErrorMessage } from '@/util/util';
import { useMutation } from '@connectrpc/connect-query';
import { UserAccountService } from '@husonym/sdk';
import { ExclamationTriangleIcon } from '@radix-ui/react-icons';
import { signIn, useSession } from 'next-auth/react';
import { useRouter, useSearchParams } from 'next/navigation';
import { ReactElement, useEffect, useRef, useState } from 'react';
import { toast } from 'sonner';

export default function InvitePage(): ReactElement {
  const { status } = useSession();
  const { setAccount, mutateUserAccount } = useAccount();
  const { isLoading: isUserLoading, error: userError } = useHusonymUser();
  const searchParams = useSearchParams();
  const token = searchParams.get('token');
  const router = useRouter();
  const [error, setError] = useState<string>();
  const isFirstRender = useRef(true);
  const { data: systemData, isLoading: isSystemDataLoading } =
    useGetSystemAppConfig();
  const { mutateAsync: acceptTeamInvite } = useMutation(
    UserAccountService.method.acceptTeamAccountInvite
  );

  useEffect(() => {
    if (isSystemDataLoading) {
      return;
    }
    if (status === 'unauthenticated') {
      // signin must be called on the client for this page so the redirectUrl is properly set
      signIn(systemData?.signInProviderId).catch((err) => {
        toast.error('Unable to redirect to signin page', {
          description: err,
        });
      });
      return;
    }
    // Don't accept invite until we know the user is authenticated and the user has been loaded
    // We wait for the user to load because sometimes the acceptTeamInvite runs before the user has been created
    // and results in a race that fails to invite unless the user refreshes the page.
    if (
      status === 'authenticated' &&
      !isUserLoading &&
      !userError &&
      token &&
      isFirstRender.current
    ) {
      isFirstRender.current = false;
      acceptTeamInvite({ token })
        .then((res) => {
          if (res.account) {
            toast.success('Invite accepted');
            setAccount(res.account);
            mutateUserAccount();
            router.replace(`/${res.account.name}`);
          }
        })
        .catch((err) => setError(getErrorMessage(err)));
    }
  }, [status, token, isUserLoading, userError, isSystemDataLoading]);

  return (
    <div className="flex min-h-[70vh] flex-col items-center justify-center gap-8 px-4 py-12 text-foreground">
      <div className="text-hi" aria-label="Husonym" role="img">
        <MainLogo bg="currentColor" />
      </div>
      <Panel className="flex w-full max-w-lg flex-col items-center gap-6 px-6 py-12 text-center shadow-pop">
        <h1 className="m-0 font-display text-2xl font-semibold tracking-[-0.02em]">
          Welcome to the team!
        </h1>
        {error ? (
          <Alert variant="destructive" className="text-left">
            <ExclamationTriangleIcon className="h-4 w-4" />
            <AlertTitle>Unable to accept invite</AlertTitle>
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : (
          <div
            className="flex flex-row items-center gap-3 text-muted-foreground"
            role="status"
          >
            <Spinner />
            Please wait. You will be redirected in a moment
          </div>
        )}
      </Panel>
    </div>
  );
}
