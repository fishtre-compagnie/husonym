'use client';
import ButtonText from '@/components/ButtonText';
import EmptyState, { EmptyStateLinkButton } from '@/components/EmptyState';
import { PageBody, PageHeader } from '@/components/ds';
import { useAccount } from '@/components/providers/account-provider';
import SkeletonTable from '@/components/skeleton/SkeletonTable';
import { Button } from '@/components/ui/button';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { useQuery } from '@connectrpc/connect-query';
import { TransformersService } from '@husonym/sdk';
import { PlusIcon } from '@radix-ui/react-icons';
import NextLink from 'next/link';
import { ReadonlyURLSearchParams, useSearchParams } from 'next/navigation';
import { ReactElement, useMemo } from 'react';
import { IoMdCode } from 'react-icons/io';
import { useTranslations } from 'next-intl';
import { getSystemTransformerColumns } from './components/SystemTransformersTable/columns';
import { SystemTransformersDataTable } from './components/SystemTransformersTable/data-table';
import { getUserDefinedTransformerColumns } from './components/UserDefinedTransformersTable/columns';
import { UserDefinedTransformersDataTable } from './components/UserDefinedTransformersTable/data-table';

export default function Transformers(): ReactElement {
  const t = useTranslations('connections.transformers.list');
  const searchParams = useSearchParams();
  const defaultTab = getTableTabFromParams(searchParams);
  return (
    <PageBody className="transformer-page">
      <PageHeader
        title={t('title')}
        description={t('description')}
        actions={<NewTransformerButton />}
      />
      <TransformersTable defaultTab={defaultTab} />
    </PageBody>
  );
}

function getTableTabFromParams(
  searchParams: ReadonlyURLSearchParams
): TableTab {
  const tab = searchParams.get('tab');
  return tab && isTableTab(tab) ? tab : 'ud';
}

function isTableTab(input: string): input is TableTab {
  return input === 'ud' || input === 'system';
}

type TableTab = 'ud' | 'system';

interface TransformersTableProps {
  defaultTab: TableTab;
}

function TransformersTable(props: TransformersTableProps): ReactElement {
  const { defaultTab } = props;
  const t = useTranslations('connections.transformers.list');
  const tTable = useTranslations('connections.transformers.table');
  const { data, isLoading: isSystemTransformersLoading } = useQuery(
    TransformersService.method.getSystemTransformers
  );
  const { account } = useAccount();
  const {
    data: udTransformers,
    isLoading: userDefinedTransformersLoading,
    refetch: userDefinedTransformerRefetch,
  } = useQuery(
    TransformersService.method.getUserDefinedTransformers,
    { accountId: account?.id ?? '' },
    { enabled: !!account?.id }
  );

  const systemTransformers = data?.transformers ?? [];
  const userDefinedTransformers = udTransformers?.transformers ?? [];

  // memoizing these columns to prevent infinite re-render when hovering over next link
  const systemTransformerColumns = useMemo(
    () =>
      getSystemTransformerColumns({
        accountName: account?.name ?? '',
        t: tTable,
      }),
    [account?.name, tTable]
  );
  // memoizing these columns to prevent infinite re-render when hovering over next link
  const userDefinedTransformerColumns = useMemo(
    () =>
      getUserDefinedTransformerColumns({
        onTransformerDeleted() {
          userDefinedTransformerRefetch();
        },
        accountName: account?.name ?? '',
        t: tTable,
      }),
    [account?.name, tTable]
  );

  if (isSystemTransformersLoading || userDefinedTransformersLoading) {
    return <SkeletonTable />;
  }

  return (
    <div>
      <Tabs defaultValue={defaultTab}>
        <TabsList>
          <TabsTrigger value="ud">{t('tabUserDefined')}</TabsTrigger>
          <TabsTrigger value="system">{t('tabSystem')}</TabsTrigger>
        </TabsList>
        <TabsContent value="ud">
          {userDefinedTransformers.length == 0 ? (
            <EmptyState
              title={t('emptyTitle')}
              description={t('emptyDescription')}
              icon={<IoMdCode className="w-8 h-8 text-primary" />}
              extra={
                <EmptyStateLinkButton
                  buttonText={t('emptyCta')}
                  href={`/${account?.name}/new/transformer`}
                />
              }
            />
          ) : (
            <UserDefinedTransformersDataTable
              columns={userDefinedTransformerColumns}
              data={userDefinedTransformers}
            />
          )}
        </TabsContent>
        <TabsContent value="system">
          <SystemTransformersDataTable
            columns={systemTransformerColumns}
            data={systemTransformers}
          />
        </TabsContent>
      </Tabs>
    </div>
  );
}

function NewTransformerButton(): ReactElement {
  const t = useTranslations('connections.transformers.list');
  const { account } = useAccount();
  return (
    <NextLink href={`/${account?.name}/new/transformer`}>
      <Button>
        <ButtonText leftIcon={<PlusIcon />} text={t('newTransformer')} />
      </Button>
    </NextLink>
  );
}
