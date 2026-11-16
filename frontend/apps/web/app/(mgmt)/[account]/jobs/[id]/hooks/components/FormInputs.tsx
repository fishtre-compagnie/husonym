import FormErrorMessage from '@/components/FormErrorMessage';
import { Input } from '@/components/ui/input';
import { Textarea } from '@/components/ui/textarea';
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';
import { Connection } from '@husonym/sdk';
import { ReactElement } from 'react';
import FormHeader from './FormHeader';
import JobConfigSqlForm from './JobConfigSqlForm';
import { HookTypeFormValue, JobHookConfigFormValues } from './validation';
import { useTranslations } from 'next-intl';

interface NameProps {
  error?: string;
  value: string;
  onChange(value: string): void;
}

export function Name(props: NameProps): ReactElement {
  const { error, value, onChange } = props;
  const t = useTranslations('jobs.hooks.form');

  return (
    <div className="space-y-2">
      <FormHeader
        htmlFor="name"
        title={t('name')}
        description={t('nameDescription')}
        isErrored={!!error}
      />
      <Input
        id="name"
        autoCapitalize="off" // we don't allow capitals in team names
        data-1p-ignore // tells 1password extension to not autofill this field
        value={value || ''}
        onChange={(e) => onChange(e.target.value)}
        placeholder={t('namePlaceholder')}
      />
      <FormErrorMessage message={error} />
    </div>
  );
}

interface DescriptionProps {
  error?: string;
  value: string;
  onChange(value: string): void;
}

export function Description(props: DescriptionProps): ReactElement {
  const { error, value, onChange } = props;
  const t = useTranslations('jobs.hooks.form');

  return (
    <div className="space-y-2">
      <FormHeader
        htmlFor="description"
        title={t('description')}
        description={t('descriptionDescription')}
        isErrored={!!error}
      />
      <Textarea
        id="description"
        value={value || ''}
        onChange={(e) => onChange(e.target.value)}
        placeholder={t('descriptionPlaceholder')}
        rows={3}
      />
      <FormErrorMessage message={error} />
    </div>
  );
}

interface PriorityProps {
  error?: string;
  value: number;
  onChange(value: number): void;
}

export function Priority(props: PriorityProps): ReactElement {
  const { error, value, onChange } = props;
  const t = useTranslations('jobs.hooks.form');

  return (
    <div className="grid grid-cols-2 gap-4">
      <div className="space-y-2">
        <FormHeader
          htmlFor="priority"
          title={t('priority')}
          description={t('priorityDescription')}
          isErrored={!!error}
        />
        <Input
          id="priority"
          type="number"
          value={value}
          onChange={(e) => onChange(e.target.valueAsNumber)}
        />
        <FormErrorMessage message={error} />
      </div>
    </div>
  );
}

interface EnabledProps {
  error?: string;
  value: boolean;
  onChange(value: boolean): void;
}

export function Enabled(props: EnabledProps): ReactElement {
  const { error, value, onChange } = props;
  const t = useTranslations('jobs.hooks.form');

  return (
    <div className="flex flex-col gap-4">
      <FormHeader
        htmlFor="enabled"
        title={t('enabled')}
        description={t('enabledDescription')}
        isErrored={!!error}
      />
      <ToggleGroup
        className="flex justify-start"
        type="single"
        onValueChange={(value) => {
          if (!value) {
            return;
          }
          onChange(value === 'yes');
        }}
        value={value ? 'yes' : 'no'}
      >
        <ToggleGroupItem value="yes">{t('yes')}</ToggleGroupItem>
        <ToggleGroupItem value="no">{t('no')}</ToggleGroupItem>
      </ToggleGroup>
      <FormErrorMessage message={error} />
    </div>
  );
}

interface HookTypeProps {
  error?: string;
  value: HookTypeFormValue;
  onChange(value: HookTypeFormValue): void;
}

export function HookType(props: HookTypeProps): ReactElement {
  const { error, value, onChange } = props;
  const t = useTranslations('jobs.hooks.form');

  return (
    <div className="flex flex-col gap-4">
      <FormHeader
        title={t('hookType')}
        description={t('hookTypeDescription')}
        isErrored={!!error}
      />
      <ToggleGroup
        className="flex justify-start"
        type="single"
        onValueChange={(value) => {
          if (value) {
            onChange(value as HookTypeFormValue);
          }
        }}
        value={value}
      >
        <ToggleGroupItem value="sql">SQL</ToggleGroupItem>
      </ToggleGroup>
      <FormErrorMessage message={error} />
    </div>
  );
}

interface JobConfigProps {
  errors: Record<string, string>;
  hookType: HookTypeFormValue;
  value: JobHookConfigFormValues;
  onChange(value: JobHookConfigFormValues): void;
  jobConnections: Connection[];
}

export function JobConfig(props: JobConfigProps): ReactElement {
  const { errors, hookType, value, onChange, jobConnections } = props;

  return (
    <div className="flex flex-col gap-4">
      {hookType === 'sql' && (
        <JobConfigSqlForm
          values={value.sql}
          setValues={(newSqlData) => {
            onChange({ sql: newSqlData });
          }}
          jobConnections={jobConnections}
          errors={errors}
        />
      )}
    </div>
  );
}
