import { RESOURCE_NAME_REGEX } from '@/yup-validations/connections';
import { useTranslations } from 'next-intl';
import { create } from '@bufbuild/protobuf';
import {
  JobHook,
  JobHookConfig,
  JobHookConfig_JobSqlHook_Timing,
  JobHookConfig_JobSqlHook_TimingSchema,
  JobHookConfig_JobSqlHookSchema,
  JobHookConfigSchema,
  JobHookSchema,
  JobHookTimingPostSyncSchema,
  JobHookTimingPreSyncSchema,
  NewJobHook,
  NewJobHookSchema,
} from '@husonym/sdk';
import * as yup from 'yup';

// The messages of the schemas below are keys of `jobs.hooks.validation`: a form shows
// them through useHookErrorMessage.
const VALIDATION_KEYS = [
  'timingRequired',
  'queryRequired',
  'connectionRequired',
  'sqlOnly',
  'hookTypeRequired',
  'enabledRequired',
  'nameRequired',
  'nameMin',
  'nameMax',
  'nameFormat',
  'descriptionRequired',
  'priorityRequired',
  'priorityRange',
  'sqlConfigRequired',
] as const;
type ValidationKey = (typeof VALIDATION_KEYS)[number];

function isValidationKey(message: string): message is ValidationKey {
  return (VALIDATION_KEYS as readonly string[]).includes(message);
}

// The text of a validation message of the schemas: its translation, or the message as
// yup gave it when it is not one of ours.
export function useHookErrorMessage(): (message: string) => string {
  const t = useTranslations('jobs.hooks.validation');
  return (message) => (isValidationKey(message) ? t(message) : message);
}

const SqlTimingFormValue = yup
  .string()
  .oneOf(['preSync', 'postSync'])
  .required('timingRequired');
export type SqlTimingFormValue = yup.InferType<typeof SqlTimingFormValue>;

const JobHookSqlFormValues = yup.object({
  query: yup.string().required('queryRequired'),
  connectionId: yup.string().uuid().required('connectionRequired'),
  timing: SqlTimingFormValue,
});
export type JobHookSqlFormValues = yup.InferType<typeof JobHookSqlFormValues>;

const HookTypeFormValue = yup
  .string()
  .oneOf(['sql'], 'sqlOnly')
  .required('hookTypeRequired');
export type HookTypeFormValue = yup.InferType<typeof HookTypeFormValue>;

const EnabledFormValue = yup.boolean().required('enabledRequired');

const JobHookNameFormValue = yup
  .string()
  .required('nameRequired')
  .min(3, 'nameMin')
  .max(100, 'nameMax')
  .test('resourceName', 'nameFormat', (value) => {
    return RESOURCE_NAME_REGEX.test(value);
  });

const JobHookDescriptionFormValue = yup
  .string()
  .required('descriptionRequired');

const JobHookPriorityFormValue = yup
  .number()
  .required('priorityRequired')
  .min(0, 'priorityRange')
  .max(100, 'priorityRange');

const JobHookConfigFormValues = yup.object({
  sql: JobHookSqlFormValues.when('hookType', (values, schema) => {
    const [hooktype] = values;
    return hooktype === 'sql' ? schema.required('sqlConfigRequired') : schema;
  }),
});
export type JobHookConfigFormValues = yup.InferType<
  typeof JobHookConfigFormValues
>;

export const EditJobHookFormValues = yup.object({
  name: JobHookNameFormValue,
  description: JobHookDescriptionFormValue,
  priority: JobHookPriorityFormValue,
  enabled: EnabledFormValue,
  hookType: HookTypeFormValue,
  config: JobHookConfigFormValues,
});
export type EditJobHookFormValues = yup.InferType<typeof EditJobHookFormValues>;

export function toEditFormData(input: JobHook): EditJobHookFormValues {
  return {
    name: input.name,
    description: input.description,
    hookType: toHookType(input.config ?? create(JobHookConfigSchema)),
    priority: input.priority,
    enabled: input.enabled,
    config: {
      sql: toSqlConfig(input.config ?? create(JobHookConfigSchema)),
    },
  };
}

export const NewJobHookFormValues = yup.object({
  name: JobHookNameFormValue,
  description: JobHookDescriptionFormValue,
  priority: JobHookPriorityFormValue,
  enabled: EnabledFormValue,
  hookType: HookTypeFormValue,
  config: JobHookConfigFormValues,
});
export type NewJobHookFormValues = yup.InferType<typeof NewJobHookFormValues>;

function toSqlConfig(input: JobHookConfig): JobHookSqlFormValues {
  switch (input.config.case) {
    case 'sql': {
      return {
        connectionId: input.config.value.connectionId,
        query: input.config.value.query,
        timing: toSqlTimingConfig(
          input.config.value.timing ??
            create(JobHookConfig_JobSqlHook_TimingSchema)
        ),
      };
    }
    default: {
      return {
        connectionId: '',
        query: 'DEFAULT QUERY',
        timing: 'preSync',
      };
    }
  }
}
function toSqlTimingConfig(
  input: JobHookConfig_JobSqlHook_Timing
): SqlTimingFormValue {
  switch (input.timing.case) {
    case 'preSync': {
      return 'preSync';
    }
    case 'postSync': {
      return 'postSync';
    }
    default: {
      return 'preSync';
    }
  }
}

function toHookType(input: JobHookConfig): HookTypeFormValue {
  switch (input.config.case) {
    case 'sql': {
      return 'sql';
    }
    default: {
      return 'sql';
    }
  }
}

export function editFormDataToJobHook(
  input: JobHook,
  values: EditJobHookFormValues
): JobHook {
  const newValues = newFormDataToNewJobHook(values);
  return create(JobHookSchema, {
    ...input,
    name: newValues.name,
    description: newValues.description,
    enabled: newValues.enabled,
    priority: newValues.priority,
    config: newValues.config,
  });
}

export function newFormDataToNewJobHook(
  values: NewJobHookFormValues
): NewJobHook {
  return create(NewJobHookSchema, {
    name: values.name,
    description: values.description,
    enabled: values.enabled,
    priority: values.priority,
    config: toJobHookConfig(values),
  });
}

function toJobHookConfig(
  values: EditJobHookFormValues
): JobHookConfig | undefined {
  switch (values.hookType) {
    case 'sql': {
      return create(JobHookConfigSchema, {
        config: {
          case: 'sql',
          value: create(JobHookConfig_JobSqlHookSchema, {
            connectionId: values.config.sql.connectionId,
            query: values.config.sql.query,
            timing: toJobHookSqlTimingConfig(values.config.sql.timing),
          }),
        },
      });
    }
  }
}

function toJobHookSqlTimingConfig(
  values: JobHookSqlFormValues['timing']
): JobHookConfig_JobSqlHook_Timing {
  switch (values) {
    case 'preSync': {
      return create(JobHookConfig_JobSqlHook_TimingSchema, {
        timing: {
          case: 'preSync',
          value: create(JobHookTimingPreSyncSchema),
        },
      });
    }
    case 'postSync': {
      return create(JobHookConfig_JobSqlHook_TimingSchema, {
        timing: {
          case: 'postSync',
          value: create(JobHookTimingPostSyncSchema),
        },
      });
    }
    default: {
      return create(JobHookConfig_JobSqlHook_TimingSchema, {
        timing: {
          case: 'preSync',
          value: create(JobHookTimingPreSyncSchema),
        },
      });
    }
  }
}
