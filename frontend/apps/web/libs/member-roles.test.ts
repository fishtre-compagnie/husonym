import { areRoleControlsShown } from './member-roles';

describe('whether the roles of members are shown and can be given', () => {
  it('shows them in the organization of the instance, whatever the deployment says', () => {
    expect(areRoleControlsShown(false, 'o', 'o')).toBe(true);
  });

  it('hides them in another account when the deployment does not enable them', () => {
    expect(areRoleControlsShown(false, 'p', 'o')).toBe(false);
    expect(areRoleControlsShown(false, 'p', undefined)).toBe(false);
  });

  it('hides them while no account is open, even with no organization either', () => {
    expect(areRoleControlsShown(false, undefined, undefined)).toBe(false);
    expect(areRoleControlsShown(false, '', '')).toBe(false);
  });

  it('shows them in any account when the deployment enables them', () => {
    expect(areRoleControlsShown(true, 'p', 'o')).toBe(true);
    expect(areRoleControlsShown(true, 'p', undefined)).toBe(true);
    expect(areRoleControlsShown(true, undefined, undefined)).toBe(true);
  });
});
