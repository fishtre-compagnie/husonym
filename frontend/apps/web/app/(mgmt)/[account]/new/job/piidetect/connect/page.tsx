'use client';
import FormPersist from '@/app/(mgmt)/FormPersist';
import { getNewJobSessionKeys } from '@/app/(mgmt)/[account]/jobs/util';
import {
  WizardFooter,
  WizardSection,
} from '@/components/progress-steps/Wizard';
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
import { useQuery } from '@connectrpc/connect-query';
import { yupResolver } from '@/util/yup-form-resolver';
import { ConnectionService } from '@husonym/sdk';
import { useRouter } from 'next/navigation';
import { ReactElement, use, useEffect } from 'react';
import { useForm } from 'react-hook-form';
import { useSessionStorage } from 'usehooks-ts';
import {
  getJobProgressStepLabels,
  getJobProgressSteps,
} from '../../JobsProgressSteps';
import NewJobWizard from '../../NewJobWizard';
import ConnectionPicker from '../../connect/ConnectionPicker';
import {
  DefineFormValues,
  PiiDetectionConnectFormValues,
} from '../../job-form-validations';

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
  const formKey = getNewJobSessionKeys(sessionPrefix).piidetect.connect;
  const [defaultValues] = useSessionStorage<PiiDetectionConnectFormValues>(
    formKey,
    {
      sourceId: '',
    }
  );

  const form = useForm<PiiDetectionConnectFormValues>({
    resolver: yupResolver(PiiDetectionConnectFormValues),
    defaultValues,
  });

  const { isLoading: isConnectionsLoading, data: connectionsData } = useQuery(
    ConnectionService.method.getConnections,
    { accountId: account?.id },
    { enabled: !!account?.id }
  );
  const connections = connectionsData?.connections ?? [];

  function onSubmit(_values: PiiDetectionConnectFormValues) {
    router.push(
      `/${account?.name}/new/job/piidetect/schema?sessionId=${sessionPrefix}`
    );
  }

  const { mysql, postgres, mssql } = splitConnections(connections);

  const [defineFormValues] = useSessionStorage<DefineFormValues>(
    getNewJobSessionKeys(sessionPrefix).global.define,
    { jobName: '' }
  );
  const sourceName = connections.find(
    (c) => c.id === form.watch('sourceId')
  )?.name;

  return (
    <NewJobWizard
      id="newjobflowcontainer"
      steps={getJobProgressSteps('pii-detection')}
      stepName="connect"
      stepLabels={getJobProgressStepLabels('pii-detection')}
      title="Which database should be scanned?"
      description="The job reads column names and a sample of rows to find personal data. Nothing is copied."
      mascot
    >
      <FormPersist formKey={formKey} form={form} />
      <Form {...form}>
        <form
          onSubmit={form.handleSubmit(onSubmit)}
          className="flex flex-1 flex-col gap-9"
        >
          <WizardSection
            id="piidetect-source"
            title="Source connection"
            description="PostgreSQL, MySQL and SQL Server connections can be scanned."
          >
            <FormField
              control={form.control}
              name="sourceId"
              render={({ field }) => (
                <FormItem>
                  <FormControl>
                    {isConnectionsLoading ? (
                      <Skeleton className="h-16 w-full rounded-[14px]" />
                    ) : (
                      <ConnectionPicker
                        ariaLabel="Connection to scan"
                        postgres={postgres}
                        mysql={mysql}
                        mssql={mssql}
                        newConnectionValue={NEW_CONNECTION_VALUE}
                        value={field.value}
                        onChange={(value: string) => {
                          if (!value) {
                            return;
                          }
                          if (value === NEW_CONNECTION_VALUE) {
                            const urlParams = new URLSearchParams({
                              returnTo: `/${account?.name}/new/job/piidetect/connect?sessionId=${sessionPrefix}&from=new-connection`,
                            });
                            urlParams.append('connectionType', 'pgConfig');
                            urlParams.append('connectionType', 'mysqlConfig');
                            urlParams.append('connectionType', 'mssqlConfig');
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

          <WizardFooter summary={[defineFormValues.jobName, sourceName]}>
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
            <Button type="submit">Continue</Button>
          </WizardFooter>
        </form>
      </Form>
    </NewJobWizard>
  );
}
