'use client';
import { useCheckedSave } from '@/components/connections/checks/useCheckedSave';
import { useAccount } from '@/components/providers/account-provider';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardFooter } from '@/components/ui/card';
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form';
import ConsistencyScopeSelect from '@/components/jobs/Form/ConsistencyScopeSelect';
import { Input } from '@/components/ui/input';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { convertNanosecondsToMinutes, getErrorMessage } from '@/util/util';
import { useMutation } from '@connectrpc/connect-query';
import { yupResolver } from '@/util/yup-form-resolver';
import { ConsistencyScope, Job, JobEngine, JobService } from '@husonym/sdk';
import { useTranslations } from 'next-intl';
import { ReactElement } from 'react';
import { useForm } from 'react-hook-form';
import { toast } from 'sonner';
import { WorkflowSettingsSchema } from '../../../new/job/job-form-validations';
import { toWorkflowOptions } from '../../util';
import { isPiiDetectJob } from '../util';

interface Props {
  job: Job;
  mutate: (newjob: Job) => void;
}

export default function WorkflowSettingsCard({
  job,
  mutate,
}: Props): ReactElement {
  const t = useTranslations('jobs.overview.workflow');
  const form = useForm({
    mode: 'onChange',
    resolver: yupResolver(WorkflowSettingsSchema),
    values: {
      runTimeout: job?.workflowOptions?.runTimeout
        ? convertNanosecondsToMinutes(job.workflowOptions.runTimeout)
        : 0,
      engine: job?.workflowOptions?.engine ?? JobEngine.UNSPECIFIED,
      consistencyScope:
        job?.workflowOptions?.consistencyScope ?? ConsistencyScope.UNSPECIFIED,
    },
  });
  const { account } = useAccount();
  const { mutateAsync: updateJobWorkflowOptions } = useMutation(
    JobService.method.setJobWorkflowOptions
  );
  const { checkThenSave, dialog: checksDialog } = useCheckedSave();

  async function onSubmit(values: WorkflowSettingsSchema) {
    if (!account?.id) {
      return;
    }
    const engine = values.engine ?? JobEngine.UNSPECIFIED;
    if (engine === (job.workflowOptions?.engine ?? JobEngine.UNSPECIFIED)) {
      await saveWorkflowOptions(values);
      return;
    }
    // What the engine changes is asked of the destination servers as a whole (suspending
    // foreign keys): they are checked with the new engine, and without the tables, so that a
    // finding the engine does not change keeps no one from changing it. The source reads the
    // same way under both.
    await checkThenSave(
      { ...job, workflowOptions: { engine } },
      () => saveWorkflowOptions(values),
      { checkSource: false, serverOnly: true }
    );
  }

  async function saveWorkflowOptions(
    values: WorkflowSettingsSchema
  ): Promise<void> {
    try {
      const resp = await updateJobWorkflowOptions({
        id: job.id,
        worfklowOptions: toWorkflowOptions(values),
      });
      toast.success(t('updated'));
      if (resp.job) {
        mutate(resp.job);
      }
    } catch (err) {
      console.error(err);
      toast.error(t('updateError'), {
        description: getErrorMessage(err),
      });
    }
  }

  return (
    <Card className="overflow-hidden">
      <Form {...form}>
        {checksDialog}
        <form onSubmit={form.handleSubmit(onSubmit)}>
          <CardContent>
            <FormField
              control={form.control}
              name="runTimeout"
              render={({ field }) => (
                <FormItem className="pt-4">
                  <FormLabel>{t('runTimeout')}</FormLabel>
                  <FormDescription>
                    {t.rich('runTimeoutDescription', {
                      code: (chunks) => <code>{chunks}</code>,
                    })}
                    {!isPiiDetectJob(job) && ` ${t('runTimeoutTables')}`}
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
              name="engine"
              render={({ field }) => (
                <FormItem className="pt-4">
                  <FormLabel>{t('engine')}</FormLabel>
                  <FormDescription>
                    {t.rich('engineDescription', {
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
                        {t('engineDefault')}
                      </SelectItem>
                      <SelectItem value={String(JobEngine.ATHANOR)}>
                        Athanor
                      </SelectItem>
                      <SelectItem value={String(JobEngine.BENTHOS)}>
                        {t('engineBenthos')}
                      </SelectItem>
                    </SelectContent>
                  </Select>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="consistencyScope"
              render={({ field }) => (
                <FormItem className="pt-4">
                  <FormLabel>{t('consistencyScope')}</FormLabel>
                  <FormDescription>
                    {t('consistencyScopeDescription')}
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
          </CardContent>
          <CardFooter className="flex justify-center border-t border-line-soft bg-surface-2 py-2">
            <div className="flex flex-row items-center justify-end w-full">
              <Button type="submit" disabled={!form.formState.isValid}>
                {t('save')}
              </Button>
            </div>
          </CardFooter>
        </form>
      </Form>
    </Card>
  );
}
