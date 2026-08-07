import '@/app/globals.css';
import { ThemeProvider } from '@/components/providers/theme-provider';
import { Metadata } from 'next';
import { ReactElement } from 'react';
import BaseLayout from './BaseLayout';

export const metadata: Metadata = {
  title: 'Husonym',
  description: 'Open Source Data Anonymization and Synthetic Data',
  icons: [{ rel: 'icon', url: '/favicon.ico' }],
};

export default async function RootLayout({
  children,
}: {
  children: React.ReactNode;
}): Promise<ReactElement> {
  return (
    <html lang="en" suppressHydrationWarning>
      <head />
      <body className="min-h-screen bg-background font-sans antialiased overflow-scroll">
        <ThemeProvider
          attribute="class"
          defaultTheme="system"
          enableSystem
          disableTransitionOnChange
        >
          <BaseLayout>{children}</BaseLayout>
        </ThemeProvider>
      </body>
    </html>
  );
}
