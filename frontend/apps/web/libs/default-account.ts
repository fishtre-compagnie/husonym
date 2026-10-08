import { UserAccount, UserAccountType } from '@husonym/sdk';

type Account = Pick<UserAccount, 'id' | 'name' | 'type'>;

// The name of the account a user lands in when they chose none: the organization of
// the instance when they are in it, else their personal account, else the first account
// they have. Nothing when they have no account at all.
export function defaultAccountName(
  accounts: Account[],
  organizationAccountId: string | undefined
): string | undefined {
  const organization = organizationAccountId
    ? accounts.find((a) => a.id === organizationAccountId)
    : undefined;
  const personal = accounts.find((a) => a.type === UserAccountType.PERSONAL);
  return (organization ?? personal ?? accounts.at(0))?.name;
}

// The account to open: the one chosen by name when the user has it, else the default
// one. A name that matches no account, such as one remembered from before the account
// was renamed, falls through to the default.
export function accountToOpen<T extends Account>(
  accounts: T[],
  chosenName: string | undefined,
  defaultName: string | undefined
): T | undefined {
  return (
    accounts.find((a) => a.name === chosenName) ??
    accounts.find((a) => a.name === defaultName)
  );
}
