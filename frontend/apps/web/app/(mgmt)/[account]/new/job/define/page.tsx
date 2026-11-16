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
import { useTranslations } from 'next-intl';
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
  const t = useTranslations('newJob');
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
      title={t('define.title')}
      description={t('define.description')}
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
                <FormLabel>{t('define.jobName')}</FormLabel>
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
                  {t('define.jobNameHint')}
                  {isJobNameAvailable && (
                    <span className="text-ok"> {t('define.available')}</span>
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
                  title={t('define.schedule.title')}
                  description={t('define.schedule.description')}
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
                      <span className="font-semibold">
                        {t('define.schedule.onDemand')}
                      </span>
                      <span className="text-[13px] text-muted-foreground">
                        {t('define.schedule.onDemandHint')}
                      </span>
                    </SelectCard>
                    <SelectCard
                      selected={isScheduleEnabled}
                      // Turning a schedule off stays possible without the feature.
                      disabled={!isSchedulingAllowed && !isScheduleEnabled}
                      onClick={() => setIsScheduleEnabled(true)}
                      className="flex-col gap-1.5 px-4 py-3.5"
                    >
                      <span className="font-semibold">
                        {t('define.schedule.scheduled')}
                      </span>
                      <span className="text-[13px] text-muted-foreground">
                        {t('define.schedule.scheduledHint', {
                          cron: DEFAULT_CRON_STRING,
                        })}
                      </span>
                    </SelectCard>
                  </div>
                  <LicenseFeatureNotice feature="scheduling" />
                  {isScheduleEnabled && (
                    <div className="flex max-w-[420px] flex-col gap-2">
                      <FormLabel>{t('define.schedule.cron')}</FormLabel>
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
                  title={t('define.initiate.title')}
                  description={t('define.initiate.description')}
                >
                  <FormControl>
                    <Segmented
                      ariaLabel={t('define.initiate.ariaLabel')}
                      className="self-start"
                      value={field.value ? 'true' : 'false'}
                      onChange={(value) => field.onChange(value === 'true')}
                      options={[
                        {
                          value: 'false',
                          label: t('define.initiate.createOnly'),
                        },
                        {
                          value: 'true',
                          label: t('define.initiate.createAndRun'),
                        },
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
                {t('define.advanced.title')}
              </AccordionTrigger>
              <AccordionContent>
                <div className="flex flex-col gap-4 border-t border-line-soft pt-5">
                  <FormField
                    control={form.control}
                    name="workflowSettings.runTimeout"
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('define.advanced.runTimeout')}</FormLabel>
                        <FormDescription>
                          {t.rich('define.advanced.runTimeoutDescription', {
                            code: (chunks) => <code>{chunks}</code>,
                          })}
                          {newJobType !== 'pii-detection' &&
                            ` ${t('define.advanced.runTimeoutTables')}`}
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
                        <FormLabel>{t('define.advanced.engine')}</FormLabel>
                        <FormDescription>
                          {t.rich('define.advanced.engineDescription', {
                            code: (chunks) => <code>{chunks}</code>,
                          })}
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
                              {t('define.advanced.engineDefault')}
                            </SelectItem>
                            <SelectItem value={String(JobEngine.ATHANOR)}>
                              Athanor
                            </SelectItem>
                            <SelectItem value={String(JobEngine.BENTHOS)}>
                              {t('define.advanced.engineBenthos')}
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
                        <FormLabel>
                          {t('define.advanced.consistencyScope')}
                        </FormLabel>
                        <FormDescription>
                          {t('define.advanced.consistencyScopeDescription')}
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
                  {/* A sync table has no time limit of its own: it runs as long
                      as it makes progress, within the job run timeout. */}
                  {newJobType === 'pii-detection' && (
                    <SyncActivityOptionsForm
                      value={form.watch('syncActivityOptions') ?? {}}
                      setValue={(value) => {
                        form.setValue('syncActivityOptions', value);
                      }}
                      errors={form.formState.errors?.syncActivityOptions}
                      jobtype={newJobType}
                    />
                  )}
                </div>
              </AccordionContent>
            </AccordionItem>
          </Accordion>
          <WizardFooter
            summary={[
              jobName,
              isScheduleEnabled
                ? t('define.summary.scheduled', {
                    cron: form.watch('cronSchedule') || DEFAULT_CRON_STRING,
                  })
                : t('define.summary.onDemand'),
            ]}
          >
            <Button
              variant="outline"
              type="reset"
              onClick={() => router.push(`/${account?.name}/new/job`)}
            >
              {t('wizard.back')}
            </Button>
            <Button type="submit" disabled={!form.formState.isValid}>
              {t('wizard.continue')}
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
