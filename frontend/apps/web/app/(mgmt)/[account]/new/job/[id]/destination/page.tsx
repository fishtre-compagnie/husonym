'use client';
import { useCheckedSave } from '@/components/connections/checks/useCheckedSave';
import { isValidConnectionPair } from '@/app/(mgmt)/[account]/connections/util';
import {
  getConnectionIdFromSource,
  getDestinationDetailsRecord,
  isDynamoDBConnection,
} from '@/app/(mgmt)/[account]/jobs/[id]/source/components/util';
import {
  getDefaultDestinationFormValueOptionsFromConnectionCase,
  toJobDestinationOptions,
} from '@/app/(mgmt)/[account]/jobs/util';
import { PageBody, PageHeader } from '@/components/ds';
import DestinationOptionsForm from '@/components/jobs/Form/DestinationOptionsForm';
import { useAccount } from '@/components/providers/account-provider';
import SkeletonTable from '@/components/skeleton/SkeletonTable';
import { PageProps } from '@/components/types';
import { Button } from '@/components/ui/button';
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormMessage,
} from '@/components/ui/form';
import { splitConnections } from '@/libs/utils';
import { getErrorMessage } from '@/util/util';
import { NewDestinationFormValues } from '@/yup-validations/jobs';
import { create } from '@bufbuild/protobuf';
import { useMutation, useQuery } from '@connectrpc/connect-query';
import { yupResolver } from '@/util/yup-form-resolver';
import {
  Connection,
  ConnectionDataService,
  ConnectionSchema,
  ConnectionService,
  CreateJobDestination,
  CreateJobDestinationSchema,
  GetConnectionSchemaMapsResponseSchema,
  JobService,
} from '@husonym/sdk';
import { ArrowLeftIcon, Cross1Icon, PlusIcon } from '@radix-ui/react-icons';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { ReactElement, use } from 'react';
import { useFieldArray, useForm } from 'react-hook-form';
import { toast } from 'sonner';
import * as Yup from 'yup';
import ConnectionPicker from '../../connect/ConnectionPicker';

const FormValues = Yup.object({
  destinations: Yup.array(NewDestinationFormValues).required(
    'Destinations are required.'
  ),
});
type FormValues = Yup.InferType<typeof FormValues>;

