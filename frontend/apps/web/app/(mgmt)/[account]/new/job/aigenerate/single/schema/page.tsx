'use client';

import { useCheckedSave } from '@/components/connections/checks/useCheckedSave';
import FormPersist from '@/app/(mgmt)/FormPersist';
import {
  clearNewJobSession,
  getCreateNewSingleTableAiGenerateJobRequest,
  getNewJobSessionKeys,
  getSampleAiGeneratedRecordsRequest,
} from '@/app/(mgmt)/[account]/jobs/util';
import ButtonText from '@/components/ButtonText';
import { Action } from '@/components/DualListBox/DualListBox';
import Spinner from '@/components/Spinner';
import { WizardFooter } from '@/components/progress-steps/Wizard';
import {
  AiSchemaTable,
  AiSchemaTableRecord,
} from '@/components/jobs/SchemaTable/AiSchemaTable';
import { getSchemaConstraintHandler } from '@/components/jobs/SchemaTable/schema-constraint-handler';
import { useAccount } from '@/components/providers/account-provider';
import { AppTableFeatures } from '@/components/table/features';
import { PageProps } from '@/components/types';
import { Alert, AlertTitle } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form';
import { Input } from '@/components/ui/input';
import { Textarea } from '@/components/ui/textarea';
import { getSingleOrUndefined } from '@/libs/utils';
import { getErrorMessage } from '@/util/util';
import { useMutation, useQuery } from '@connectrpc/connect-query';
import { yupResolver } from '@/util/yup-form-resolver';
import {
  ConnectionDataService,
  ConnectionService,
  JobService,
} from '@husonym/sdk';
import { ExclamationTriangleIcon } from '@radix-ui/react-icons';
import { ColumnDef } from '@tanstack/react-table';
import { useTranslations } from 'next-intl';
import { useRouter } from 'next/navigation';
import { ReactElement, use, useEffect, useMemo, useState } from 'react';
import { useForm } from 'react-hook-form';
import { toast } from 'sonner';
import { useSessionStorage } from 'usehooks-ts';
import {
  getJobProgressStepLabels,
  getJobProgressSteps,
} from '../../../JobsProgressSteps';
import NewJobWizard from '../../../NewJobWizard';
import {
  DefineFormValues,
  SingleTableAiConnectFormValues,
  SingleTableAiSchemaFormValues,
} from '../../../job-form-validations';
import SampleTable from './SampleTable/SampleTable';
import { getAiSampleTableColumns } from './SampleTable/SampleTableColumns';
import SelectModelNames from './SelectModelNames';
import { SampleRecord } from './types';

