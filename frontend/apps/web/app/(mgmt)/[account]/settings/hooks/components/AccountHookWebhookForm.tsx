import { useTranslations } from 'next-intl';
import { ReactElement } from 'react';

import FormErrorMessage from '@/components/FormErrorMessage';
import FormHeader from '@/components/forms/FormHeader';
import { PasswordInput } from '@/components/PasswordComponent';
import { Input } from '@/components/ui/input';
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';
import { AccountHookWebhookFormValues } from './validation';

interface Props {
  values: AccountHookWebhookFormValues;
  setValues(values: AccountHookWebhookFormValues): void;
  errors: Record<string, string>;
}

export default function AccountHookWebhookForm(props: Props): ReactElement {
  const { values, setValues, errors } = props;
  const t = useTranslations('settings.hooks.form');
  return (
    <>
      <div className="flex flex-col gap-3">
        <FormHeader
          title={t('webhookUrl')}
          description={t('webhookUrlDescription')}
          isErrored={!!errors['config.webhook.url']}
          isRequired={true}
        />
        <Input
          id="url"
          value={values.url}
          onChange={(e) => setValues({ ...values, url: e.target.value })}
          placeholder="https://example.com/webhook"
        />
        <FormErrorMessage message={errors['config.webhook.url']} />
      </div>
      <div className="flex flex-col gap-3">
        <FormHeader
          title={t('webhookSecret')}
          description={t('webhookSecretDescription')}
          isErrored={!!errors['config.webhook.secret']}
          isRequired={true}
        />
        <PasswordInput
          id="secret"
          value={values.secret}
          onChange={(e) => setValues({ ...values, secret: e.target.value })}
          placeholder="your-secret-key"
        />
        <FormErrorMessage message={errors['config.webhook.secret']} />
      </div>
      <div className="flex flex-col gap-3">
        <FormHeader
          title={t('disableSsl')}
          description={t('disableSslDescription')}
        />
        <ToggleGroup
          className="flex justify-start"
          type="single"
          value={values.disableSslVerification ? 'yes' : 'no'}
          onValueChange={(value) => {
            if (!value) {
              return;
            }
            setValues({
              ...values,
              disableSslVerification: value === 'yes',
            });
          }}
        >
          <ToggleGroupItem value="no">{t('no')}</ToggleGroupItem>
          <ToggleGroupItem value="yes">{t('yes')}</ToggleGroupItem>
        </ToggleGroup>
      </div>
    </>
  );
}
