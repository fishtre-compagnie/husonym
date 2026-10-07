import { areRunLogsHidden, isFeatureAvailable } from '@/libs/license/license';
import { useQuery } from '@connectrpc/connect-query';
import { UserAccountService } from '@husonym/sdk';

// Whether the license of the instance allows a feature, as far as the interface knows.
// Until the license was read — the request is pending, or it failed — the feature
// counts as allowed: neither greys out the interface.
export function useLicenseFeature(name: string): {
  allowed: boolean;
  isLoading: boolean;
} {
  const { data, isLoading } = useQuery(
    UserAccountService.method.getSystemInformation
  );
  return {
    allowed: isFeatureAvailable(data !== undefined, data?.license, name),
    isLoading,
  };
}

// Whether the logs of a run are hidden: only under a license in force that does not
// include them. A license that has lapsed, or none at all, leaves them readable.
export function useRunLogsHidden(): boolean {
  const { data } = useQuery(UserAccountService.method.getSystemInformation);
  return areRunLogsHidden(data !== undefined, data?.license);
}
