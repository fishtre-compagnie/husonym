'use client';

import { useGetSystemAppConfig } from '@/libs/hooks/useGetSystemAppConfig';
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

// Pages that start the sign-in themselves, once they have said where it leads.
const SIGN_IN_PAGES = ['/account-login/'];

export function SessionProvider({ children, session }: Props) {
  const { data, isLoading } = useGetSystemAppConfig();
  const pathname = usePathname();
  if (isLoading) {
    return <Skeleton />;
  }
  const isSignInPage = SIGN_IN_PAGES.some((p) => pathname?.startsWith(p));
  if (data?.isAuthEnabled && !isSessionValid(session) && !isSignInPage) {
    signIn(data.signInProviderId);
    return <Skeleton />;
  }
  return (
    <NextAuthSessionProvider session={session}>
      {children}
    </NextAuthSessionProvider>
  );
}

function isSessionValid(session: Session | null): boolean {
  if (!session) {
    return false;
  }
  const expiryDate = parseISO(session.expires);
  return !isPast(expiryDate);
}
