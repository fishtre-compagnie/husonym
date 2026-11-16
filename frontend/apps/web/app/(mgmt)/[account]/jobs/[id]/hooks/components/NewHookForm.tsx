import ButtonText from '@/components/ButtonText';
import Spinner from '@/components/Spinner';
import { Button } from '@/components/ui/button';
import { Connection, NewJobHook } from '@husonym/sdk';
import { useTranslations } from 'next-intl';
import { FormEvent, ReactElement } from 'react';
import { ValidationError } from 'yup';
import {
  Description,
  Enabled,
  HookType,
  JobConfig,
  Name,
  Priority,
} from './FormInputs';
import { useNewHookStore } from './stores';
import {
  newFormDataToNewJobHook,
  NewJobHookFormValues,
  useHookErrorMessage,
} from './validation';

interface Props {
  onSubmit(values: NewJobHook): Promise<void>;
  jobConnections: Connection[];
}

export default function NewHookForm(props: Props): ReactElement {
  const { onSubmit, jobConnections } = props;
  const t = useTranslations('jobs.hooks.form');
  const errorMessage = useHookErrorMessage();
  const {
    formData,
    errors,
    setFormData,
    setErrors,
    setSubmitting,
    isSubmitting,
  } = useNewHookStore();

  async function handleSubmit(e: FormEvent): Promise<void> {
    e.preventDefault();
    if (isSubmitting) {
      return;
    }

    try {
      setSubmitting(true);
      setErrors({});

      const validatedData = await NewJobHookFormValues.validate(formData, {
        abortEarly: false,
      });
      await onSubmit(newFormDataToNewJobHook(validatedData));
    } catch (err) {
      if (err instanceof ValidationError) {
        const validationErrors: Record<string, string> = {};
        err.inner.forEach((error) => {
          if (error.path) {
            validationErrors[error.path] = errorMessage(error.message);
          }
        });
        setErrors(validationErrors);
      }
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-6">
      <Name
        error={errors.name}
        value={formData.name ?? ''}
        onChange={(value) => setFormData({ name: value })}
      />
      <Description
        error={errors.description}
        value={formData.description ?? ''}
        onChange={(value) => setFormData({ description: value })}
      />
      <Priority
        error={errors.priority}
        value={formData.priority}
        onChange={(value) => setFormData({ priority: value })}
      />
      <Enabled
        error={errors.enabled}
        value={formData.enabled}
        onChange={(value) => setFormData({ enabled: value })}
      />
      <HookType
        error={errors.hookType}
        value={formData.hookType}
        onChange={(value) => setFormData({ hookType: value })}
      />
      <JobConfig
        errors={errors}
        value={formData.config}
        hookType={formData.hookType}
        jobConnections={jobConnections}
        onChange={(value) => setFormData({ config: value })}
      />

      <div className="flex justify-end gap-3">
        <Button
          type="submit"
          disabled={isSubmitting}
          className="w-full sm:w-auto"
        >
          <ButtonText
            leftIcon={isSubmitting ? <Spinner /> : undefined}
            text={t('create')}
          />
        </Button>
      </div>
    </form>
  );
}
