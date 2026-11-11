'use client';
import FormPersist from '@/app/(mgmt)/FormPersist';
import { getConnectionType } from '@/app/(mgmt)/[account]/connections/util';
import {
  getDefaultDestinationFormValueOptionsFromConnectionCase,
  getNewJobSessionKeys,
} from '@/app/(mgmt)/[account]/jobs/util';
import TestConnectionBadge from '@/components/connections/TestConnectionBadge';
import { getServerScope } from '@/components/connections/checks/targets';
import {
  WizardFooter,
  WizardPanel,
  WizardSection,
} from '@/components/progress-steps/Wizard';
import DestinationOptionsForm from '@/components/jobs/Form/DestinationOptionsForm';
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
  ConnectionConfigSchema,
  ConnectionRole,
  ConnectionService,
} from '@husonym/sdk';
import { useRouter } from 'next/navigation';
import { ReactElement, use, useEffect, useState } from 'react';
import { Control, useForm, useWatch } from 'react-hook-form';
import { useSessionStorage } from 'usehooks-ts';
import {
  getJobProgressStepLabels,
  getJobProgressSteps,
} from '../../../JobsProgressSteps';
import NewJobWizard from '../../../NewJobWizard';
import ConnectionPicker from '../../../connect/ConnectionPicker';
import {
  DefineFormValues,
  SingleTableConnectFormValues,
} from '../../../job-form-validations';

