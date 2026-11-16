'use client';
import { getErrorMessage } from '@/util/util';
import { Code, ConnectError } from '@husonym/sdk';
import {
  isServer,
  QueryCache,
  QueryClient,
  QueryClientProvider,
} from '@tanstack/react-query';
import { useTranslations } from 'next-intl';
import { ReactElement, ReactNode, useEffect } from 'react';
import { toast } from 'sonner';

interface Props {
  children: ReactNode;
}

let browserQueryClient: QueryClient | undefined = undefined;

// The title of the toast a failed query shows, in the UI language. The query clients
// outlive the renders, so they read it here, kept current by useErrorTitle.
const errorTitle = { current: 'Something went wrong' };

function useErrorTitle(): void {
  const t = useTranslations('common');
  const title = t('somethingWentWrong');
  useEffect(() => {
    errorTitle.current = title;
  }, [title]);
}

export default function TanstackQueryProvider(props: Props): ReactElement {
  const { children } = props;
  useErrorTitle();

  if (isServer) {
    const client = new QueryClient({
      defaultOptions: {
        queries: { staleTime: 60 * 1000 },
      },
    });
    return (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    );
  }

  if (!browserQueryClient) {
    browserQueryClient = new QueryClient({
      queryCache: new QueryCache({
        // good blog here: https://tkdodo.eu/blog/react-query-error-handling
        onError(error, query) {
          toast.error(errorTitle.current, {
            description: getErrorMessage(error),
            id: query.queryKey.toString(),
          });
        },
      }),
      defaultOptions: {},
    });
  }

  return (
    <QueryClientProvider client={browserQueryClient}>
      {children}
    </QueryClientProvider>
  );
}

// This is a query provider that ignores 404 errors.
export function TanstackQueryProviderIgnore404Errors(
  props: Props
): ReactElement {
  const { children } = props;
  useErrorTitle();

  if (isServer) {
    const client = new QueryClient({
      defaultOptions: {
        queries: { staleTime: 60 * 1000 },
      },
    });
    return (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    );
  }

  const browserQueryClient404 = new QueryClient({
    queryCache: new QueryCache({
      // good blog here: https://tkdodo.eu/blog/react-query-error-handling
      onError(error, query) {
        if (error instanceof ConnectError && error.code !== Code.NotFound) {
          toast.error(errorTitle.current, {
            description: getErrorMessage(error),
            id: query.queryKey.toString(),
          });
        }
      },
    }),
    defaultOptions: {},
  });

  return (
    <QueryClientProvider client={browserQueryClient404}>
      {children}
    </QueryClientProvider>
  );
}
