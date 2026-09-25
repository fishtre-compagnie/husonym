'use client';
import AccountProvider from '@/components/providers/account-provider';
import { usePathname } from 'next/navigation';
import { ReactElement, ReactNode } from 'react';

// Pages shown before anyone has signed in: they stand alone, without the app around them.
const SIGN_IN_PAGES = ['/account-login/'];

export function isSignInPage(pathname: string | null): boolean {
  return SIGN_IN_PAGES.some((p) => pathname?.startsWith(p));
}

interface Props {
  header: ReactNode;
  footer: ReactNode;
  // What only a signed-in user sees, such as the onboarding dialog.
  extras: ReactNode;
  children: ReactNode;
}

/**
 * The app around a page: its account, header and footer. A sign-in page has none of them:
 * without a session, their calls to the API fail, and they would stand between the
 * visitor and what the page asks.
 */
export default function AppShell(props: Props): ReactElement {
  const { header, footer, extras, children } = props;
  const pathname = usePathname();
  if (isSignInPage(pathname)) {
    return (
      <div className="relative flex min-h-screen flex-col">
        <div className="flex-1 container">{children}</div>
      </div>
    );
  }
  return (
    <AccountProvider>
      <div className="relative flex min-h-screen flex-col">
        {header}
        <div className="flex-1 container" id="top-level-layout">
          {children}
        </div>
        {footer}
        {extras}
      </div>
    </AccountProvider>
  );
}
