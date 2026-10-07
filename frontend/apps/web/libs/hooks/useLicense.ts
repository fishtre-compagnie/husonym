import { isFeatureAllowed } from '@/libs/license/license';
import { useQuery } from '@connectrpc/connect-query';
import { UserAccountService } from '@husonym/sdk';

// Whether the license of the instance allows a feature. While the license is being
// read the feature counts as allowed: a pending request never greys out the interface.
export function useLicenseFeature(name: string): {
  allowed: boolean;
  isLoading: boolean;
} {
  const { data, isLoading } = useQuery(
    UserAccountService.method.getSystemInformation
  );
  return {
    allowed: isLoading || isFeatureAllowed(data?.license, name),
    isLoading,
  };
}