export default function Page(props: PageProps): ReactElement {
  const searchParams = use(props.searchParams);
  const { account } = useAccount();
  const router = useRouter();
  const t = useTranslations('newJob');
  const [aioutput, setaioutput] = useState<SampleRecord[]>([]);

  useEffect(() => {
    if (!searchParams?.sessionId) {
      router.push(`/${account?.name}/new/job`);
    }
  }, [searchParams?.sessionId]);
  const { data: connectionsData } = useQuery(
    ConnectionService.method.getConnections,
    { accountId: account?.id },
    { enabled: !!account?.id }
  );
  const connections = connectionsData?.connections ?? [];

  const { mutateAsync: createJobAsync } = useMutation(
    JobService.method.createJob
  );
  const { checkThenSave, dialog: checksDialog } = useCheckedSave();
  const { mutateAsync: sampleRecords } = useMutation(
    ConnectionDataService.method.getAiGeneratedData
  );

  const sessionPrefix = getSingleOrUndefined(searchParams?.sessionId) ?? '';
  const sessionKeys = getNewJobSessionKeys(sessionPrefix);

  // Used to complete the whole form
  const defineFormKey = sessionKeys.global.define;
  const [defineFormValues] = useSessionStorage<DefineFormValues>(
    defineFormKey,
    { jobName: '' }
  );
  const connectFormKey = sessionKeys.aigenerate.connect;
  const [connectFormValues] = useSessionStorage<SingleTableAiConnectFormValues>(
    connectFormKey,
    {
      sourceId: '',
      fkSourceConnectionId: '',
      destination: {
        connectionId: '',
        destinationOptions: {},
      },
    }
  );

  const {
    data: connectionSchemaDataMap,
    isLoading: isSchemaMapLoading,
    isFetching: isSchemaMapValidating,
  } = useQuery(
    ConnectionDataService.method.getConnectionSchemaMap,
    { connectionId: connectFormValues.fkSourceConnectionId },
    { enabled: !!connectFormValues.fkSourceConnectionId }
  );

  const formKey = sessionKeys.aigenerate.schema;
  const [schemaFormData] = useSessionStorage<SingleTableAiSchemaFormValues>(
    formKey,
    {
      numRows: 10,
      generateBatchSize: 10,
      model: 'gpt-3.5-turbo',
      userPrompt: '',
      schema: '',
      table: '',
    }
  );

  const form = useForm<SingleTableAiSchemaFormValues>({
    resolver: yupResolver(SingleTableAiSchemaFormValues),
    values: schemaFormData,
  });

  const [isClient, setIsClient] = useState(false);
  useEffect(() => {
    setIsClient(true);
  }, []);

  const [isSampling, setIsSampling] = useState(false);

  async function onSubmit(values: SingleTableAiSchemaFormValues) {
    if (!account) {
      return;
    }
    const connMap = new Map(connections.map((c) => [c.id, c]));
    const request = getCreateNewSingleTableAiGenerateJobRequest(
      {
        define: defineFormValues,
        connect: connectFormValues,
        schema: values,
      },
      account.id,
      (id) => connMap.get(id)
    );
    await checkThenSave(request, async () => {
      try {
        const job = await createJobAsync(request);
        toast.success(t('toasts.created'));

        clearNewJobSession(window.sessionStorage, sessionPrefix);

        if (job.job?.id) {
          router.push(`/${account?.name}/jobs/${job.job.id}`);
        } else {
          router.push(`/${account?.name}/jobs`);
        }
      } catch (err) {
        console.error(err);
        toast.error(t('toasts.createFailedBang'), {
          description: getErrorMessage(err),
        });
      }
    });
  }

  const { data: tableConstraints, isFetching: isTableConstraintsValidating } =
    useQuery(
      ConnectionDataService.method.getConnectionTableConstraints,
      { connectionId: connectFormValues.fkSourceConnectionId },
      { enabled: !!connectFormValues.fkSourceConnectionId }
    );

  const schemaConstraintHandler = useMemo(
    () =>
      getSchemaConstraintHandler(
        connectionSchemaDataMap?.schemaMap ?? {},
        tableConstraints?.primaryKeyConstraints ?? {},
        tableConstraints?.foreignKeyConstraints ?? {},
        tableConstraints?.uniqueConstraints ?? {},
        []
      ),
    [isSchemaMapValidating, isTableConstraintsValidating]
  );
  const [selectedTables, setSelectedTables] = useState<Set<string>>(new Set());

  useEffect(() => {
    if (isSchemaMapLoading || selectedTables.size > 0) {
      return;
    }
    const js = schemaFormData;
    if (js.schema && js.table) {
      onSelectedTableToggle(new Set([`${js.schema}.${js.table}`]), 'add');
    }
  }, [isSchemaMapLoading]);

  async function onSampleClick(): Promise<void> {
    if (!account?.id || isSampling) {
      return;
    }
    try {
      setIsSampling(true);
      const output = await sampleRecords(
        getSampleAiGeneratedRecordsRequest({
          connect: connectFormValues,
          schema: form.getValues(),
        })
      );
      setaioutput(output.records);
    } catch (err) {
      toast.error(t('aiGenerate.schema.sampleFailed'), {
        description: getErrorMessage(err),
      });
    } finally {
      setIsSampling(false);
    }
  }
  function onSelectedTableToggle(items: Set<string>, action: Action): void {
    if (items.size === 0) {
      setSelectedTables(new Set());
      form.setValue('schema', '');
      form.setValue('table', '');
      return;
    }
    if (action === 'add') {
      setSelectedTables(items);
      const item = Array.from(items)[0];
      const [schema, table] = item.split('.');
      form.setValue('schema', schema);
      form.setValue('table', table);
    } else {
      setSelectedTables(new Set());
      form.setValue('schema', '');
      form.setValue('table', '');
    }
  }

  const [formSchema, formTable] = form.watch(['schema', 'table']);

  const [tableData, columns] = useMemo(() => {
    const tdata: AiSchemaTableRecord[] = [];
    const cols: ColumnDef<AppTableFeatures, SampleRecord>[] = [];
    if (formSchema && formTable && connectionSchemaDataMap?.schemaMap) {
      const tableSchema =
        connectionSchemaDataMap.schemaMap[`${formSchema}.${formTable}`];
      if (tableSchema) {
        tdata.push(...tableSchema.schemas);
        cols.push(
          ...getAiSampleTableColumns(
            tableSchema.schemas.map((dbcol) => dbcol.column)
          )
        );
      }
    }
    return [tdata, cols];
  }, [formSchema, formTable, isSchemaMapValidating]);

  return (
    <NewJobWizard
      steps={getJobProgressSteps('ai-generate-table')}
      stepLabels={getJobProgressStepLabels('ai-generate-table')}
      stepName="schema"
      title={t('aiGenerate.schema.title')}
      description={t('aiGenerate.schema.description')}
      wide
    >
      <FormPersist formKey={formKey} form={form} />
      {checksDialog}
      <Form {...form}>
        <form
          onSubmit={form.handleSubmit(onSubmit)}
          className="flex flex-1 flex-col gap-8"
        >
          {isClient && (
            <AiSchemaTable
              data={tableData}
              constraintHandler={schemaConstraintHandler}
              schema={connectionSchemaDataMap?.schemaMap ?? {}}
              isSchemaDataReloading={isSchemaMapValidating}
              selectedTables={selectedTables}
              onSelectedTableToggle={onSelectedTableToggle}
            />
          )}

          <FormField
            control={form.control}
            name="model"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('aiGenerate.schema.model')}</FormLabel>
                <div className="flex flex-col md:flex-row gap-2 md:items-center">
                  <FormDescription>
                    {t('aiGenerate.schema.modelDescription')}
                  </FormDescription>
                  <div>
                    <SelectModelNames
                      onSelected={(modelName) => field.onChange(modelName)}
                    />
                  </div>
                </div>
                <FormControl>
                  <Input {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="userPrompt"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('aiGenerate.schema.prompt')}</FormLabel>
                <FormDescription>
                  {t('aiGenerate.schema.promptDescription')}
                </FormDescription>
                <FormControl>
                  <Textarea
                    {...field}
                    autoComplete="off"
                    autoCapitalize="off"
                    autoCorrect="off"
                    spellCheck={false}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="numRows"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('generate.schema.numRows')}</FormLabel>
                <FormDescription>
                  {t('generate.schema.numRowsDescription')}
                </FormDescription>
                <FormControl>
                  <Input
                    {...field}
                    type="number"
                    onChange={(e) => {
                      const numberValue = e.target.valueAsNumber;
                      if (!isNaN(numberValue)) {
                        field.onChange(numberValue);
                      }
                    }}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="generateBatchSize"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('aiGenerate.schema.batchSize')}</FormLabel>
                <FormDescription>
                  {t('aiGenerate.schema.batchSizeDescription')}
                </FormDescription>
                <FormControl>
                  <Input
                    {...field}
                    type="number"
                    onChange={(e) => {
                      const numberValue = e.target.valueAsNumber;
                      if (!isNaN(numberValue)) {
                        field.onChange(numberValue);
                      }
                    }}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          {form.formState.errors.root && (
            <Alert variant="destructive">
              <AlertTitle className="flex flex-row space-x-2 justify-center">
                <ExclamationTriangleIcon />
                <p>{t('formErrors')}</p>
              </AlertTitle>
            </Alert>
          )}
          <SampleTable columns={columns} records={aioutput} />

          <WizardFooter
            summary={[
              form.watch('model'),
              t('wizard.rows', { count: form.watch('numRows') ?? 0 }),
            ]}
          >
            <Button
              key="back"
              type="button"
              variant="outline"
              onClick={() => router.back()}
            >
              {t('wizard.back')}
            </Button>
            <Button
              type="button"
              variant="outline"
              onClick={() => onSampleClick()}
            >
              <ButtonText
                leftIcon={isSampling ? <Spinner /> : undefined}
                text={t('aiGenerate.schema.sample')}
              />
            </Button>
            <Button key="submit" type="submit">
              {t('wizard.createJob')}
            </Button>
          </WizardFooter>
        </form>
      </Form>
    </NewJobWizard>
  );
}
