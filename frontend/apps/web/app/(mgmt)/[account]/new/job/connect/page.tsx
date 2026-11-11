'use client';
import FormPersist from '@/app/(mgmt)/FormPersist';
import TestConnectionBadge from '@/components/connections/TestConnectionBadge';
import { getServerScope } from '@/components/connections/checks/targets';
import {
  WizardFooter,
  WizardPanel,
  WizardSection,
} from '@/components/progress-steps/Wizard';
import SourceOptionsForm from '@/components/jobs/Form/SourceOptionsForm';
import { useAccount } from '@/components/providers/account-provider';
import { PageProps } from '@/components/types';
import { Button } from '@/components/ui/button';
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormMessage,
} from '@/components/ui/form';
import { Skeleton } from '@/components/ui/skeleton';
import { getSingleOrUndefined, splitConnections } from '@/libs/utils';
import { create } from '@bufbuild/protobuf';
import { useMutation, useQuery } from '@connectrpc/connect-query';
import { yupResolver } from '@/util/yup-form-resolver';
import {
  CheckConnectionConfigByIdResponse,
  CheckConnectionConfigByIdResponseSchema,
  Code,
  ConnectError,
  Connection,
  ConnectionConfigSchema,
  ConnectionRole,
  ConnectionSchema,
  ConnectionService,
} from '@husonym/sdk';
import { Cross2Icon, PlusIcon } from '@radix-ui/react-icons';
import { useRouter } from 'next/navigation';
import { ReactElement, use, useEffect, useState } from 'react';
import { useFieldArray, useForm } from 'react-hook-form';
import { useSessionStorage } from 'usehooks-ts';
import DestinationOptionsForm from '../../../../../../components/jobs/Form/DestinationOptionsForm';
import {
  getAllowedSyncDestinationTypes,
  getAllowedSyncSourceTypes,
  getConnectionType,
  isValidConnectionPair,
} from '../../../connections/util';
import { isConnectionSubsettable } from '../../../jobs/[id]/source/components/util';
import {
  getDefaultDestinationFormValueOptionsFromConnectionCase,
  getDefaultUnmappedTransformConfig,
  getNewJobSessionKeys,
} from '../../../jobs/util';
import { getJobProgressSteps } from '../JobsProgressSteps';
import NewJobWizard from '../NewJobWizard';
import { ConnectFormValues, DefineFormValues } from '../job-form-validations';
import ConnectionPicker from './ConnectionPicker';

const NEW_CONNECTION_VALUE = 'new-connection';

interface DestinationValidationState {
  isValidating: boolean;
  response?: CheckConnectionConfigByIdResponse;
}

