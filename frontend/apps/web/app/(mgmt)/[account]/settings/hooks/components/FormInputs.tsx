import FormErrorMessage from '@/components/FormErrorMessage';
import FormHeader from '@/components/forms/FormHeader';
import { Input } from '@/components/ui/input';
import { Textarea } from '@/components/ui/textarea';
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';
import { AccountHookEvent } from '@husonym/sdk';
import { useTranslations } from 'next-intl';
import { ReactElement } from 'react';
import AccountHookWebhookForm from './AccountHookWebhookForm';
import {
  AccountHookConfigFormValues,
  AccountHookEventsFormValue,
  HookTypeFormValue,
} from './validation';
interface NameProps {
  error?: string;
  value: string;
  onChange(value: string): void;
}

export function Name(props: NameProps): ReactElement {
  const { error, value, onChange } = props;
  const t = useTranslations('settings.hooks.form');

  return (
    <div className="space-y-2">
      <FormHeader
        htmlFor="name"
        title={t('name')}
        description={t('nameDescription')}
        isErrored={!!error}
        isRequired={true}
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
  const t = useTranslations('settings.hooks.form');

  return (
    <div className="space-y-2">
      <FormHeader
        htmlFor="description"
        title={t('description')}
        description={t('descriptionDescription')}
        isErrored={!!error}
        isRequired={true}
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

interface EnabledProps {
  error?: string;
  value: boolean;
  onChange(value: boolean): void;
}

export function Enabled(props: EnabledProps): ReactElement {
  const { error, value, onChange } = props;
  const t = useTranslations('settings.hooks.form');

  return (
    <div className="flex flex-col gap-4">
      <FormHeader
        htmlFor="enabled"
        title={t('enabled')}
        description={t('enabledDescription')}
        isErrored={!!error}
        isRequired={true}
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
  const t = useTranslations('settings.hooks.form');
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
        <ToggleGroupItem value="webhook">Webhook</ToggleGroupItem>
      </ToggleGroup>
      <FormErrorMessage message={error} />
    </div>
  );
}

interface AccountHookConfigProps {
  errors: Record<string, string>;
  hookType: HookTypeFormValue;
  value: AccountHookConfigFormValues;
  onChange(value: AccountHookConfigFormValues): void;
}

export function AccountHookConfig(props: AccountHookConfigProps): ReactElement {
  const { errors, hookType, value, onChange } = props;

  return (
    <div className="flex flex-col gap-4">
      {hookType === 'webhook' && (
        <AccountHookWebhookForm
          values={value.webhook}
          setValues={(newWebhookData) => {
            onChange({ webhook: newWebhookData });
          }}
          errors={errors}
        />
      )}
    </div>
  );
}

interface AccountHookEventsProps {
  error?: string;
  value: AccountHookEventsFormValue;
  onChange(value: AccountHookEventsFormValue): void;
}

export function AccountHookEvents(props: AccountHookEventsProps): ReactElement {
  const { error, value, onChange } = props;
  const t = useTranslations('settings.hooks.form');

  return (
    <div className="flex flex-col gap-4">
      <FormHeader
        title={t('events')}
        description={t('eventsDescription')}
        isErrored={!!error}
      />
      <ToggleGroup
        className="flex justify-start"
        type="multiple"
        value={value}
        onValueChange={(value) => {
          onChange(value as AccountHookEventsFormValue);
        }}
      >
        <ToggleGroupItem
          key={AccountHookEvent.UNSPECIFIED}
          value={AccountHookEvent.UNSPECIFIED.toString()}
          className="border-2"
        >
          {t('wildcard')}
        </ToggleGroupItem>
        <ToggleGroupItem
          key={AccountHookEvent.JOB_RUN_CREATED}
          value={AccountHookEvent.JOB_RUN_CREATED.toString()}
          className="border-2"
        >
          {t('jobRunCreated')}
        </ToggleGroupItem>
        <ToggleGroupItem
          key={AccountHookEvent.JOB_RUN_FAILED}
          value={AccountHookEvent.JOB_RUN_FAILED.toString()}
          className="border-2"
        >
          {t('jobRunFailed')}
        </ToggleGroupItem>
        <ToggleGroupItem
          key={AccountHookEvent.JOB_RUN_SUCCEEDED}
          value={AccountHookEvent.JOB_RUN_SUCCEEDED.toString()}
          className="border-2"
        >
          {t('jobRunSucceeded')}
        </ToggleGroupItem>
      </ToggleGroup>
      <FormErrorMessage message={error} />
    </div>
  );
}
