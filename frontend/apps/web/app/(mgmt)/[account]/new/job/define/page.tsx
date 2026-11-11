'use client';
import { Segmented } from '@/components/ds';
import {
  SelectCard,
  WizardFooter,
  WizardSection,
} from '@/components/progress-steps/Wizard';
import LicenseFeatureNotice from '@/components/LicenseFeatureNotice';
import { useAccount } from '@/components/providers/account-provider';
import { PageProps } from '@/components/types';
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
import { yupResolver } from '@/util/yup-form-resolver';
import { useRouter } from 'next/navigation';
import { ReactElement, use, useEffect, useState } from 'react';
import { useForm } from 'react-hook-form';
import { useSessionStorage } from 'usehooks-ts';
import {
  getJobProgressStepLabels,
  getJobProgressSteps,
} from '../JobsProgressSteps';
import NewJobWizard from '../NewJobWizard';
import { DefineFormValues, NewJobType } from '../job-form-validations';

import FormPersist from '@/app/(mgmt)/FormPersist';
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from '@/components/ui/accordion';
import { useLicenseFeature } from '@/libs/hooks/useLicense';
import { getSingleOrUndefined } from '@/libs/utils';
import { useMutation } from '@connectrpc/connect-query';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import ConsistencyScopeSelect from '@/components/jobs/Form/ConsistencyScopeSelect';
import { JobEngine, JobService } from '@husonym/sdk';
import { DEFAULT_CRON_STRING } from '../../../jobs/schedule';
import { getNewJobSessionKeys } from '../../../jobs/util';
import SyncActivityOptionsForm from './components/WorkflowSettings';