export default function Page(props: PageProps): ReactElement {
  const searchParams = use(props.searchParams);
  const { account } = useAccount();
  const router = useRouter();
  useEffect(() => {
    if (!searchParams?.sessionId) {
      router.push(`/${account?.name}/new/job`);
    }
  }, [searchParams?.sessionId]);

  const sessionPrefix = getSingleOrUndefined(searchParams?.sessionId) ?? '';
  const sessionKeys = getNewJobSessionKeys(sessionPrefix);
  const sessionKey = sessionKeys.dataSync.connect;
  const [defaultValues] = useSessionStorage<ConnectFormValues>(sessionKey, {
    sourceId: '',
    sourceOptions: {},
    destinations: [{ connectionId: '', destinationOptions: {} }],
  });
  const [defineFormValues] = useSessionStorage<DefineFormValues>(
    sessionKeys.global.define,
    { jobName: '' }
  );
  const [isSourceValidating, setIsSourceValidating] = useState<boolean>(false);

  const [sourceValidationResponse, setSourceValidationResponse] = useState<
    CheckConnectionConfigByIdResponse | undefined
  >();

  const [destinationValidation, setDestinationValidation] = useState<
    Record<string, DestinationValidationState>
  >({});

  const { isLoading: isConnectionsLoading, data: connectionsData } = useQuery(
    ConnectionService.method.getConnections,
    { accountId: account?.id },
    { enabled: !!account?.id }
  );
  const connections = connectionsData?.connections ?? [];

  const form = useForm<ConnectFormValues>({
    mode: 'onChange',
    resolver: yupResolver(ConnectFormValues),
    values: defaultValues,
    context: { connections },
  });

  const { fields, append, remove } = useFieldArray({
    control: form.control,
    name: 'destinations',
  });

  async function onSubmit(_values: ConnectFormValues) {
    router.push(`/${account?.name}/new/job/schema?sessionId=${sessionPrefix}`);
  }

  const { postgres, mysql, s3, mongodb, gcpcs, dynamodb, mssql } =
    splitConnections(connections);

  const { mutateAsync: checkConnectionConfig } = useMutation(
    ConnectionService.method.checkConnectionConfigById
  );

  const sourceConnection = connections.find(
    (c) => c.id === form.watch('sourceId')
  );
  const destinationNames = form
    .watch('destinations')
    .map((d) => connections.find((c) => c.id === d.connectionId)?.name)
    .filter((name): name is string => !!name);

  return (
    <NewJobWizard
      id="newjobflowcontainer"
      steps={getJobProgressSteps(
        'data-sync',
        !!sourceConnection && isConnectionSubsettable(sourceConnection)
      )}
      stepName="connect"
      title="Where does the data come from, and where does it go?"
      description="Pick the source to read, then one or more destinations to write the anonymized data to."
    >
      <FormPersist formKey={sessionKey} form={form} />
      <Form {...form}>
        <form
          onSubmit={form.handleSubmit(onSubmit)}
          className="flex flex-1 flex-col gap-9"
        >
          <WizardSection
            id="connect-source"
            title="Source connection"
            description="The database the job reads from."
          >
            <div className="flex flex-col gap-4">
              <FormField
                control={form.control}
                name="sourceId"
                render={({ field }) => (
                  <FormItem>
                    <FormControl>
                      {isConnectionsLoading ? (
                        <Skeleton className="w-full h-12 rounded-lg" />
                      ) : (
                        <ConnectionPicker
                          ariaLabel="Source connection"
                          postgres={postgres}
                          mysql={mysql}
                          mongodb={mongodb}
                          dynamodb={dynamodb}
                          mssql={mssql}
                          newConnectionValue={NEW_CONNECTION_VALUE}
                          isChecking={isSourceValidating}
                          selectedDetail={
                            !isSourceValidating && (
                              <TestConnectionBadge
                                validationResponse={sourceValidationResponse}
                                connectionId={field.value}
                                accountName={account?.name ?? ''}
                              />
                            )
                          }
                          value={field.value}
                          onChange={async (value: string) => {
                            if (!value) {
                              return;
                            }
                            if (value === NEW_CONNECTION_VALUE) {
                              const destIds = new Set(
                                form
                                  .getValues('destinations')
                                  .map((d) => d.connectionId)
                              );

                              const urlParams = new URLSearchParams({
                                returnTo: `/${account?.name}/new/job/connect?sessionId=${sessionPrefix}&from=new-connection`,
                              });

                              const connTypes = new Set(
                                connections
                                  .filter((c) => destIds.has(c.id))
                                  .map((c) =>
                                    getConnectionType(
                                      c.connectionConfig ??
                                        create(ConnectionConfigSchema, {})
                                    )
                                  )
                                  .filter((x) => !!x)
                              );
                              const allowedSourceTypes =
                                getAllowedSyncSourceTypes(
                                  Array.from(connTypes)
                                );
                              allowedSourceTypes.forEach((sourceType) =>
                                urlParams.append('connectionType', sourceType)
                              );

                              router.push(
                                `/${account?.name}/new/connection?${urlParams.toString()}`
                              );
                              return;
                            }
                            field.onChange(value);
                            const connection =
                              connections.find((c) => c.id === value) ??
                              create(ConnectionSchema, {});
                            const connectionType = getConnectionType(
                              connection.connectionConfig ??
                                create(ConnectionConfigSchema, {})
                            );
                            setIsSourceValidating(true);
                            try {
                              const res = await checkConnectionConfig({
                                id: form.getValues('sourceId'),
                                scope: getServerScope(
                                  ConnectionRole.SOURCE,
                                  defineFormValues.workflowSettings?.engine
                                ),
                              });
                              setSourceValidationResponse(res);
                            } catch (err) {
                              if (
                                err instanceof ConnectError &&
                                err.code === Code.InvalidArgument &&
                                err.message.includes('unsupported operation')
                              ) {
                                setSourceValidationResponse(undefined);
                              } else {
                                setSourceValidationResponse(
                                  create(
                                    CheckConnectionConfigByIdResponseSchema,
                                    {
                                      isConnected: false,
                                      connectionError:
                                        err instanceof Error
                                          ? err.message
                                          : 'unknown error',
                                    }
                                  )
                                );
                              }
                            } finally {
                              setIsSourceValidating(false);
                            }

                            if (connectionType === 'pgConfig') {
                              form.setValue(
                                'sourceOptions',
                                {
                                  postgres: {
                                    newColumnAdditionStrategy: 'halt',
                                    columnRemovalStrategy: 'continue',
                                  },
                                },
                                {
                                  shouldDirty: true,
                                  shouldTouch: true,
                                  shouldValidate: true,
                                }
                              );
                            } else if (connectionType === 'mysqlConfig') {
                              form.setValue(
                                'sourceOptions',
                                {
                                  mysql: {
                                    newColumnAdditionStrategy: 'halt',
                                    columnRemovalStrategy: 'continue',
                                  },
                                },
                                {
                                  shouldDirty: true,
                                  shouldTouch: true,
                                  shouldValidate: true,
                                }
                              );
                            } else if (connectionType === 'dynamodbConfig') {
                              form.setValue(
                                'sourceOptions',
                                {
                                  dynamodb: {
                                    unmappedTransformConfig:
                                      getDefaultUnmappedTransformConfig(),
                                    enableConsistentRead: false,
                                  },
                                },
                                {
                                  shouldDirty: true,
                                  shouldTouch: true,
                                  shouldValidate: true,
                                }
                              );
                            } else if (connectionType === 'mssqlConfig') {
                              form.setValue(
                                'sourceOptions',
                                {
                                  mssql: {
                                    newColumnAdditionStrategy: 'halt',
                                    columnRemovalStrategy: 'continue',
                                  },
                                },
                                {
                                  shouldDirty: true,
                                  shouldTouch: true,
                                  shouldValidate: true,
                                }
                              );
                            } else {
                              form.setValue(
                                'sourceOptions',
                                {},
                                {
                                  shouldDirty: true,
                                  shouldTouch: true,
                                  shouldValidate: true,
                                }
                              );
                            }
                          }}
                        />
                      )}
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              {hasSourceOptions(sourceConnection) && (
                <WizardPanel title="When the source schema changes">
                  <FormField
                    control={form.control}
                    name="sourceOptions"
                    render={({ field }) => (
                      <FormItem>
                        <FormControl>
                          <SourceOptionsForm
                            connection={sourceConnection}
                            value={field.value}
                            setValue={(newOpts) => field.onChange(newOpts)}
                          />
                        </FormControl>
                      </FormItem>
                    )}
                  />
                </WizardPanel>
              )}
            </div>
          </WizardSection>
          <WizardSection
            id="connect-destinations"
            title="Where to write the anonymized data?"
            description="The source itself cannot be a destination."
          >
            <div className="flex flex-col gap-8">
              {fields.map((val, index) => {
                const destErrs = form.formState.errors?.destinations ?? [];
                const destConnection = connections.find(
                  (c) =>
                    c.id === form.watch(`destinations.${index}.connectionId`)
                );
                return (
                  <div className="flex flex-col gap-4" key={val.id}>
                    {fields.length > 1 && (
                      <div className="flex items-center gap-2">
                        <span className="text-[13px] font-medium text-muted-foreground">
                          Destination {index + 1}
                        </span>
                        <Button
                          type="button"
                          variant="ghost"
                          size="sm"
                          className="ml-auto"
                          aria-label={`Remove destination ${index + 1}`}
                          onClick={() => {
                            if (fields.length != 1) {
                              remove(index);
                            }
                          }}
                        >
                          <Cross2Icon className="size-4" aria-hidden="true" />
                          Remove
                        </Button>
                      </div>
                    )}
                    <div>
                      <div>
                        <FormField
                          control={form.control}
                          name={`destinations.${index}.connectionId`}
                          render={({ field }) => (
                            <FormItem>
                              <FormControl>
                                {isConnectionsLoading ? (
                                  <Skeleton className="w-full h-12 rounded-lg" />
                                ) : (
                                  <ConnectionPicker
                                    ariaLabel={`Destination ${index + 1}`}
                                    postgres={postgres}
                                    mysql={mysql}
                                    s3={s3}
                                    mongodb={mongodb}
                                    gcpcs={gcpcs}
                                    dynamodb={dynamodb}
                                    mssql={mssql}
                                    newConnectionValue={NEW_CONNECTION_VALUE}
                                    disabledReasons={getDestinationDisabledReasons(
                                      form.watch('sourceId'),
                                      form
                                        .watch('destinations')
                                        .map((d, i) =>
                                          i === index ? '' : d.connectionId
                                        ),
                                      connections
                                    )}
                                    isChecking={
                                      !!field.value &&
                                      destinationValidation[field.value]
                                        ?.isValidating
                                    }
                                    selectedDetail={
                                      <TestConnectionBadge
                                        validationResponse={
                                          destinationValidation[field.value]
                                            ?.response
                                        }
                                        connectionId={field.value}
                                        accountName={account?.name ?? ''}
                                      />
                                    }
                                    value={field.value}
                                    onChange={async (value: string) => {
                                      if (!value) {
                                        return;
                                      }
                                      const sourceId =
                                        form.getValues('sourceId');
                                      const connection = connections.find(
                                        (c) => c.id === sourceId
                                      );

                                      if (value === NEW_CONNECTION_VALUE) {
                                        const urlParams = new URLSearchParams({
                                          returnTo: `/${account?.name}/new/job/connect?sessionId=${sessionPrefix}&from=new-connection`,
                                        });

                                        const allowedDestinationConnectionTypes =
                                          getAllowedSyncDestinationTypes(
                                            connection?.connectionConfig?.config
                                              .case
                                          );
                                        allowedDestinationConnectionTypes.forEach(
                                          (ct) =>
                                            urlParams.append(
                                              'connectionType',
                                              ct
                                            )
                                        );

                                        router.push(
                                          `/${account?.name}/new/connection?${urlParams.toString()}`
                                        );
                                        return;
                                      }
                                      // set values
                                      field.onChange(value);
                                      const destConnection = connections.find(
                                        (c) => c.id === value
                                      );
                                      const destConnType = getConnectionType(
                                        destConnection?.connectionConfig ??
                                          create(ConnectionConfigSchema, {})
                                      );

                                      setDestinationValidation((prevState) => ({
                                        ...prevState,
                                        [value]: {
                                          isValidating: true,
                                          response: create(
                                            CheckConnectionConfigByIdResponseSchema,
                                            {}
                                          ),
                                        },
                                      }));
                                      try {
                                        const res = await checkConnectionConfig(
                                          {
                                            id: value,
                                            scope: getServerScope(
                                              ConnectionRole.DESTINATION,
                                              defineFormValues.workflowSettings
                                                ?.engine
                                            ),
                                          }
                                        );
                                        setDestinationValidation(
                                          (prevState) => ({
                                            ...prevState,
                                            [value]: {
                                              isValidating: false,
                                              response: res,
                                            },
                                          })
                                        );
                                      } catch (err) {
                                        if (
                                          err instanceof ConnectError &&
                                          err.code === Code.InvalidArgument &&
                                          err.message.includes(
                                            'unsupported operation'
                                          )
                                        ) {
                                          setDestinationValidation(
                                            (prevState) => ({
                                              ...prevState,
                                              [value]: {
                                                isValidating: false,
                                                response: undefined,
                                              },
                                            })
                                          );
                                        } else {
                                          setDestinationValidation(
                                            (prevState) => ({
                                              ...prevState,
                                              [value]: {
                                                isValidating: false,
                                                response: create(
                                                  CheckConnectionConfigByIdResponseSchema,
                                                  {
                                                    isConnected: false,
                                                    connectionError:
                                                      err instanceof Error
                                                        ? err.message
                                                        : 'unknown error',
                                                  }
                                                ),
                                              },
                                            })
                                          );
                                        }
                                      } finally {
                                        setIsSourceValidating(false);
                                      }

                                      const newOpts =
                                        getDefaultDestinationFormValueOptionsFromConnectionCase(
                                          destConnType,
                                          () => new Set()
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
                                )}
                              </FormControl>
                              <FormMessage />
                            </FormItem>
                          )}
                        />
                      </div>
                    </div>
                    {hasDestinationOptions(destConnection) && (
                      <WizardPanel
                        title={`Before writing to ${destConnection?.name}`}
                      >
                        <FormField
                          control={form.control}
                          name={`destinations.${index}.destinationOptions`}
                          render={({ field }) => (
                            <FormItem>
                              <FormControl>
                                <DestinationOptionsForm
                                  connection={destConnection}
                                  value={field.value}
                                  setValue={(newOpts) => {
                                    field.onChange(newOpts);
                                  }}
                                  hideDynamoDbTableMappings={true}
                                  destinationDetailsRecord={{}} // not used beacause we are hiding dynamodb table mappings
                                  errors={destErrs[index]?.destinationOptions}
                                />
                              </FormControl>
                            </FormItem>
                          )}
                        />
                      </WizardPanel>
                    )}
                  </div>
                );
              })}

              <Button
                type="button"
                variant="link"
                className="self-start px-0"
                onClick={() => {
                  append({
                    connectionId: '',
                    destinationOptions: {},
                  });
                }}
              >
                <PlusIcon className="size-4" aria-hidden="true" />
                Add another destination
              </Button>
            </div>
          </WizardSection>

          <WizardFooter
            summary={[
              defineFormValues.jobName,
              sourceConnection &&
                `${sourceConnection.name} → ${
                  destinationNames.length > 0
                    ? destinationNames.join(', ')
                    : '…'
                }`,
            ]}
          >
            <Button
              variant="outline"
              type="button"
              onClick={() => {
                if (searchParams?.from === 'new-connection') {
                  router.push(
                    `/${account?.name}/new/job/define?sessionId=${sessionPrefix}`
                  );
                  return;
                }
                router.back();
              }}
            >
              Back
            </Button>
            <Button type="submit" disabled={!form.formState.isValid}>
              Continue
            </Button>
          </WizardFooter>
        </form>
      </Form>
    </NewJobWizard>
  );
}

/** Why a connection cannot be one more destination of this job. */
function getDestinationDisabledReasons(
  sourceId: string,
  otherDestinationIds: string[],
  connections: Connection[]
): Record<string, string> {
  const reasons: Record<string, string> = {};
  const source = connections.find((c) => c.id === sourceId);
  connections.forEach((c) => {
    if (source && !isValidConnectionPair(source, c)) {
      reasons[c.id] = 'Not a valid destination for this source';
    }
  });
  otherDestinationIds.forEach((id) => {
    if (id) {
      reasons[id] = 'Already a destination of this job';
    }
  });
  if (sourceId) {
    reasons[sourceId] = 'This is the source of this job';
  }
  return reasons;
}

function hasSourceOptions(connection?: Connection): boolean {
  switch (connection?.connectionConfig?.config.case) {
    case 'pgConfig':
    case 'mysqlConfig':
    case 'mssqlConfig':
    case 'dynamodbConfig':
      return true;
    default:
      return false;
  }
}

function hasDestinationOptions(connection?: Connection): boolean {
  switch (connection?.connectionConfig?.config.case) {
    case 'pgConfig':
    case 'mysqlConfig':
    case 'mssqlConfig':
    case 'awsS3Config':
      return true;
    default:
      return false;
  }
}
