'use client';
import { use } from 'react';
import { PageBody } from '@/components/ds';
import { useAccount } from '@/components/providers/account-provider';
import ResourceId from '@/components/ResourceId';
import SkeletonForm from '@/components/skeleton/SkeletonForm';
import { PageProps } from '@/components/types';
import { useQuery } from '@connectrpc/connect-query';
import { ConnectionService } from '@husonym/sdk';
import Error from 'next/error';
import { useRouter } from 'next/navigation';
import { useGetConnectionComponentDetails } from '../components/useGetConnectionComponentDetails';

export default function EditConnectionPage(props: PageProps) {
  const params = use(props.params);
  const id = params?.id ?? '';
  const { account } = useAccount();

  const {
    data,
    isLoading,
    refetch: mutateGetConnection,
  } = useQuery(
    ConnectionService.method.getConnection,
    { id: id, excludeSensitive: false },
    { enabled: !!id }
  );

  const router = useRouter();

  const connectionComponent = useGetConnectionComponentDetails({
    mode: 'edit',
    connection: data?.connection!,
    onSaved: (updatedConnection) => {
      // maybe use the query cache here for faster round trip and navigation
      mutateGetConnection().finally(() => {
        router.push(`/${account?.name}/connections/${updatedConnection.id}`);
      });
    },
    subHeading: (
      <ResourceId
        labelText={data?.connection?.id ?? ''}
        copyText={data?.connection?.id ?? ''}
        onHoverText="Copy the connection id"
      />
    ),
  });

  if (!id) {
    return <Error statusCode={404} />;
  }

  if (isLoading) {
    return (
      <div className="mt-10">
        <SkeletonForm />
      </div>
    );
  }
  if (!isLoading && !data?.connection) {
    return <Error statusCode={404} />;
  }

  return (
    <PageBody className="max-w-[1320px]">
      {connectionComponent.header}
      <div className="connection-details-container">
        <div className="flex flex-col gap-8">
          <div>{connectionComponent.body}</div>
        </div>
      </div>
    </PageBody>
  );
}
