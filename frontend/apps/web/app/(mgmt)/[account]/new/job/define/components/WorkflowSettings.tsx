import FormErrorMessage from '@/components/FormErrorMessage';
import FormHeader from '@/components/forms/FormHeader';
import { Input } from '@/components/ui/input';
import { useTranslations } from 'next-intl';
import { FieldErrors } from 'react-hook-form';
import {
  ActivityOptionsFormValues,
  NewJobType,
} from '../../job-form-validations';

interface SyncActivityOptionsFormProps {
  value: ActivityOptionsFormValues;
  setValue(value: ActivityOptionsFormValues): void;
  errors?: FieldErrors<ActivityOptionsFormValues>;
  jobtype: NewJobType;
}

export default function SyncActivityOptionsForm({
  value,
  setValue,
  errors,
  jobtype,
}: SyncActivityOptionsFormProps) {
  const { title: startToCloseTitle, description: startToCloseDescription } =
    useStartToCloseTimeoutLabels(jobtype);
  const {
    title: scheduleToCloseTitle,
    description: scheduleToCloseDescription,
  } = useScheduleToCloseTimeoutLabels(jobtype);
  const { title: retryPolicyTitle, description: retryPolicyDescription } =
    useRetryPolicyLabels(jobtype);
  return (
    <div className="flex flex-col gap-6">
      <div className="space-y-2">
        <FormHeader
          htmlFor="startToCloseTimeout"
          title={startToCloseTitle}
          description={startToCloseDescription}
          isRequired={true}
          isErrored={!!errors?.startToCloseTimeout}
        />
        <Input
          type="number"
          value={value.startToCloseTimeout ?? 0}
          onChange={(e) =>
            setValue({
              ...value,
              startToCloseTimeout: e.target.valueAsNumber,
            })
          }
        />
        <FormErrorMessage message={errors?.startToCloseTimeout?.message} />
      </div>
      <div className="space-y-2">
        <FormHeader
          htmlFor="scheduleToCloseTimeout"
          title={scheduleToCloseTitle}
          description={scheduleToCloseDescription}
          isErrored={!!errors?.scheduleToCloseTimeout}
        />
        <Input
          type="number"
          value={value.scheduleToCloseTimeout ?? 0}
          onChange={(e) =>
            setValue({
              ...value,
              scheduleToCloseTimeout: e.target.valueAsNumber,
            })
          }
        />
        <FormErrorMessage message={errors?.scheduleToCloseTimeout?.message} />
      </div>
      <div className="space-y-2">
        <FormHeader
          htmlFor="retryPolicy.maximumAttempts"
          title={retryPolicyTitle}
          description={retryPolicyDescription}
          isErrored={!!errors?.retryPolicy?.maximumAttempts}
        />
        <Input
          type="number"
          value={value.retryPolicy?.maximumAttempts ?? 0}
          onChange={(e) =>
            setValue({
              ...value,
              retryPolicy: {
                ...value.retryPolicy,
                maximumAttempts: e.target.valueAsNumber,
              },
            })
          }
        />
        <FormErrorMessage
          message={errors?.retryPolicy?.maximumAttempts?.message}
        />
      </div>
    </div>
  );
}

interface Labels {
  title: string;
  description: string;
}

function useStartToCloseTimeoutLabels(jobtype: NewJobType): Labels {
  const t = useTranslations('newJob.activityOptions');
  return jobtype === 'pii-detection'
    ? {
        title: t('pii.startToCloseTitle'),
        description: t('pii.startToCloseDescription'),
      }
    : {
        title: t('sync.startToCloseTitle'),
        description: t('sync.startToCloseDescription'),
      };
}

function useScheduleToCloseTimeoutLabels(jobtype: NewJobType): Labels {
  const t = useTranslations('newJob.activityOptions');
  return jobtype === 'pii-detection'
    ? {
        title: t('pii.scheduleToCloseTitle'),
        description: t('pii.scheduleToCloseDescription'),
      }
    : {
        title: t('sync.scheduleToCloseTitle'),
        description: t('sync.scheduleToCloseDescription'),
      };
}

function useRetryPolicyLabels(jobtype: NewJobType): Labels {
  const t = useTranslations('newJob.activityOptions');
  return jobtype === 'pii-detection'
    ? {
        title: t('retryTitle'),
        description: t('pii.retryDescription'),
      }
    : {
        title: t('retryTitle'),
        description: t('sync.retryDescription'),
      };
}
