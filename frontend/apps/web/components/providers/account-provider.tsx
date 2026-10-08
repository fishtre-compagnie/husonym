'use client';
import { accountToOpen, defaultAccountName } from '@/libs/default-account';
import { useHusonymUser } from '@/libs/hooks/useHusonymUser';
import { getSingleOrUndefined } from '@/libs/utils';
import { getErrorMessage } from '@/util/util';
import { useQuery } from '@connectrpc/connect-query';
import { UserAccount, UserAccountService } from '@husonym/sdk';
import { useParams, useRouter } from 'next/navigation';
import {
  ReactElement,
  ReactNode,
  createContext,
  useContext,
  useEffect,
  useState,
} from 'react';
import { useLocalStorage, useSessionStorage } from 'usehooks-ts';

interface AccountContextType {
  account: UserAccount | undefined;
  setAccount(updatedAccount: UserAccount): void;
  isLoading: boolean;
  mutateUserAccount(): void;
  // The name of the account to route to while none is active. Nothing until it is
  // known, and nothing when the user has no account.
  defaultAccountName: string | undefined;
  // Why no account could be opened for the user, when none could.
  entryError: string | undefined;
  isRetryingEntry: boolean;
  retryEntry(): void;
}
const AccountContext = createContext<AccountContextType>({
  account: undefined,
  setAccount: () => {},
  isLoading: false,
  mutateUserAccount() {},
  defaultAccountName: undefined,
  entryError: undefined,
  isRetryingEntry: false,
  retryEntry() {},
});

interface Props {
  children: ReactNode;
}

const STORAGE_ACCOUNT_KEY = 'account';

export default function AccountProvider(props: Props): ReactElement {
  const { children } = props;
  const { account } = useParams();
  const accountName = useGetAccountName();

  const [, setLastSelectedAccountSession] = useSessionStorage<
    string | undefined
  >(STORAGE_ACCOUNT_KEY, undefined);
  const [, setLastSelectedAccountLocal] = useLocalStorage<string | undefined>(
    STORAGE_ACCOUNT_KEY,
    undefined
  );

  const {
    data: user,
    isLoading: isUserLoading,
    isFetching: isUserFetching,
    error: userError,
    refetch: refetchUser,
  } = useHusonymUser();

  const {
    data: accountsResponse,
    isLoading,
    refetch: mutate,
    isPending,
    isFetching,
    error: accountsError,
  } = useQuery(UserAccountService.method.getUserAccounts, undefined, {
    enabled: !isUserLoading,
  });
  const { data: systemInfo, isPending: isSystemInfoPending } = useQuery(
    UserAccountService.method.getSystemInformation
  );

  const router = useRouter();

  const [userAccount, setUserAccount] = useState<UserAccount | undefined>(
    undefined
  );

  // Where to land when no account was chosen, or when the one chosen is not among the
  // user's. It waits for the system information, which says which account is the
  // organization of the instance; one that could not be read leaves it out.
  const fallbackAccountName = isSystemInfoPending
    ? undefined
    : defaultAccountName(
        accountsResponse?.accounts ?? [],
        systemInfo?.instanceOrganizationAccountId
      );
  // An account that keeps its id may change its name and its type (a personal account
  // made the organization of the instance): the list is followed by what it holds.
  const accountsKey = accountsResponse?.accounts
    .map((a) => `${a.id}:${a.name}:${a.type}`)
    .join(',');

  useEffect(() => {
    // need to check for isPending because the query is conditionally enabled but the data is not yet available
    if (isLoading || accountsResponse == null || isPending) {
      return;
    }
    if (userAccount && userAccount.name === accountName) {
      return;
    }
    const target = accountToOpen(
      accountsResponse.accounts,
      accountName,
      fallbackAccountName
    );
    // Nowhere to land yet, or no account at all, which entryError tells.
    if (!target) {
      return;
    }
    if (
      userAccount &&
      userAccount.id === target.id &&
      userAccount.name === target.name &&
      userAccount.type === target.type
    ) {
      return;
    }

    setUserAccount(target);
    // Update both storages
    setLastSelectedAccountSession(target.name);
    setLastSelectedAccountLocal(target.name);
    const accountParam = getSingleOrUndefined(account);
    // only want to push here if we actually have an account param. Otherwise we might push on a page like /invite
    if (!!accountParam && accountParam !== target.name) {
      router.push(`/${target.name}/jobs`);
    }
  }, [
    userAccount?.id,
    userAccount?.name,
    userAccount?.type,
    accountsKey,
    isLoading,
    isPending,
    accountName,
    fallbackAccountName,
  ]);

  const entryError = ((): string | undefined => {
    // The entry that never passed is told whatever account the list still gives. One
    // that passed and later fails to be read again is not: it must not take the page,
    // and what is being typed in it, away.
    if (userError && !user) {
      return getErrorMessage(userError);
    }
    // Without a user set there is nobody to open an account for: the visitor has not
    // signed in, and that is not told here.
    if (userAccount || !user) {
      return undefined;
    }
    if (accountsError) {
      return getErrorMessage(accountsError);
    }
    // The list may have been read by another component before the user was set: it
    // counts once it was read again.
    if (!isFetching && accountsResponse?.accounts.length === 0) {
      return 'You are signed in, but you have no account on this instance.';
    }
    return undefined;
  })();

  function retryEntry(): void {
    refetchUser().then(() => mutate());
  }

  function setAccount(userAccount: UserAccount): void {
    if (userAccount.name !== accountName) {
      // Update both storages before routing
      setLastSelectedAccountSession(userAccount.name);
      setLastSelectedAccountLocal(userAccount.name);
      setUserAccount(userAccount);
      router.push(`/${userAccount.name}`);
    }
  }

  return (
    <AccountContext.Provider
      value={{
        account: userAccount,
        setAccount: setAccount,
        isLoading,
        mutateUserAccount: mutate,
        defaultAccountName: fallbackAccountName,
        entryError,
        isRetryingEntry: isUserFetching || isFetching,
        retryEntry,
      }}
    >
      {children}
    </AccountContext.Provider>
  );
}

// The account chosen explicitly: by the URL, else by the last selection. Nothing when
// none was, which leaves the choice to the default.
function useGetAccountName(): string | undefined {
  const { account } = useParams();

  const [sessionAccount] = useSessionStorage<string | undefined>(
    STORAGE_ACCOUNT_KEY,
    undefined
  );
  const [localAccount] = useLocalStorage<string | undefined>(
    STORAGE_ACCOUNT_KEY,
    undefined
  );

  const accountParam = getSingleOrUndefined(account);
  if (accountParam) {
    return accountParam;
  }
  // Prefer session storage account over local storage
  const singleSessionAccount = getSingleOrUndefined(sessionAccount);
  if (singleSessionAccount) {
    return singleSessionAccount;
  }
  const singleLocalAccount = getSingleOrUndefined(localAccount);
  if (singleLocalAccount) {
    return singleLocalAccount;
  }
  return undefined;
}

export function useAccount(): AccountContextType {
  const account = useContext(AccountContext);
  return account;
}
