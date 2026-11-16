import SiteFooter from '@/components/SiteFooter';
import WelcomeDialog from '@/components/onboarding-checklist/WelcomeDialog';
import AppShell from '@/components/AppShell';
import { LOCALE_COOKIE, toPreference } from '@/i18n/config';
import { LocaleProvider } from '@/components/providers/locale-provider';
import ConnectProvider from '@/components/providers/connect-provider';
import TanstackQueryProvider from '@/components/providers/query-provider';
import { SessionProvider } from '@/components/providers/session-provider';
import Sidebar from '@/components/shell/Sidebar';
import SiteHeader from '@/components/site-header/SiteHeader';
import { Toaster } from '@/components/ui/sonner';
import { cookies } from 'next/headers';
import { ReactElement, ReactNode } from 'react';
import { auth } from './api/auth/[...nextauth]/auth';
import { getSystemAppConfig } from './api/config/config';

interface Props {
  children: ReactNode;
}
export default async function BaseLayout(props: Props): Promise<ReactElement> {
  const { children } = props;
  const session = await auth();
  const { publicHusonymApiBaseUrl, isAuthEnabled } = getSystemAppConfig();
  const cookiePreference = toPreference(
    (await cookies()).get(LOCALE_COOKIE)?.value
  );

  return (
    <ConnectProvider apiBaseUrl={publicHusonymApiBaseUrl}>
      <TanstackQueryProvider>
        <SessionProvider session={session}>
          <LocaleProvider
            cookiePreference={cookiePreference}
            isAuthEnabled={isAuthEnabled}
          >
            <AppShell
              sidebar={<Sidebar isAuthEnabled={isAuthEnabled} />}
              header={<SiteHeader />}
              footer={<SiteFooter />}
              extras={<WelcomeDialog />}
            >
              {children}
            </AppShell>
            {/* https://sonner.emilkowal.ski/styling for styling documentation */}
            <Toaster richColors closeButton />
          </LocaleProvider>
        </SessionProvider>
      </TanstackQueryProvider>
    </ConnectProvider>
  );
}