export default function Page(props: PageProps): ReactElement {
  const params = use(props.params);
  const id = params?.id ?? '';
  const { account } = useAccount();
  const router = useRouter();
  const { data, isLoading } = useQuery(
    JobService.method.getJob,
    { id },
    { enabled: !!id }
  );
  const { data: connectionsData, isLoading: isConnectionsLoading } = useQuery(
    ConnectionService.method.getConnections,
    { accountId: account?.id },
    { enabled: !!account?.id }
  );
  const { mutateAsync: createJobConnections } = useMutation(
    JobService.method.createJobDestinationConnections
  );
  const { checkThenSave, dialog: checksDialog } = useCheckedSave();

  const connections = connectionsData?.connections ?? [];
  const connRecord = connections.reduce(
    (record, conn) => {
      record[conn.id] = conn;
      return record;
    },
    {} as Record<string, Connection>
  );
  const destinationConnectionIds = new Set(
    data?.job?.destinations.map((d) => d.connectionId)
  );
  const sourceConnectionId = getConnectionIdFromSource(data?.job?.source);
  const sourceConnection = sourceConnectionId
    ? connRecord[sourceConnectionId]
    : create(ConnectionSchema, {});

  const form = useForm({
    resolver: yupResolver(FormValues),
    defaultValues: {
      destinations: [{ connectionId: '', destinationOptions: {} }],
    },
  });

  const availableConnections = connections.filter(
    (c) =>
      c.id != sourceConnectionId &&
      !destinationConnectionIds?.has(c.id) &&
      isValidConnectionPair(sourceConnection, c)
  );

  const { append, remove } = useFieldArray({
    control: form.control,
    name: 'destinations',
  });

  const fields = form.watch('destinations');

  // Contains a list of the new destinations to be added that are specifically dynamo db connections
  const newDynamoDestConnections = fields
    .map((field) => connRecord[field.connectionId])
    .filter((conn) => !!conn && isDynamoDBConnection(conn));

  const { data: destinationConnectionSchemaMapsResp } = useQuery(
    ConnectionDataService.method.getConnectionSchemaMaps,
    {
      requests: newDynamoDestConnections.map((conn) => ({
        connectionId: conn.id,
      })),
    },
    { enabled: newDynamoDestConnections.length > 0 }
  );

  async function onSubmit(values: FormValues): Promise<void> {
    const currentJob = data?.job;
    if (!currentJob) {
      return;
    }
    const connMap = new Map(connections.map((c) => [c.id, c]));
    const destinations = values.destinations.map((d) =>
      create(CreateJobDestinationSchema, {
        connectionId: d.connectionId,
        options: toJobDestinationOptions(d, connMap.get(d.connectionId)),
      })
    );
    // Only the new destinations are checked: the rest of the job is not changed.
    await checkThenSave(
      { ...currentJob, destinations },
      () => saveDestinations(destinations),
      { checkSource: false }
    );
  }

  async function saveDestinations(
    destinations: CreateJobDestination[]
  ): Promise<void> {
    try {
      const job = await createJobConnections({
        jobId: id,
        destinations,
      });
      toast.success('Successfully created job destination(s)');
      if (job.job?.id) {
        router.push(`/${account?.name}/jobs/${job.job.id}/destinations`);
      } else {
        router.push(`/${account?.name}/jobs`);
      }
    } catch (err) {
      console.error(err);
      toast.error('Unable to create job destination(s)', {
        description: getErrorMessage(err),
      });
    }
  }

  const { postgres, mysql, s3, mongodb, gcpcs, dynamodb, mssql } =
    splitConnections(availableConnections);

  const jobName = data?.job?.name;

  return (
    <PageBody className="max-w-[960px]">
      <div className="flex flex-col gap-3">
        <Link
          href={`/${account?.name}/jobs/${id}/destinations`}
          className="inline-flex items-center gap-1.5 self-start text-[13px] text-link hover:text-foreground"
        >
          <ArrowLeftIcon aria-hidden="true" />
          {jobName ? `Back to ${jobName}` : 'Back to the job'}
        </Link>
        <PageHeader
          title="Add destinations"
          description={
            sourceConnectionId && connRecord[sourceConnectionId]
              ? `Where else should the anonymized data from ${connRecord[sourceConnectionId].name} be written? Only connections compatible with the source are listed.`
              : 'Where else should the anonymized data be written? Only connections compatible with the source are listed.'
          }
        />
      </div>

      {isLoading || isConnectionsLoading ? (
        <SkeletonTable />
      ) : (
        <Form {...form}>
          {checksDialog}
          <form
            onSubmit={form.handleSubmit(onSubmit)}
            className="flex flex-col gap-8"
          >
            <div className="flex flex-col gap-8">
              {fields.map((f, index) => {
                // not using the field here because it doesn't seem to always update when it needs to
                const connId = f.connectionId;
                const destOpts = f.destinationOptions;
                const destConnection = connRecord[connId] as
                  Connection | undefined;
                const destinationsErrors =
                  form.formState.errors.destinations ?? [];
                return (
                  <div key={index} className="flex flex-col gap-4">
                    <div className="flex items-center gap-2">
                      <span className="text-[13px] font-medium text-muted-foreground">
                        Destination {index + 1}
                      </span>
                      <Button
                        variant="ghost"
                        size="sm"
                        type="button"
                        className="ml-auto"
                        aria-label={`Remove destination ${index + 1}`}
                        onClick={() => {
                          remove(index);
                        }}
                      >
                        <Cross1Icon aria-hidden="true" />
                        Remove
                      </Button>
                    </div>
                    <div>
                      <div>
                        <FormField
                          control={form.control}
                          name={`destinations.${index}.connectionId`}
                          render={({ field }) => (
                            <FormItem>
                              <FormControl>
                                <ConnectionPicker
                                  ariaLabel={`Destination ${index + 1}`}
                                  postgres={postgres}
                                  mysql={mysql}
                                  mssql={mssql}
                                  s3={s3}
                                  mongodb={mongodb}
                                  gcpcs={gcpcs}
                                  dynamodb={dynamodb}
                                  disabledReasons={Object.fromEntries(
                                    fields
                                      .filter(
                                        (other, i) =>
                                          i !== index && !!other.connectionId
                                      )
                                      .map((other) => [
                                        other.connectionId,
                                        'Already added above',
                                      ])
                                  )}
                                  emptyLabel="No other connection can receive data from this source."
                                  value={field.value}
                                  onChange={(value: string) => {
                                    field.onChange(value);
                                    const newDest = connRecord[value];
                                    const newOpts =
                                      getDefaultDestinationFormValueOptionsFromConnectionCase(
                                        newDest.connectionConfig?.config.case,
                                        () =>
                                          new Set(
                                            data?.job?.mappings.map(
                                              (mapping) => mapping.table
                                            )
                                          )
                                      );
                                    form.setValue(
                                      `destinations.${index}.destinationOptions`,
                                      newOpts,
                                      {
                                        shouldDirty: true,
                                        shouldTouch: true,
                                        shouldValidate: true,
                                      }
                                    );
                                  }}
                                />
                              </FormControl>
                              <FormMessage />
                            </FormItem>
                          )}
                        />
                      </div>
                    </div>

                    {destConnection &&
                      destConnection.connectionConfig?.config.case !==
                        'mongoConfig' &&
                      destConnection.connectionConfig?.config.case !==
                        'gcpCloudstorageConfig' && (
                        <div className="rounded-[14px] border border-line-soft bg-card p-[18px]">
                          <DestinationOptionsForm
                            connection={destConnection}
                            value={destOpts}
                            setValue={(newOpts) => {
                              form.setValue(
                                `destinations.${index}.destinationOptions`,
                                newOpts,
                                {
                                  shouldDirty: true,
                                  shouldTouch: true,
                                  shouldValidate: true,
                                }
                              );
                            }}
                            hideDynamoDbTableMappings={
                              !isDynamoDBConnection(
                                destConnection ?? create(ConnectionSchema, {})
                              )
                            }
                            destinationDetailsRecord={getDestinationDetailsRecord(
                              fields.map((field) => ({
                                connectionId: field.connectionId,
                                id: field.connectionId,
                              })),
                              connRecord,
                              destinationConnectionSchemaMapsResp ??
                                create(
                                  GetConnectionSchemaMapsResponseSchema,
                                  {}
                                )
                            )}
                            errors={
                              destinationsErrors[index]?.destinationOptions
                            }
                          />
                        </div>
                      )}
                  </div>
                );
              })}
              <div className="flex w-full flex-wrap items-center justify-between gap-3 border-t border-line-soft pt-5">
                <Button
                  variant="link"
                  type="button"
                  className="px-0"
                  onClick={() => {
                    append({ connectionId: '', destinationOptions: {} });
                  }}
                >
                  <PlusIcon aria-hidden="true" />
                  Add another destination
                </Button>
                <Button
                  disabled={
                    !form.formState.isDirty ||
                    form.getValues().destinations.length === 0
                  }
                  type="submit"
                >
                  Save
                </Button>
              </div>
            </div>
          </form>
        </Form>
      )}
    </PageBody>
  );
}
