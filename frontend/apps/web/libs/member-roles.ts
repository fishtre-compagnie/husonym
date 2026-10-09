// Whether the roles of an account's members are shown and can be given: the Role
// columns, "Update Role" and the role of an invitation. In the organization of the
// instance they always are: whoever signs in enters it as a viewer, and an
// administrator must be able to give more. Elsewhere the deployment decides.
export function areRoleControlsShown(
  isRbacEnabled: boolean,
  accountId: string | undefined,
  organizationAccountId: string | undefined
): boolean {
  if (isRbacEnabled) {
    return true;
  }
  return !!accountId && accountId === organizationAccountId;
}
