import { useAccount } from '@/components/providers/account-provider';
import { areRoleControlsShown } from '@/libs/member-roles';
import { useQuery } from '@connectrpc/connect-query';
import { UserAccountService } from '@husonym/sdk';
import { useGetSystemAppConfig } from './useGetSystemAppConfig';

// Whether the roles of the members of the active account are shown and can be given.
export function useRoleControls(): boolean {
  const { account } = useAccount();
  const { data: config } = useGetSystemAppConfig();
  const { data: systemInfo } = useQuery(
    UserAccountService.method.getSystemInformation
  );
  return areRoleControlsShown(
    config?.isRbacEnabled ?? false,
    account?.id,
    systemInfo?.instanceOrganizationAccountId
  );
}
