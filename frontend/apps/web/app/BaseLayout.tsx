import SiteFooter from '@/components/SiteFooter';
import WelcomeDialog from '@/components/onboarding-checklist/WelcomeDialog';
import AppShell from '@/components/AppShell';
import ConnectProvider from '@/components/providers/connect-provider';
import TanstackQueryProvider from '@/components/providers/query-provider';
import { SessionProvider } from '@/components/providers/session-provider';
import SiteHeader from '@/components/site-header/SiteHeader';
import { Toaster } from '@/components/ui/sonner';
import { ReactElement, ReactNode } from 'react';
import { auth } from './api/auth/[...nextauth]/auth';
import { getSystemAppConfig } from './api/config/config';

interface Props {
  children: ReactNode;
}
export default async function BaseLayout(props: Props): Promise<ReactElement> {
  const { children } = props;
  const session = await auth();
  const { publicHusonymApiBaseUrl } = getSystemAppConfig();

  return (
    <ConnectProvider apiBaseUrl={publicHusonymApiBaseUrl}>
      <TanstackQueryProvider>
        <SessionProvider session={session}>
          <AppShell
            header={<SiteHeader />}
            footer={<SiteFooter />}
            extras={<WelcomeDialog />}
          >
            {children}
          </AppShell>
          {/* https://sonner.emilkowal.ski/styling for styling documentation */}
          <Toaster richColors closeButton />
        </SessionProvider>
      </TanstackQueryProvider>
    </ConnectProvider>
  );
}
