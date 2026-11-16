'use client';
import LicenseFeatureNotice from '@/components/LicenseFeatureNotice';
import { Button } from '@/components/ui/button';
import {
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormMessage,
} from '@/components/ui/form';
import { Input } from '@/components/ui/input';
import { useLicenseFeature } from '@/libs/hooks/useLicense';
import { getErrorMessage } from '@/util/util';
import { useMutation } from '@connectrpc/connect-query';
import { yupResolver } from '@/util/yup-form-resolver';
import { Job, JobService } from '@husonym/sdk';
import cron from 'cron-validate';
import { useTranslations } from 'next-intl';
import { ReactElement, useMemo } from 'react';
import { useForm } from 'react-hook-form';
import { toast } from 'sonner';
import * as Yup from 'yup';

function scheduleFormSchema(invalidCron: string) {
  return Yup.object({
    cronSchedule: Yup.string()
      .optional()
      .test('isValidCron', invalidCron, (value, context) => {
        if (!value) {
          return false;
        }
        const output = cron(value);
        if (output.isValid()) {
          return true;
        }
        if (output.isError()) {
          const errors = output.getError();
          if (errors.length > 0) {
            return context.createError({ message: errors.join(', ') });
          }
        }
        return output.isValid();
      }),
  });
}

type ScheduleFormValues = Yup.InferType<ReturnType<typeof scheduleFormSchema>>;

interface Props {
  job: Job;
  mutate: (newjob: Job) => void;
}

export default function JobScheduleCard({ job, mutate }: Props): ReactElement {
  const t = useTranslations('jobs.overview.schedule');
  const { allowed } = useLicenseFeature('scheduling');
  const schema = useMemo(() => scheduleFormSchema(t('invalidCron')), [t]);
  const form = useForm({
    mode: 'onChange',
    resolver: yupResolver(schema),
    values: { cronSchedule: job?.cronSchedule },
  });
  const { mutateAsync: updateJobScheduleAsync } = useMutation(
    JobService.method.updateJobSchedule
  );

  async function onSubmit(values: ScheduleFormValues) {
    try {
      const resp = await updateJobScheduleAsync({
        id: job.id,
        cronSchedule: values.cronSchedule,
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

  const msg =
    !form.getValues().cronSchedule || form.getValues().cronSchedule === ''
      ? t('notScheduled')
      : '';

  return (
    <Card className="h-full overflow-hidden">
      <CardHeader>
        <CardTitle>{t('title')}</CardTitle>
        <LicenseFeatureNotice feature="scheduling" />
      </CardHeader>
      <Form {...form}>
        <form onSubmit={form.handleSubmit(onSubmit)}>
          <CardContent className="p-5">
            <FormField
              control={form.control}
              name="cronSchedule"
              render={({ field }) => (
                <FormItem>
                  <FormDescription>{t('description')}</FormDescription>
                  <FormControl>
                    <Input
                      placeholder={t('placeholder')}
                      className="font-mono"
                      value={field.value || ''}
                      onChange={field.onChange}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </CardContent>
          <CardFooter className="flex justify-center border-t border-line-soft bg-surface-2 py-2">
            <div className="flex flex-row items-center justify-between w-full">
              <p className="text-muted-foreground text-sm">{msg}</p>
              {/* Without the feature a new or changed cron cannot be saved; a schedule is stopped with Pause. */}
              <Button
                type="submit"
                disabled={
                  !form.formState.isDirty ||
                  (!allowed && !!form.watch('cronSchedule'))
                }
              >
                {t('save')}
              </Button>
            </div>
          </CardFooter>
        </form>
      </Form>
    </Card>
  );
}