export default function Page(props: PageProps): ReactElement {
  const searchParams = use(props.searchParams);
  const router = useRouter();
  const { account } = useAccount();
  const { allowed: isSchedulingAllowed } = useLicenseFeature('scheduling');
  useEffect(() => {
    if (!searchParams?.sessionId) {
      router.push(`/${account?.name}/new/job`);
    }
  }, [searchParams?.sessionId]);

  const sessionPrefix = getSingleOrUndefined(searchParams?.sessionId) ?? '';
  const sessionKeys = getNewJobSessionKeys(sessionPrefix);

  const formKey = sessionKeys.global.define;
  const [defaultValues] = useSessionStorage<DefineFormValues>(formKey, {
    jobName: '',
    cronSchedule: '',
    initiateJobRun: false,
    workflowSettings: {},
    syncActivityOptions: {
      startToCloseTimeout: 10,
      retryPolicy: {
        maximumAttempts: 1,
      },
    },
  });
  const [isScheduleEnabled, setIsScheduleEnabled] = useState<boolean>(false);

  // run once to check if schedule should be enabled. (only want to run this a single time)
  useEffect(() => {
    if (defaultValues.cronSchedule && !isScheduleEnabled) {
      setIsScheduleEnabled(true);
    }
  }, []);

  const { mutateAsync: isJobNameAvailableAsync } = useMutation(
    JobService.method.isJobNameAvailable
  );
  const form = useForm({
    mode: 'onChange',
    resolver: yupResolver(DefineFormValues),
    defaultValues,
    context: {
      accountId: account?.id ?? '',
      isJobNameAvailable: isJobNameAvailableAsync,
    },
  });

  const newJobType = getNewJobType(getSingleOrUndefined(searchParams?.jobType));

  async function onSubmit(_values: DefineFormValues) {
    if (!isScheduleEnabled) {
      form.setValue('cronSchedule', '');
    }
    if (newJobType === 'generate-table') {
      router.push(
        `/${account?.name}/new/job/generate/single/connect?sessionId=${sessionPrefix}`
      );
    } else if (newJobType === 'ai-generate-table') {
      router.push(
        `/${account?.name}/new/job/aigenerate/single/connect?sessionId=${sessionPrefix}`
      );
    } else if (newJobType === 'pii-detection') {
      router.push(
        `/${account?.name}/new/job/piidetect/connect?sessionId=${sessionPrefix}`
      );
    } else {
      router.push(
        `/${account?.name}/new/job/connect?sessionId=${sessionPrefix}`
      );
    }
  }

  const jobName = form.watch('jobName');
  const jobNameState = form.getFieldState('jobName', form.formState);
  const isJobNameAvailable =
    !!jobName &&
    !jobNameState.invalid &&
    !form.formState.isValidating &&
    jobNameState.isDirty;

  return (
    <NewJobWizard
      id="newjobdefine"
      steps={getJobProgressSteps(newJobType)}
      stepLabels={getJobProgressStepLabels(newJobType)}
      stepName="define"
      title="Name the job"
      description="A unique name, then when it should run."
    >
      <FormPersist formKey={formKey} form={form} />
      <Form {...form}>
        <form
          onSubmit={form.handleSubmit(onSubmit)}
          className="flex flex-1 flex-col gap-7"
        >
          <FormField
            control={form.control}
            name="jobName"
            render={({ field }) => (
              <FormItem className="max-w-[420px]">
                <FormLabel>Job name</FormLabel>
                <FormControl>
                  <Input
                    placeholder="prod-to-stage"
                    className="h-11 font-mono"
                    autoComplete="off"
                    spellCheck={false}
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  Lowercase letters, numbers and hyphens.
                  {isJobNameAvailable && (
                    <span className="text-ok"> Available.</span>
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name="cronSchedule"
            render={({ field }) => (
              <FormItem>
                <WizardSection
                  id="define-schedule"
                  title="When should it run?"
                  description="Without a schedule, the job is paused and runs only when you trigger it; a default cron is stored."
                >
                  <div
                    role="radiogroup"
                    aria-labelledby="define-schedule"
                    className="grid grid-cols-[repeat(auto-fit,minmax(220px,1fr))] gap-2.5"
                  >
                    <SelectCard
                      selected={!isScheduleEnabled}
                      onClick={() => {
                        setIsScheduleEnabled(false);
                        form.resetField('cronSchedule', {
                          keepError: false,
                        });
                      }}
                      className="flex-col gap-1.5 px-4 py-3.5"
                    >
                      <span className="font-semibold">When I decide</span>
                      <span className="text-[13px] text-muted-foreground">
                        From the interface, the CLI or the API.
                      </span>
                    </SelectCard>
                    <SelectCard
                      selected={isScheduleEnabled}
                      // Turning a schedule off stays possible without the feature.
                      disabled={!isSchedulingAllowed && !isScheduleEnabled}
                      onClick={() => setIsScheduleEnabled(true)}
                      className="flex-col gap-1.5 px-4 py-3.5"
                    >
                      <span className="font-semibold">On a schedule</span>
                      <span className="text-[13px] text-muted-foreground">
                        A cron expression, such as {DEFAULT_CRON_STRING}.
                      </span>
                    </SelectCard>
                  </div>
                  <LicenseFeatureNotice feature="scheduling" />
                  {isScheduleEnabled && (
                    <div className="flex max-w-[420px] flex-col gap-2">
                      <FormLabel>Cron schedule</FormLabel>
                      <FormControl>
                        <Input
                          placeholder={DEFAULT_CRON_STRING}
                          className="font-mono"
                          {...field}
                          disabled={!isScheduleEnabled || !isSchedulingAllowed}
                        />
                      </FormControl>
                      <FormMessage />
                    </div>
                  )}
                </WizardSection>
              </FormItem>
            )}
          />
          <FormField
            name="initiateJobRun"
            render={({ field }) => (
              <FormItem>
                <WizardSection
                  id="define-initiate"
                  title="Once it is created"
                  description="Start a single run right after the job is created, or keep it for later."
                >
                  <FormControl>
                    <Segmented
                      ariaLabel="Initiate a job run after creation"
                      className="self-start"
                      value={field.value ? 'true' : 'false'}
                      onChange={(value) => field.onChange(value === 'true')}
                      options={[
                        { value: 'false', label: 'Create only' },
                        { value: 'true', label: 'Create and run' },
                      ]}
                    />
                  </FormControl>
                  <FormMessage />
                </WizardSection>
              </FormItem>
            )}
          />
          <Accordion
            type="single"
            collapsible
            className="w-full rounded-[14px] border border-line-soft bg-card px-[18px]"
          >
            <AccordionItem value="settings" className="border-b-0">
              <AccordionTrigger className="py-4 font-semibold hover:no-underline">
                Advanced settings
              </AccordionTrigger>
              <AccordionContent>
                <div className="flex flex-col gap-4 border-t border-line-soft pt-5">
                  <FormField
                    control={form.control}
                    name="workflowSettings.runTimeout"
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>Job Run Timeout</FormLabel>
                        <FormDescription>
                          The maximum length of time (in minutes) that a single
                          job run is allowed to span before it times out.{' '}
                          <code>0</code> means no overall timeout.
                        </FormDescription>
                        <FormControl>
                          <Input
                            type="number"
                            {...field}
                            value={field.value || 0}
                            onChange={(e) => {
                              field.onChange(e.target.valueAsNumber);
                            }}
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <FormField
                    control={form.control}
                    name="workflowSettings.engine"
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>Engine</FormLabel>
                        <FormDescription>
                          Transformation engine used to run this job.{' '}
                          <code>Default</code> follows the deployment setting.
                        </FormDescription>
                        <Select
                          onValueChange={(v) => field.onChange(Number(v))}
                          value={String(field.value ?? JobEngine.UNSPECIFIED)}
                        >
                          <FormControl>
                            <SelectTrigger>
                              <SelectValue />
                            </SelectTrigger>
                          </FormControl>
                          <SelectContent>
                            <SelectItem value={String(JobEngine.UNSPECIFIED)}>
                              Default (deployment)
                            </SelectItem>
                            <SelectItem value={String(JobEngine.ATHANOR)}>
                              Athanor
                            </SelectItem>
                            <SelectItem value={String(JobEngine.BENTHOS)}>
                              Benthos (legacy)
                            </SelectItem>
                          </SelectContent>
                        </Select>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <FormField
                    control={form.control}
                    name="workflowSettings.consistencyScope"
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>Consistency scope</FormLabel>
                        <FormDescription>
                          Deterministic consistency (Athanor engine only): the
                          same source value gets the same anonymized value
                          everywhere in this scope. Beyond a single run, outputs
                          stay linkable over time.
                        </FormDescription>
                        <FormControl>
                          <ConsistencyScopeSelect
                            value={field.value}
                            onChange={field.onChange}
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <SyncActivityOptionsForm
                    value={form.watch('syncActivityOptions') ?? {}}
                    setValue={(value) => {
                      form.setValue('syncActivityOptions', value);
                    }}
                    errors={form.formState.errors?.syncActivityOptions}
                    jobtype={newJobType}
                  />
                </div>
              </AccordionContent>
            </AccordionItem>
          </Accordion>
          <WizardFooter
            summary={[
              jobName,
              isScheduleEnabled
                ? `Scheduled: ${form.watch('cronSchedule') || DEFAULT_CRON_STRING}`
                : 'Run on demand',
            ]}
          >
            <Button
              variant="outline"
              type="reset"
              onClick={() => router.push(`/${account?.name}/new/job`)}
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

function getNewJobType(jobtype?: string): NewJobType {
  switch (jobtype) {
    case 'generate-table':
    case 'ai-generate-table':
    case 'data-sync':
    case 'pii-detection':
      return jobtype as NewJobType;
    default:
      return 'data-sync';
  }
}