const NEW_CONNECTION_VALUE = 'new-connection';

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
  const formKey = sessionKeys.generate.connect;
  const [defaultValues] = useSessionStorage<SingleTableConnectFormValues>(
    formKey,
    {
      fkSourceConnectionId: '',
      destination: {
        connectionId: '',
        destinationOptions: {},
      },
    }
  );
  const [defineFormValues] = useSessionStorage<DefineFormValues>(
    sessionKeys.global.define,
    { jobName: '' }
  );
  const [isSourceValidating, setIsSourceValidating] = useState<boolean>(false);

  const [sourceValidationResponse, setSourceValidationResponse] = useState<
    CheckConnectionConfigByIdResponse | undefined
  >();
  const [isDestinationValidating, setIsDestinationValidating] =
    useState<boolean>(false);

  const [destinationValidationResponse, setDestinationValidationResponse] =
    useState<CheckConnectionConfigByIdResponse | undefined>();

  const form = useForm({
    resolver: yupResolver(SingleTableConnectFormValues),
    defaultValues,
  });

  const { isLoading: isConnectionsLoading, data: connectionsData } = useQuery(
    ConnectionService.method.getConnections,
    { accountId: account?.id },
    { enabled: !!account?.id }
  );
  const connections = connectionsData?.connections ?? [];

  async function onSubmit(_values: SingleTableConnectFormValues) {
    router.push(
      `/${account?.name}/new/job/generate/single/schema?sessionId=${sessionPrefix}`
    );
  }

  const errors = form.formState.errors;

  const { mysql, postgres } = splitConnections(connections);

  const destOpts = form.watch('destination.destinationOptions');
  const shouldHideInitTableSchema = useShouldHideInitConnectionSchema(
    form.control
  );

  const { mutateAsync: checkConnectionConfig } = useMutation(
    ConnectionService.method.checkConnectionConfigById
  );

  const schemaConnectionName = connections.find(
    (c) => c.id === form.watch('fkSourceConnectionId')
  )?.name;
  const destinationName = connections.find(
    (c) => c.id === form.watch('destination.connectionId')
  )?.name;

  return (
    <NewJobWizard
      id="newjobflowcontainer"
      steps={getJobProgressSteps('generate-table')}
      stepLabels={getJobProgressStepLabels('generate-table')}
      stepName="connect"
      title="Where should the generated data go?"
      description="A connection gives the shape of the tables; the destination receives the synthetic rows. Both can be the same database."
    >
      <FormPersist formKey={formKey} form={form} />
      <Form {...form}>
        <form
          onSubmit={form.handleSubmit(onSubmit)}
          className="flex flex-1 flex-col gap-9"
        >
          <WizardSection
            id="generate-schema-connection"
            title="Table schema connection"
            description="The connection whose tables give the shape of the data to generate. It can be the destination itself."
          >
            <div className="flex flex-col gap-4">
              <FormField
                control={form.control}
                name="fkSourceConnectionId"
                render={({ field }) => (
                  <FormItem>
                    <FormControl>
                      {isConnectionsLoading ? (
                        <Skeleton className="w-full h-12 rounded-lg" />
                      ) : (
                        <ConnectionPicker
                          ariaLabel="Table schema connection"
                          postgres={postgres}
                          mysql={mysql}
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
                            if (value === NEW_CONNECTION_VALUE) {
                              const destId = form.getValues(
                                'destination.connectionId'
                              );

                              const urlParams = new URLSearchParams({
                                returnTo: `/${account?.name}/new/job/generate/single/connect?sessionId=${sessionPrefix}&from=new-connection`,
                              });
                              const connection = connections.find(
                                (c) => c.id === destId
                              );
                              const connType = getConnectionType(
                                connection?.connectionConfig ??
                                  create(ConnectionConfigSchema, {})
                              );

                              if (connType) {
                                urlParams.append('connectionType', connType);
                              }
                              if (
                                urlParams.getAll('connectionType').length === 0
                              ) {
                                urlParams.append('connectionType', 'pgConfig');
                                urlParams.append(
                                  'connectionType',
                                  'mysqlConfig'
                                );
                              }
                              router.push(
                                `/${account?.name}/new/connection?${urlParams.toString()}`
                              );
                              return;
                            }
                            field.onChange(value);
                            const destId = form.getValues(
                              'destination.connectionId'
                            );
                            if (!destId) {
                              form.setValue('destination.connectionId', value);
                              const destConnection = connections.find(
                                (c) => c.id === value
                              );
                              const destConnType = getConnectionType(
                                destConnection?.connectionConfig ??
                                  create(ConnectionConfigSchema, {})
                              );
                              const newOpts =
                                getDefaultDestinationFormValueOptionsFromConnectionCase(
                                  destConnType,
                                  () => new Set()
                                );
                              form.setValue(
                                'destination.destinationOptions',
                                newOpts,
                                {
                                  shouldDirty: true,
                                  shouldTouch: true,
                                  shouldValidate: true,
                                }
                              );
                            }

                            setIsSourceValidating(true);
                            try {
                              const res = await checkConnectionConfig({
                                id: form.getValues('fkSourceConnectionId'),
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
                          }}
                        />
                      )}
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>
          </WizardSection>
          <WizardSection
            id="generate-destination"
            title="Destination"
            description="Where the generated rows are written."
          >
            <div className="flex flex-col gap-4">
              <FormField
                control={form.control}
                name="destination.connectionId"
                render={({ field }) => (
                  <FormItem>
                    <FormControl>
                      {isConnectionsLoading ? (
                        <Skeleton className="w-full h-12 rounded-lg" />
                      ) : (
                        <ConnectionPicker
                          ariaLabel="Destination connection"
                          postgres={postgres}
                          mysql={mysql}
                          newConnectionValue={NEW_CONNECTION_VALUE}
                          isChecking={isDestinationValidating}
                          selectedDetail={
                            !isDestinationValidating && (
                              <TestConnectionBadge
                                validationResponse={
                                  !destinationValidationResponse
                                    ? sourceValidationResponse
                                    : destinationValidationResponse
                                }
                                connectionId={field.value}
                                accountName={account?.name ?? ''}
                              />
                            )
                          }
                          value={field.value}
                          onChange={async (value: string) => {
                            if (value === NEW_CONNECTION_VALUE) {
                              const urlParams = new URLSearchParams({
                                returnTo: `/${account?.name}/new/job/generate/single/connect?sessionId=${sessionPrefix}&from=new-connection`,
                              });
                              urlParams.append('connectionType', 'pgConfig');
                              urlParams.append('connectionType', 'mysqlConfig');
                              router.push(
                                `/${account?.name}/new/connection?${urlParams.toString()}`
                              );
                              return;
                            }
                            field.onChange(value);
                            const destConnection = connections.find(
                              (c) => c.id === value
                            );
                            const destConnType = getConnectionType(
                              destConnection?.connectionConfig ??
                                create(ConnectionConfigSchema, {})
                            );
                            const newOpts =
                              getDefaultDestinationFormValueOptionsFromConnectionCase(
                                destConnType,
                                () => new Set()
                              );
                            form.setValue(
                              'destination.destinationOptions',
                              newOpts,
                              {
                                shouldDirty: true,
                                shouldTouch: true,
                                shouldValidate: true,
                              }
                            );
                            setIsDestinationValidating(true);
                            try {
                              const res = await checkConnectionConfig({
                                id: form.getValues('destination.connectionId'),
                                scope: getServerScope(
                                  ConnectionRole.DESTINATION,
                                  defineFormValues.workflowSettings?.engine
                                ),
                              });
                              setDestinationValidationResponse(res);
                            } catch (err) {
                              if (
                                err instanceof ConnectError &&
                                err.code === Code.InvalidArgument &&
                                err.message.includes('unsupported operation')
                              ) {
                                setDestinationValidationResponse(undefined);
                              } else {
                                setDestinationValidationResponse(
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
                              setIsDestinationValidating(false);
                            }
                          }}
                        />
                      )}
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />

              {destinationName && (
                <WizardPanel title={`Before writing to ${destinationName}`}>
                  <DestinationOptionsForm
                    connection={connections.find(
                      (c) => c.id === form.getValues().destination.connectionId
                    )}
                    hideInitTableSchema={shouldHideInitTableSchema}
                    value={destOpts}
                    setValue={(newOpts) => {
                      form.setValue('destination.destinationOptions', newOpts, {
                        shouldDirty: true,
                        shouldTouch: true,
                        shouldValidate: true,
                      });
                    }}
                    hideDynamoDbTableMappings={true}
                    destinationDetailsRecord={{}} // not used because we are hiding dynamodb table mappings
                    errors={
                      form.formState.errors?.destination?.destinationOptions
                    }
                  />
                </WizardPanel>
              )}
            </div>
          </WizardSection>
          <WizardFooter
            summary={[
              defineFormValues.jobName,
              schemaConnectionName &&
                `${schemaConnectionName} → ${destinationName ?? '…'}`,
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
            <Button
              type="submit"
              disabled={(errors?.root?.message?.length ?? 0) > 0}
            >
              Continue
            </Button>
          </WizardFooter>
        </form>
      </Form>
    </NewJobWizard>
  );
}

function useShouldHideInitConnectionSchema(
  control: Control<SingleTableConnectFormValues>
): boolean {
  const [destinationConnectionid, fkSourceConnectionId] = useWatch({
    control,
    name: ['destination.connectionId', 'fkSourceConnectionId'],
  });
  return destinationConnectionid === fkSourceConnectionId;
}
