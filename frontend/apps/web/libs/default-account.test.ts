import { UserAccountType } from '@husonym/sdk';
import { accountToOpen, defaultAccountName } from './default-account';

const personal = { id: 'p', name: 'personal', type: UserAccountType.PERSONAL };
const organization = { id: 'o', name: 'acme', type: UserAccountType.TEAM };
const team = { id: 't', name: 'other-team', type: UserAccountType.TEAM };

describe('the account a user lands in when they chose none', () => {
  it('is the organization of the instance when the user is in it', () => {
    expect(defaultAccountName([personal, team, organization], 'o')).toBe(
      'acme'
    );
  });

  it('is the personal account when the user is not in the organization', () => {
    expect(defaultAccountName([team, personal], 'o')).toBe('personal');
    expect(defaultAccountName([team, personal], undefined)).toBe('personal');
  });

  it('is the first account when there is neither', () => {
    expect(defaultAccountName([team, organization], undefined)).toBe(
      'other-team'
    );
  });

  it('is nothing when the user has no account', () => {
    expect(defaultAccountName([], 'o')).toBeUndefined();
    expect(defaultAccountName([], undefined)).toBeUndefined();
  });
});

describe('the account that is opened', () => {
  it('is the one chosen by name when the user has it', () => {
    expect(accountToOpen([personal, organization], 'personal', 'acme')).toBe(
      personal
    );
  });

  it('is the default one when none was chosen', () => {
    expect(accountToOpen([personal, organization], undefined, 'acme')).toBe(
      organization
    );
  });

  it('is the default one when the name chosen matches no account', () => {
    // The only account of the user is not named "personal", which a browser may
    // still remember: a page opens, on the account the user has.
    const accounts = [organization];
    expect(
      accountToOpen(accounts, 'personal', defaultAccountName(accounts, 'o'))
    ).toBe(organization);
    expect(
      accountToOpen(
        accounts,
        'personal',
        defaultAccountName(accounts, undefined)
      )
    ).toBe(organization);
  });

  it('is none when the user has no account', () => {
    expect(
      accountToOpen([], 'personal', defaultAccountName([], 'o'))
    ).toBeUndefined();
  });
});
