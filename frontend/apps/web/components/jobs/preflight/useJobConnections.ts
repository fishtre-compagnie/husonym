'use client';
import { getSqlSourceConnectionId } from '@/components/connections/checks/targets';
import { useQuery } from '@connectrpc/connect-query';
import { ConnectionService, Job } from '@husonym/sdk';
import { useTranslations } from 'next-intl';
import { JobConnection } from './report';

// useJobConnections names the connections of a job, each with its role: what the findings
// of a pre-flight report about a connection are grouped by.
export function useJobConnections(
  job: Job | undefined,
  accountId: string | undefined
): JobConnection[] {
  const t = useTranslations('mapping.preflight');
  const { data } = useQuery(
    ConnectionService.method.getConnections,
    // Names only: what the connections store is not needed here.
    { accountId, excludeSensitive: true },
    { enabled: !!accountId }
  );
  const names = new Map(
    (data?.connections ?? []).map((c) => [c.id, c.name] as const)
  );
  const connections: JobConnection[] = [];
  const sourceId = getSqlSourceConnectionId(job?.source);
  if (sourceId) {
    connections.push({
      id: sourceId,
      name: names.get(sourceId) ?? t('unnamedConnection'),
      role: 'Source',
    });
  }
  for (const destination of job?.destinations ?? []) {
    connections.push({
      id: destination.connectionId,
      name: names.get(destination.connectionId) ?? t('unnamedConnection'),
      role: 'Destination',
    });
  }
  return connections;
}
