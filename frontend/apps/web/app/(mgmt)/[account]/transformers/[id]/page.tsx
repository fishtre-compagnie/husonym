'use client';
import { use } from 'react';
import { PageBody, PageHeader, Pill } from '@/components/ds';
import SkeletonForm from '@/components/skeleton/SkeletonForm';
import { PageProps } from '@/components/types';
import { getTransformerDataTypesString } from '@/util/util';
import { create } from '@bufbuild/protobuf';
import { createConnectQueryKey, useQuery } from '@connectrpc/connect-query';
import {
  GetUserDefinedTransformerByIdResponseSchema,
  TransformersService,
} from '@husonym/sdk';
import { useQueryClient } from '@tanstack/react-query';
import RemoveTransformerButton from './components/RemoveTransformerButton';
import UpdateTransformerForm from './components/UpdateTransformerForm';

export default function UpdateUserDefinedTransformerPage(props: PageProps) {
  const params = use(props.params);
  const id = params?.id ?? '';

  const { data, isLoading } = useQuery(
    TransformersService.method.getUserDefinedTransformerById,
    { transformerId: id },
    { enabled: !!id }
  );
  const queryclient = useQueryClient();

  if (isLoading) {
    return (
      <div className="mt-10">
        <SkeletonForm />
      </div>
    );
  }

  return (
    <PageBody className="max-w-[1320px]">
      <PageHeader
        title={data?.transformer?.name ?? 'Custom Transformer'}
        description={
          <Pill className="font-mono">
            {getTransformerDataTypesString(data?.transformer?.dataTypes ?? [])}
          </Pill>
        }
        actions={
          <RemoveTransformerButton
            transformerID={data?.transformer?.id ?? ''}
          />
        }
      />
      <div className="transformer-details-container">
        <div>
          <div className="flex flex-col">
            <div>
              {data?.transformer && (
                <UpdateTransformerForm
                  currentTransformer={data.transformer}
                  onUpdated={(updatedTransformer) => {
                    const key = createConnectQueryKey({
                      schema:
                        TransformersService.method
                          .getUserDefinedTransformerById,
                      input: { transformerId: id },
                      cardinality: undefined,
                    });
                    queryclient.setQueryData(
                      key,
                      create(GetUserDefinedTransformerByIdResponseSchema, {
                        transformer: updatedTransformer,
                      })
                    );
                  }}
                />
              )}
            </div>
          </div>
        </div>
      </div>
    </PageBody>
  );
}
