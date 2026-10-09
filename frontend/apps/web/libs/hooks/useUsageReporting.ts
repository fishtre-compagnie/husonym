import { useQuery } from '@connectrpc/connect-query';
import { UsageService } from '@husonym/sdk';

// How this instance reports its usage, as an account may read it. One read for the
// License page and for the notice of the Usage pages: they share its answer. It is not
// tried again: what depends on it says that it could not be read, or says nothing.
export function useUsageReporting(accountId: string) {
  return useQuery(
    UsageService.method.getUsageReporting,
    { accountId },
    { enabled: !!accountId, retry: false }
  );
}
