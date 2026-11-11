'use client';
import { getErrorMessage } from '@/util/util';
import { useCheckedSave } from '@/components/connections/checks/useCheckedSave';
import {
  clearNewJobSession,
  getCreateNewPiiDetectJobRequest,
  getNewJobSessionKeys,
} from '@/app/(mgmt)/[account]/jobs/util';
import ButtonText from '@/components/ButtonText';
import { WizardFooter, WizardPanel } from '@/components/progress-steps/Wizard';
import { useAccount } from '@/components/providers/account-provider';
import Spinner from '@/components/Spinner';
import { PageProps } from '@/components/types';
import { Button } from '@/components/ui/button';
import { getSingleOrUndefined } from '@/libs/utils';
import { useMutation, useQuery } from '@connectrpc/connect-query';
import { Connection, ConnectionService, JobService } from '@husonym/sdk';
import { useRouter } from 'next/navigation';
import { FormEvent, ReactElement, use, useEffect } from 'react';
import { toast } from 'sonner';
import { useSessionStorage } from 'usehooks-ts';
import { ValidationError } from 'yup';
import {
  DefineFormValues,
  PiiDetectionConnectFormValues,
  PiiDetectionSchemaFormValues,
} from '../../job-form-validations';
import {
  getJobProgressStepLabels,
  getJobProgressSteps,
} from '../../JobsProgressSteps';
import NewJobWizard from '../../NewJobWizard';
import {
  DataSampling,
  Incremental,
  TableScanFilterMode,
  TableScanFilterPatterns,
  UserPrompt,
} from './FormInputs';
import {
  setPiiDetectionSchemaStorePersistenceKey,
  usePiiDetectionSchemaStore,
} from './stores';

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

  // Used to complete the whole form
  const defineFormKey = sessionKeys.global.define;
  const [defineFormValues] = useSessionStorage<DefineFormValues>(
    defineFormKey,
    { jobName: '' }
  );

  const connectFormKey = sessionKeys.piidetect.connect;
  const [connectFormValues] = useSessionStorage<PiiDetectionConnectFormValues>(
    connectFormKey,
    {
      sourceId: '',
    }
  );

  const { data: connectionsData } = useQuery(
    ConnectionService.method.getConnections,
    { accountId: account?.id },
    { enabled: !!account?.id }
  );
  const connections = connectionsData?.connections ?? [];
  const connectionsRecord = connections.reduce(
    (record, conn) => {
      record[conn.id] = conn;
      return record;
    },
    {} as Record<string, Connection | undefined>
  );

  const { mutateAsync: createJob } = useMutation(JobService.method.createJob);
  const { checkThenSave, dialog: checksDialog } = useCheckedSave();

  const {
    formData,
    setFormData,
    errors,
    setErrors,
    isSubmitting,
    setSubmitting,
    resetForm,
  } = usePiiDetectionSchemaStore();

  useEffect(() => {
    setPiiDetectionSchemaStorePersistenceKey(sessionKeys.piidetect.schema);
  }, [sessionKeys.piidetect.schema]);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (isSubmitting) {
      return;
    }

    try {
      setSubmitting(true);
      setErrors({});

      const validatedData = await PiiDetectionSchemaFormValues.validate(
        formData,
        {
          abortEarly: false,
        }
      );

      const request = getCreateNewPiiDetectJobRequest(
        {
          define: defineFormValues,
          connect: connectFormValues,
          schema: validatedData,
        },
        account?.id ?? '',
        (id) => connectionsRecord[id]
      );
      await checkThenSave(request, async () => {
        try {
          const job = await createJob(request);
          toast.success('Successfully created job!');

          resetForm();
          clearNewJobSession(window.sessionStorage, sessionPrefix);

          if (job.job?.id) {
            router.push(`/${account?.name}/jobs/${job.job.id}`);
          } else {
            router.push(`/${account?.name}/jobs`);
          }
        } catch (err) {
          toast.error('Unable to create job', {
            description: getErrorMessage(err),
          });
        }
      });
    } catch (err) {
      if (err instanceof ValidationError) {
        const validationErrors: Record<string, string> = {};
        err.inner.forEach((error) => {
          if (error.path) {
            validationErrors[error.path] = error.message;
          }
        });
        setErrors(validationErrors);
      }
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <NewJobWizard
      steps={getJobProgressSteps('pii-detection')}
      stepLabels={getJobProgressStepLabels('pii-detection')}
      stepName="schema"
      title="How should the scan run?"
      description="What to look for, how many rows to sample, and which tables to read. The analysis runs where your data is."
      mascot
    >
      {checksDialog}
      <form onSubmit={onSubmit} className="flex flex-1 flex-col gap-4">
        <WizardPanel>
          <UserPrompt
            value={formData.userPrompt ?? ''}
            onChange={(value) =>
              setFormData({ ...formData, userPrompt: value })
            }
            error={errors['userPrompt']}
          />
        </WizardPanel>
        <WizardPanel>
          <DataSampling
            value={formData.dataSampling}
            onChange={(value) =>
              setFormData({ ...formData, dataSampling: value })
            }
            errors={errors}
          />
        </WizardPanel>
        <WizardPanel>
          <Incremental
            value={formData.incremental}
            onChange={(value) =>
              setFormData({ ...formData, incremental: value })
            }
            errors={errors}
          />
        </WizardPanel>
        <WizardPanel className="[&>div]:flex [&>div]:flex-col [&>div]:gap-6">
          <TableScanFilterMode
            value={formData.tableScanFilter.mode}
            onChange={(value) =>
              setFormData({
                ...formData,
                tableScanFilter: { ...formData.tableScanFilter, mode: value },
              })
            }
            error={errors['tableScanFilter.mode']}
          />

          <TableScanFilterPatterns
            value={formData.tableScanFilter.patterns}
            onChange={(value) =>
              setFormData({
                ...formData,
                tableScanFilter: {
                  ...formData.tableScanFilter,
                  patterns: value,
                },
              })
            }
            connectionId={connectFormValues.sourceId}
            errors={errors}
            mode={formData.tableScanFilter.mode}
          />
        </WizardPanel>
        <WizardFooter
          className="mt-auto"
          summary={[
            defineFormValues.jobName,
            connectionsRecord[connectFormValues.sourceId]?.name,
          ]}
        >
          <Button
            key="back"
            type="button"
            variant="outline"
            onClick={() => router.back()}
          >
            Back
          </Button>
          <Button type="submit" disabled={isSubmitting}>
            <ButtonText
              leftIcon={isSubmitting ? <Spinner /> : undefined}
              text="Create job"
            />
          </Button>
        </WizardFooter>
      </form>
    </NewJobWizard>
  );
}
