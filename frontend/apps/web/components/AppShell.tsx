'use client';
import AccountEntryGate from '@/components/providers/AccountEntryGate';
import AccountProvider from '@/components/providers/account-provider';
import { usePathname } from 'next/navigation';
import { ReactElement, ReactNode, ViewTransition } from 'react';

// Pages shown before anyone has signed in: they stand alone, without the app around them.
const SIGN_IN_PAGES = ['/account-login/'];

export function isSignInPage(pathname: string | null): boolean {
  return SIGN_IN_PAGES.some((p) => pathname?.startsWith(p));
}

// The job-creation wizard: a focused page with its own top bar and exit, without the
// sidebar and header around it. Adding a destination to an existing job is not part of
// it: that page stays in the app.
const WIZARD_PAGE = /^\/[^/]+\/new\/job(\/|$)/;

export function isWizardPage(pathname: string | null): boolean {
  return (
    !!pathname &&
    WIZARD_PAGE.test(pathname) &&
    !pathname.endsWith('/destination')
  );
}

interface Props {
  // The left column: logo, account, sections, user. Hidden under lg, where the
  // header's menu button opens it as a drawer.
  sidebar: ReactNode;
  header: ReactNode;
  footer: ReactNode;
  // What only a signed-in user sees, such as the onboarding dialog.
  extras: ReactNode;
  children: ReactNode;
}

/**
 * The app around a page: its account, sidebar, header and footer. A sign-in page has
 * none of them: without a session, their calls to the API fail, and they would stand
 * between the visitor and what the page asks.
 *
 * Pages lay out their own column (PageBody, or OverviewContainer): the shell gives
 * them the full width beside the sidebar.
 */
export default function AppShell(props: Props): ReactElement {
  const { sidebar, header, footer, extras, children } = props;
  const pathname = usePathname();
  if (isSignInPage(pathname)) {
    return (
      <div className="relative flex min-h-screen flex-col">
        <div className="flex-1 container">{children}</div>
      </div>
    );
  }
  if (isWizardPage(pathname)) {
    return (
      <AccountProvider>
        <main
          className="relative min-h-screen bg-background text-foreground"
          id="top-level-layout"
        >
          <PageTransition>
            <AccountEntryGate>{children}</AccountEntryGate>
          </PageTransition>
        </main>
        {extras}
      </AccountProvider>
    );
  }
  return (
    <AccountProvider>
      <div className="relative flex min-h-screen bg-background text-foreground">
        <aside className="sticky top-0 hidden h-screen w-60 shrink-0 overflow-y-auto border-r border-line-soft bg-sidebar lg:block">
          {sidebar}
        </aside>
        <div className="flex min-w-0 flex-1 flex-col">
          {header}
          <main className="min-w-0 flex-1" id="top-level-layout">
            <PageTransition>
              <AccountEntryGate>{children}</AccountEntryGate>
            </PageTransition>
          </main>
          {footer}
        </div>
        {extras}
      </div>
    </AccountProvider>
  );
}

/**
 * The page fades in and rises a little when the app moves to another one (see
 * `vt-page` in globals.css). The browser animates two snapshots on the compositor:
 * React renders nothing more. Only a navigation runs it: the updates of a page
 * (its data arriving, a filter) are not transitions, and stay instant.
 */
function PageTransition(props: { children: ReactNode }): ReactElement {
  return (
    <ViewTransition default="none" update="vt-page">
      <div>{props.children}</div>
    </ViewTransition>
  );
}
