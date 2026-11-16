import { RESOURCE_NAME_REGEX } from '@/yup-validations/connections';
import { create } from '@bufbuild/protobuf';
import {
  AccountHook,
  AccountHookConfig,
  AccountHookConfig_WebHookSchema,
  AccountHookConfigSchema,
  AccountHookEvent,
  AccountHookSchema,
  NewAccountHook,
  NewAccountHookSchema,
} from '@husonym/sdk';
import { useTranslations } from 'next-intl';
import * as yup from 'yup';

// The messages of the schemas below are keys of `settings.hooks.validation`, turned into
// words by the form that shows them.
const VALIDATION_KEYS = [
  'urlRequired',
  'secretRequired',
  'webhookOnly',
  'hookTypeRequired',
  'enabledRequired',
  'nameRequired',
  'nameMin',
  'nameMax',
  'nameFormat',
  'descriptionRequired',
  'webhookConfigRequired',
  'eventRequired',
  'eventsMin',
  'eventsRequired',
] as const;
type ValidationKey = (typeof VALIDATION_KEYS)[number];

function isValidationKey(message: string): message is ValidationKey {
  return (VALIDATION_KEYS as readonly string[]).includes(message);
}

// The words of a validation message: one of ours is translated, any other (from yup
// itself) is shown as it is.
export function hookValidationMessage(
  message: string,
  t: ReturnType<typeof useTranslations<'settings.hooks.validation'>>
): string {
  return isValidationKey(message) ? t(message) : message;
}

const AccountHookWebhookFormValues = yup.object().shape({
  // The hook type context must be provided to disable the required validation all the way down.
  // Simply telling yup that the parent config is nullable is not sufficient.
  url: yup.string().when('$hookType', {
    is: 'webhook',
    then: (schema) => schema.required('urlRequired'),
    otherwise: (schema) => schema.notRequired(),
  }),
  secret: yup.string().when('$hookType', {
    is: 'webhook',
    then: (schema) => schema.required('secretRequired'),
    otherwise: (schema) => schema.notRequired(),
  }),
  disableSslVerification: yup.boolean().optional(),
});

export type AccountHookWebhookFormValues = yup.InferType<
  typeof AccountHookWebhookFormValues
>;

const HookTypeFormValue = yup
  .string()
  .oneOf(['webhook'], 'webhookOnly')
  .required('hookTypeRequired');
export type HookTypeFormValue = yup.InferType<typeof HookTypeFormValue>;

const EnabledFormValue = yup.boolean().required('enabledRequired');

const AccountHookNameFormValue = yup
  .string()
  .required('nameRequired')
  .min(3, 'nameMin')
  .max(100, 'nameMax')
  .test('resourceName', 'nameFormat', (value) => {
    return RESOURCE_NAME_REGEX.test(value);
  });

const AccountHookDescriptionFormValue = yup
  .string()
  .required('descriptionRequired');

const AccountHookConfigFormValues = yup.object().shape({
  webhook: AccountHookWebhookFormValues.when('$hookType', {
    is: 'webhook',
    then: (schema) => schema.required('webhookConfigRequired'),
    otherwise: (schema) => schema.notRequired(),
  }),
});
export type AccountHookConfigFormValues = yup.InferType<
  typeof AccountHookConfigFormValues
>;

const AccountHookEventFormValue = yup
  .string()
  .default(AccountHookEvent.UNSPECIFIED.toString())
  .required('eventRequired');
type AccountHookEventFormValue = yup.InferType<
  typeof AccountHookEventFormValue
>;

const AccountHookEventsFormValue = yup
  .array()
  .of(AccountHookEventFormValue)
  .min(1, 'eventsMin');
export type AccountHookEventsFormValue = yup.InferType<
  typeof AccountHookEventsFormValue
>;

export const EditAccountHookFormValues = yup
  .object<AccountHookFormContext>()
  .shape({
    name: AccountHookNameFormValue,
    description: AccountHookDescriptionFormValue,
    enabled: EnabledFormValue,
    hookType: HookTypeFormValue,
    config: AccountHookConfigFormValues,
    events: AccountHookEventsFormValue.required('eventsRequired'),
  });
export type EditAccountHookFormValues = yup.InferType<
  typeof EditAccountHookFormValues
>;

export function toEditFormData(input: AccountHook): EditAccountHookFormValues {
  return {
    name: input.name,
    description: input.description,
    hookType: 'webhook',
    enabled: input.enabled,
    config: {
      webhook: toWebhookConfig(input.config ?? create(AccountHookConfigSchema)),
    },
    events: input.events.map((event) => event.toString()),
  };
}

interface AccountHookFormContext {
  hookType: HookTypeFormValue;
}

export const NewAccountHookFormValues = yup
  .object<AccountHookFormContext>()
  .shape({
    name: AccountHookNameFormValue,
    description: AccountHookDescriptionFormValue,
    enabled: EnabledFormValue,
    hookType: HookTypeFormValue,
    config: AccountHookConfigFormValues,
    events: AccountHookEventsFormValue.required('eventsRequired'),
  });
export type NewAccountHookFormValues = yup.InferType<
  typeof NewAccountHookFormValues
>;

// What the API returns in the place of a webhook secret to someone who may not edit the
// account. It is not a secret: the API refuses it when it is sent back.
const MASKED_SECRET = '********';

function toWebhookConfig(
  input: AccountHookConfig
): AccountHookWebhookFormValues {
  switch (input.config.case) {
    case 'webhook': {
      const { secret } = input.config.value;
      return {
        url: input.config.value.url,
        // A masked secret leaves the field empty, so that the form asks for the secret
        // rather than sending the mask.
        secret: secret === MASKED_SECRET ? '' : secret,
        disableSslVerification: input.config.value.disableSslVerification,
      };
    }
    default: {
      return {
        url: '',
        secret: '',
        disableSslVerification: false,
      };
    }
  }
}

export function editFormDataToAccountHook(
  input: AccountHook,
  values: EditAccountHookFormValues
): AccountHook {
  const newValues = newFormDataToNewAccountHook(values);
  return create(AccountHookSchema, {
    ...input,
    name: newValues.name,
    description: newValues.description,
    enabled: newValues.enabled,
    config: newValues.config,
    events: newValues.events,
  });
}

export function newFormDataToNewAccountHook(
  values: NewAccountHookFormValues
): NewAccountHook {
  return create(NewAccountHookSchema, {
    name: values.name,
    description: values.description,
    enabled: values.enabled,
    config: toAccountHookConfig(values),
    events: values.events.map((event) => parseInt(event, 10)),
  });
}

function toAccountHookConfig(
  values: EditAccountHookFormValues
): AccountHookConfig | undefined {
  switch (values.hookType) {
    case 'webhook': {
      return create(AccountHookConfigSchema, {
        config: {
          case: 'webhook',
          value: create(AccountHookConfig_WebHookSchema, {
            url: values.config.webhook.url,
            secret: values.config.webhook.secret,
            disableSslVerification:
              values.config.webhook.disableSslVerification,
          }),
        },
      });
    }
    default: {
      return undefined;
    }
  }
}
