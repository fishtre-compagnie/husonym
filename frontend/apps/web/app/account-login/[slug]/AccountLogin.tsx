'use client';
import ButtonText from '@/components/ButtonText';
import Spinner from '@/components/Spinner';
import { Button } from '@/components/ui/button';
import { Panel } from '@/components/ds';
import { MainLogo } from '@/components/site-header/MainLogo';
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

  async function run(
    action: () => Promise<void>,
    failure: string
  ): Promise<void> {
    setIsBusy(true);
    try {
      await action();
    } catch (err) {
      toast.error(failure, { description: getErrorMessage(err) });
      setIsBusy(false);
    }
  }

  // This page again, once signed out: the provider's logout comes back to it.
  const thisPage = `/account-login/${slug}${
    callbackPath ? `?${new URLSearchParams({ callbackUrl: callbackPath })}` : ''
  }`;

  const signedInAs =
    status === 'authenticated'
      ? (session?.user?.email ?? session?.user?.name ?? 'someone')
      : null;

  return (
    <div className="flex min-h-screen flex-col items-center justify-center gap-8 bg-background px-4 py-12 text-foreground">
      <div className="text-hi" aria-label="Husonym" role="img">
        <MainLogo bg="currentColor" />
      </div>
      <Panel className="w-full max-w-lg shadow-pop">
        <div className="flex flex-col gap-2 px-6 pt-6">
          <h1 className="m-0 font-display text-2xl font-semibold tracking-[-0.02em]">
            Sign in to <span className="font-mono text-[0.85em]">{slug}</span>
          </h1>
          <p className="text-sm text-muted-foreground">
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
          </p>
        </div>
        {signedInAs ? (
          <div className="mx-6 mt-5 rounded-xl border border-warn-line bg-warn-soft px-4 py-3 text-sm text-warn-fg">
            You are signed in as{' '}
            <span className="font-semibold">{signedInAs}</span>. Signing in to{' '}
            {slug} ends that session first.
          </div>
        ) : null}
        <div className="flex flex-wrap justify-end gap-2 p-6">
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
                onClick={() =>
                  run(() => signOutEverywhere(thisPage), 'Unable to sign out')
                }
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
                run(
                  () =>
                    // The account travels in the body of the sign-in, which its CSRF
                    // token protects: it is this click, and nothing else, that
                    // chooses the account's provider.
                    signIn(config?.signInProviderId, {
                      callbackUrl: callbackPath ?? '/',
                      account: slug,
                    }),
                  'Unable to sign in'
                )
              }
            >
              <ButtonText
                leftIcon={isBusy ? <Spinner /> : undefined}
                text="Continue"
              />
            </Button>
          )}
        </div>
      </Panel>
    </div>
  );
}
