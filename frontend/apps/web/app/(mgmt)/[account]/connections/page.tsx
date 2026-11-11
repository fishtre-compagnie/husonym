'use client';
import ButtonText from '@/components/ButtonText';
import { PageBody, PageHeader } from '@/components/ds';
import EmptyState, { EmptyStateLinkButton } from '@/components/EmptyState';
import { useAccount } from '@/components/providers/account-provider';
import SkeletonTable from '@/components/skeleton/SkeletonTable';
import { Button } from '@/components/ui/button';
import { useQuery } from '@connectrpc/connect-query';
import { ConnectionService } from '@husonym/sdk';
import { PlusIcon } from '@radix-ui/react-icons';
import NextLink from 'next/link';
import { ReactElement, useMemo } from 'react';
import { GoWorkflow } from 'react-icons/go';
import { getColumns } from './components/ConnectionsTable/columns';
import { DataTable } from './components/ConnectionsTable/data-table';

export default function Connections(): ReactElement {
  return (
    <PageBody className="connections-page">
      <PageHeader
        title="Connections"
        description="The databases and storages your jobs read from and write to."
        actions={<NewConnectionButton />}
      />
      <ConnectionTable />
    </PageBody>
  );
}

interface ConnectionTableProps {}

function ConnectionTable(props: ConnectionTableProps): ReactElement {
  const {} = props;
  const { account } = useAccount();
  const { data, isLoading, refetch } = useQuery(
    ConnectionService.method.getConnections,
    { accountId: account?.id ?? '' },
    { enabled: !!account?.id }
  );

  const columns = useMemo(
    () =>
      getColumns({
        accountName: account?.name ?? '',
        onConnectionDeleted() {
          refetch();
        },
      }),
    [account?.name ?? '']
  );

  if (isLoading) {
    return <SkeletonTable />;
  }

  const connections = data?.connections ?? [];

  return (
    <div>
      {connections.length == 0 ? (
        <EmptyState
          title="No Connections yet"
          description="Get started by adding your first connection. Connections help you
          integrate and sync data across your databases."
          icon={<GoWorkflow className="w-8 h-8 text-primary" />}
          extra={
            <EmptyStateLinkButton
              buttonText="Create your first Connection"
              href={`/${account?.name}/new/connection`}
            />
          }
        />
      ) : (
        <DataTable columns={columns} data={connections} />
      )}
    </div>
  );
}

interface NewConnectionButtonprops {}

function NewConnectionButton(props: NewConnectionButtonprops): ReactElement {
  const {} = props;
  const { account } = useAccount();
  return (
    <NextLink href={`/${account?.name}/new/connection`}>
      <Button>
        <ButtonText leftIcon={<PlusIcon />} text="New Connection" />
      </Button>
    </NextLink>
  );
}
