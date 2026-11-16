'use client';
import FormPersist from '@/app/(mgmt)/FormPersist';
import { getConnectionType } from '@/app/(mgmt)/[account]/connections/util';
import {
  getDefaultDestinationFormValueOptionsFromConnectionCase,
  getNewJobSessionKeys,
} from '@/app/(mgmt)/[account]/jobs/util';
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
import { useQuery } from '@connectrpc/connect-query';
import { yupResolver } from '@/util/yup-form-resolver';
import { ConnectionConfigSchema, ConnectionService } from '@husonym/sdk';
import { useTranslations } from 'next-intl';
import { useRouter } from 'next/navigation';
import { ReactElement, use, useEffect } from 'react';
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
  SingleTableAiConnectFormValues,
} from '../../../job-form-validations';

const NEW_CONNECTION_VALUE = 'new-connection';

export default function Page(props: PageProps): ReactElement {
  const searchParams = use(props.searchParams);
  const { account } = useAccount();
  const router = useRouter();
  const t = useTranslations('newJob');
  useEffect(() => {
    if (!searchParams?.sessionId) {
      router.push(`/${account?.name}/new/job`);
    }
  }, [searchParams?.sessionId]);

  const sessionPrefix = getSingleOrUndefined(searchParams?.sessionId) ?? '';
  const formKey = getNewJobSessionKeys(sessionPrefix).aigenerate.connect;
  const [defaultValues] = useSessionStorage<SingleTableAiConnectFormValues>(
    formKey,
    {
      sourceId: '',
      fkSourceConnectionId: '',
      destination: {
        connectionId: '',
        destinationOptions: {},
      },
    }
  );

  const form = useForm<SingleTableAiConnectFormValues>({
    resolver: yupResolver(SingleTableAiConnectFormValues),
    defaultValues,
  });

  const { isLoading: isConnectionsLoading, data: connectionsData } = useQuery(
    ConnectionService.method.getConnections,
    { accountId: account?.id },
    { enabled: !!account?.id }
  );
  const connections = connectionsData?.connections ?? [];

  function onSubmit(_values: SingleTableAiConnectFormValues) {
    router.push(
      `/${account?.name}/new/job/aigenerate/single/schema?sessionId=${sessionPrefix}`
    );
  }

  const { mysql, postgres, openai } = splitConnections(connections);

  const destOpts = form.watch('destination.destinationOptions');
  const shouldHideInitTableSchema = useShouldHideInitConnectionSchema(
    form.control
  );

  const [defineFormValues] = useSessionStorage<DefineFormValues>(
    getNewJobSessionKeys(sessionPrefix).global.define,
    { jobName: '' }
  );
  const nameOf = (id: string | undefined) =>
    connections.find((c) => c.id === id)?.name;
  const modelName = nameOf(form.watch('sourceId'));
  const destinationName = nameOf(form.watch('destination.connectionId'));

  return (
    <NewJobWizard
      id="newjobflowcontainer"
      steps={getJobProgressSteps('ai-generate-table')}
      stepLabels={getJobProgressStepLabels('ai-generate-table')}
      stepName="connect"
      title={t('aiGenerate.connect.title')}
      description={t('aiGenerate.connect.description')}
    >
      <FormPersist formKey={formKey} form={form} />
      <Form {...form}>
        <form
          onSubmit={form.handleSubmit(onSubmit)}
          className="flex flex-1 flex-col gap-9"
        >
          <WizardSection
            id="aigenerate-source"
            title={t('aiGenerate.connect.model.title')}
            description={t('aiGenerate.connect.model.description')}
          >
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
                        ariaLabel={t('aiGenerate.connect.model.title')}
                        openai={openai}
                        newConnectionValue={NEW_CONNECTION_VALUE}
                        value={field.value}
                        onChange={(value: string) => {
                          if (!value) {
                            return;
                          }
                          if (value === NEW_CONNECTION_VALUE) {
                            const urlParams = new URLSearchParams({
                              returnTo: `/${account?.name}/new/job/aigenerate/single/connect?sessionId=${sessionPrefix}&from=new-connection`,
                              connectionType: 'openaiConfig',
                            });
                            router.push(
                              `/${account?.name}/new/connection?${urlParams.toString()}`
                            );
                            return;
                          }
                          field.onChange(value);
                        }}
                      />
                    )}
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </WizardSection>
          <WizardSection
            id="aigenerate-schema-connection"
            title={t('generate.connect.schemaConnection.title')}
            description={t('aiGenerate.connect.schemaConnectionDescription')}
          >
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
                        ariaLabel={t('generate.connect.schemaConnection.title')}
                        postgres={postgres}
                        mysql={mysql}
                        newConnectionValue={NEW_CONNECTION_VALUE}
                        value={field.value}
                        onChange={(value: string) => {
                          if (value === NEW_CONNECTION_VALUE) {
                            const destId = form.getValues(
                              'destination.connectionId'
                            );

                            const urlParams = new URLSearchParams({
                              returnTo: `/${account?.name}/new/job/aigenerate/single/connect?sessionId=${sessionPrefix}&from=new-connection`,
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
                              urlParams.append('connectionType', 'mysqlConfig');
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
                        }}
                      />
                    )}
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </WizardSection>
          <WizardSection
            id="aigenerate-destination"
            title={t('generate.connect.destination.title')}
            description={t('generate.connect.destination.description')}
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
                          ariaLabel={t(
                            'generate.connect.destination.ariaLabel'
                          )}
                          postgres={postgres}
                          mysql={mysql}
                          newConnectionValue={NEW_CONNECTION_VALUE}
                          value={field.value}
                          onChange={(value: string) => {
                            if (value === NEW_CONNECTION_VALUE) {
                              const fkSourceConnId = form.getValues(
                                'fkSourceConnectionId'
                              );

                              const urlParams = new URLSearchParams({
                                returnTo: `/${account?.name}/new/job/aigenerate/single/connect?sessionId=${sessionPrefix}&from=new-connection`,
                              });
                              const connection = connections.find(
                                (c) => c.id === fkSourceConnId
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
                          }}
                        />
                      )}
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />

              {destinationName && (
                <WizardPanel
                  title={t('connect.destinations.beforeWriting', {
                    name: destinationName ?? '',
                  })}
                >
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
              modelName,
              destinationName && `→ ${destinationName}`,
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
              {t('wizard.back')}
            </Button>
            <Button type="submit">{t('wizard.continue')}</Button>
          </WizardFooter>
        </form>
      </Form>
    </NewJobWizard>
  );
}

function useShouldHideInitConnectionSchema(
  control: Control<SingleTableAiConnectFormValues>
): boolean {
  const [destinationConnectionid, fkSourceConnectionId] = useWatch({
    control,
    name: ['destination.connectionId', 'fkSourceConnectionId'],
  });
  return destinationConnectionid === fkSourceConnectionId;
}
