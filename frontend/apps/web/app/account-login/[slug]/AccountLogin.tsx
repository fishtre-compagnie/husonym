'use client';
import ButtonText from '@/components/ButtonText';
import Spinner from '@/components/Spinner';
import { Button } from '@/components/ui/button';
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { useGetSystemAppConfig } from '@/libs/hooks/useGetSystemAppConfig';
import { signOutEverywhere } from '@/libs/sign-out';
import { getErrorMessage } from '@/util/util';
import { signIn, useSession } from 'next-auth/react';
import { useRouter } from 'next/navigation';
import { ReactElement, useState } from 'react';
import { toast } from 'sonner';

interface Props {
  slug: string;
  // The host of the account's provider, or the name of the deployment's.
  provider: string;
  ownProvider: boolean;
  callbackPath: string | null;
}

export default function AccountLogin(props: Props): ReactElement {
  const { slug, provider, ownProvider, callbackPath } = props;
  const { data: session, status } = useSession();
  const { data: config } = useGetSystemAppConfig();
  const router = useRouter();
  const [isBusy, setIsBusy] = useState(false);

  async function run(action: () => Promise<void>): Promise<void> {
    setIsBusy(true);
    try {
      await action();
    } catch (err) {
      toast.error('Unable to sign in', { description: getErrorMessage(err) });
      setIsBusy(false);
    }
  }

  const signedInAs =
    status === 'authenticated'
      ? (session?.user?.email ?? session?.user?.name ?? 'someone')
      : null;

  return (
    <div className="flex justify-center pt-24">
      <Card className="w-full max-w-lg">
        <CardHeader>
          <CardTitle>Sign in to {slug}</CardTitle>
          <CardDescription>
            {ownProvider ? (
              <>
                This account signs in with its own provider:{' '}
                <span className="font-semibold text-foreground">
                  {provider}
                </span>
                .
              </>
            ) : (
              <>This account signs in with {provider}.</>
            )}
          </CardDescription>
        </CardHeader>
        {signedInAs ? (
          <CardContent className="text-sm">
            You are signed in as{' '}
            <span className="font-semibold">{signedInAs}</span>. Signing in to{' '}
            {slug} ends that session first.
          </CardContent>
        ) : null}
        <CardFooter className="flex justify-end gap-2">
          {status === 'loading' ? (
            <Spinner />
          ) : signedInAs ? (
            <>
              <Button
                variant="outline"
                disabled={isBusy}
                onClick={() => router.push(callbackPath ?? '/')}
              >
                Stay signed in
              </Button>
              <Button
                disabled={isBusy}
                onClick={() => run(() => signOutEverywhere())}
              >
                <ButtonText
                  leftIcon={isBusy ? <Spinner /> : undefined}
                  text="Sign out and continue"
                />
              </Button>
            </>
          ) : (
            <Button
              disabled={isBusy || !config}
              onClick={() =>
                run(() =>
                  signIn(config?.signInProviderId, {
                    callbackUrl: callbackPath ?? '/',
                  })
                )
              }
            >
              <ButtonText
                leftIcon={isBusy ? <Spinner /> : undefined}
                text="Continue"
              />
            </Button>
          )}
        </CardFooter>
      </Card>
    </div>
  );
}
