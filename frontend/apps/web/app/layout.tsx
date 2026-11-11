import '@/app/globals.css';
import { ThemeProvider } from '@/components/providers/theme-provider';
import { Metadata } from 'next';
import { Inter, JetBrains_Mono, Space_Grotesk } from 'next/font/google';
import { ReactElement } from 'react';
import BaseLayout from './BaseLayout';

export const metadata: Metadata = {
  title: 'Husonym',
  description: 'Open Source Data Anonymization and Synthetic Data',
  icons: [{ rel: 'icon', url: '/favicon.ico' }],
};

const inter = Inter({
  subsets: ['latin'],
  variable: '--font-inter',
  display: 'swap',
});
const spaceGrotesk = Space_Grotesk({
  subsets: ['latin'],
  weight: ['500', '600', '700'],
  variable: '--font-space-grotesk',
  display: 'swap',
});
const jetbrainsMono = JetBrains_Mono({
  subsets: ['latin'],
  weight: ['400', '500'],
  variable: '--font-jetbrains-mono',
  display: 'swap',
});

export default async function RootLayout({
  children,
}: {
  children: React.ReactNode;
}): Promise<ReactElement> {
  return (
    <html
      lang="en"
      suppressHydrationWarning
      className={`${inter.variable} ${spaceGrotesk.variable} ${jetbrainsMono.variable}`}
    >
      <head />
      <body className="min-h-screen bg-background font-sans text-sm antialiased">
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
