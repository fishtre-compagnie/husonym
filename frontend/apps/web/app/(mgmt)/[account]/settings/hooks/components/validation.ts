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
import * as yup from 'yup';

const AccountHookWebhookFormValues = yup.object().shape({
  // The hook type context must be provided to disable the required validation all the way down.
  // Simply telling yup that the parent config is nullable is not sufficient.
  url: yup.string().when('$hookType', {
    is: 'webhook',
    then: (schema) => schema.required('URL is required'),
    otherwise: (schema) => schema.notRequired(),
  }),
  secret: yup.string().when('$hookType', {
    is: 'webhook',
    then: (schema) => schema.required('Secret is required'),
    otherwise: (schema) => schema.notRequired(),
  }),
  disableSslVerification: yup.boolean().optional(),
});

export type AccountHookWebhookFormValues = yup.InferType<
  typeof AccountHookWebhookFormValues
>;

const HookTypeFormValue = yup
  .string()
  .oneOf(['webhook'], 'Only webhook hooks are currently supported')
  .required('Hook type is required');
export type HookTypeFormValue = yup.InferType<typeof HookTypeFormValue>;

const EnabledFormValue = yup
  .boolean()
  .required('Must provide an enabled value');

const AccountHookNameFormValue = yup
  .string()
  .required('Name is required')
  .min(3, 'Name must be at least 3 characters')
  .max(100, 'The Hook name must be at most 100 characters')
  .test(
    'resourceName',
    'Name must be between 3-100 characters and may only include lowercase letters, numbers, and hyphens',
    (value) => {
      return RESOURCE_NAME_REGEX.test(value);
    }
  );

const AccountHookDescriptionFormValue = yup
  .string()
  .required('Description is required');

const AccountHookConfigFormValues = yup.object().shape({
  webhook: AccountHookWebhookFormValues.when('$hookType', {
    is: 'webhook',
    then: (schema) =>
      schema.required('Webhook config is required when hook type is webhook'),
    otherwise: (schema) => schema.notRequired(),
  }),
});
export type AccountHookConfigFormValues = yup.InferType<
  typeof AccountHookConfigFormValues
>;

const AccountHookEventFormValue = yup
  .string()
  .default(AccountHookEvent.UNSPECIFIED.toString())
  .required('An event is required');
type AccountHookEventFormValue = yup.InferType<
  typeof AccountHookEventFormValue
>;

const AccountHookEventsFormValue = yup
  .array()
  .of(AccountHookEventFormValue)
  .min(1, 'At least one event is required');
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
    events: AccountHookEventsFormValue.required('Events are required'),
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
    events: AccountHookEventsFormValue.required('Events are required'),
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
