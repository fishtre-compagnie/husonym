'use client';

import { LocalePreference, toPreference } from '@/i18n/config';
import { readLocaleCookie, writeLocaleCookie } from '@/i18n/locale-cookie';
import {
  createConnectQueryKey,
  useMutation,
  useQuery,
} from '@connectrpc/connect-query';
import { UserAccountService } from '@husonym/sdk';
import { useQueryClient } from '@tanstack/react-query';
import { useSession } from 'next-auth/react';
import { useRouter } from 'next/navigation';
import {
  ReactElement,
  ReactNode,
  createContext,
  use,
  useCallback,
  useEffect,
  useMemo,
  useRef,
} from 'react';

interface LocaleContextValue {
  // The user's choice: a language, or '' to follow the instance's default.
  preference: LocalePreference;
  // Stores the choice with the user, mirrors it in the cookie, renders the page again.
  // Throws when the API refuses it: the page stays in the language it was in.
  setPreference(preference: LocalePreference): Promise<void>;
  isSaving: boolean;
}

const LocaleContext = createContext<LocaleContextValue | undefined>(undefined);

interface Props {
  children: ReactNode;
  // The preference the server rendered with, read from the cookie.
  cookiePreference: LocalePreference;
  isAuthEnabled: boolean;
}

/**
 * The user's language. The server renders with the cookie; the preference itself is
 * stored with the user. When the two differ (chosen on another device, a new browser),
 * the cookie follows the user and the page renders again, once.
 */
export function LocaleProvider(props: Props): ReactElement {
  const { children, cookiePreference, isAuthEnabled } = props;
  const router = useRouter();
  const queryClient = useQueryClient();
  const session = useSession();
  // Without auth, the API answers for its anonymous user.
  const canAsk = !isAuthEnabled || session.status === 'authenticated';

  const { data } = useQuery(
    UserAccountService.method.getUser,
    {},
    { enabled: canAsk }
  );
  const { mutateAsync, isPending } = useMutation(
    UserAccountService.method.setUserPreferences
  );

  const stored = data ? toPreference(data.locale) : undefined;
  const isSynced = useRef(false);
  useEffect(() => {
    if (stored === undefined || isSynced.current) {
      return;
    }
    isSynced.current = true;
    if (stored !== readLocaleCookie()) {
      writeLocaleCookie(stored);
      router.refresh();
    }
  }, [stored, router]);

  const setPreference = useCallback(
    async (preference: LocalePreference): Promise<void> => {
      const res = await mutateAsync({ locale: preference });
      const saved = toPreference(res.locale);
      writeLocaleCookie(saved);
      await queryClient.invalidateQueries({
        queryKey: createConnectQueryKey({
          schema: UserAccountService.method.getUser,
          cardinality: undefined,
        }),
      });
      router.refresh();
    },
    [mutateAsync, queryClient, router]
  );

  const value = useMemo(
    (): LocaleContextValue => ({
      preference: stored ?? cookiePreference,
      setPreference,
      isSaving: isPending,
    }),
    [stored, cookiePreference, setPreference, isPending]
  );

  return <LocaleContext value={value}>{children}</LocaleContext>;
}

export function useLocalePreference(): LocaleContextValue {
  const ctx = use(LocaleContext);
  if (!ctx) {
    throw new Error('useLocalePreference must be used within LocaleProvider');
  }
  return ctx;
}
