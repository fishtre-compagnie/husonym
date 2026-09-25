'use client';

import { useGetSystemAppConfig } from '@/libs/hooks/useGetSystemAppConfig';
import { getSafePath } from '@/libs/safe-path';
import { isSignInPage } from '../AppShell';
import { isPast, parseISO } from 'date-fns';
import { Session } from 'next-auth';
import {
  SessionProvider as NextAuthSessionProvider,
  signIn,
} from 'next-auth/react';
import { usePathname } from 'next/navigation';
import { ReactNode } from 'react';
import { Skeleton } from '../ui/skeleton';

interface Props {
  children: ReactNode;
  session: Session | null;
}

export function SessionProvider({ children, session }: Props) {
  const { data, isLoading } = useGetSystemAppConfig();
  const pathname = usePathname();
  if (isLoading) {
    return <Skeleton />;
  }
  // A sign-in page starts the sign-in itself, once it has said where it leads.
  if (
    data?.isAuthEnabled &&
    !isSessionValid(session) &&
    !isSignInPage(pathname)
  ) {
    // Back from a provider's logout that was asked to come back to a sign-in page (the
    // logout's state, see provider-sign-out): go there rather than sign in straight away.
    const back = getSignInPageToReturnTo();
    if (back) {
      window.location.replace(back);
    } else {
      signIn(data.signInProviderId);
    }
    return <Skeleton />;
  }
  return (
    <NextAuthSessionProvider session={session}>
      {children}
    </NextAuthSessionProvider>
  );
}

function getSignInPageToReturnTo(): string | null {
  if (typeof window === 'undefined') {
    return null;
  }
  const state = getSafePath(
    new URLSearchParams(window.location.search).get('state')
  );
  return state && isSignInPage(state) ? state : null;
}

function isSessionValid(session: Session | null): boolean {
  if (!session) {
    return false;
  }
  const expiryDate = parseISO(session.expires);
  return !isPast(expiryDate);
}
