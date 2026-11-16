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
import { useTranslations } from 'next-intl';
import { getColumns } from './components/ConnectionsTable/columns';
import { DataTable } from './components/ConnectionsTable/data-table';

export default function Connections(): ReactElement {
  const t = useTranslations('connections.list');
  return (
    <PageBody className="connections-page">
      <PageHeader
        title={t('title')}
        description={t('description')}
        actions={<NewConnectionButton />}
      />
      <ConnectionTable />
    </PageBody>
  );
}

interface ConnectionTableProps {}

function ConnectionTable(props: ConnectionTableProps): ReactElement {
  const {} = props;
  const t = useTranslations('connections.list');
  const tTable = useTranslations('connections.table');
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
        t: tTable,
        onConnectionDeleted() {
          refetch();
        },
      }),
    [account?.name ?? '', tTable]
  );

  if (isLoading) {
    return <SkeletonTable />;
  }

  const connections = data?.connections ?? [];

  return (
    <div>
      {connections.length == 0 ? (
        <EmptyState
          title={t('emptyTitle')}
          description={t('emptyDescription')}
          icon={<GoWorkflow className="w-8 h-8 text-primary" />}
          extra={
            <EmptyStateLinkButton
              buttonText={t('emptyCta')}
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
  const t = useTranslations('connections.list');
  const { account } = useAccount();
  return (
    <NextLink href={`/${account?.name}/new/connection`}>
      <Button>
        <ButtonText leftIcon={<PlusIcon />} text={t('newConnection')} />
      </Button>
    </NextLink>
  );
}
