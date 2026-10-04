import { create } from '@bufbuild/protobuf';
import {
  JobSchema,
  JobTypeConfig_JobTypePiiDetect_DataSampling_ModelInput,
} from '@husonym/sdk';
import { toPiiDetectJobTypeConfig } from '../../../../jobs/util';
import { PiiDetectionSchemaFormValues } from '../../job-form-validations';
import { withModelInputDefault } from './model-input';
import { setInitialFormStateFromJob } from './stores';

function piiDetectJob(
  modelInput: JobTypeConfig_JobTypePiiDetect_DataSampling_ModelInput
) {
  return create(JobSchema, {
    jobType: {
      jobType: {
        case: 'piiDetect',
        value: { dataSampling: { isEnabled: true, modelInput } },
      },
    },
  });
}

function storedDataSampling(
  job: ReturnType<typeof piiDetectJob>
): Record<string, unknown> {
  const items = new Map<string, string>();
  const storage = {
    setItem: (key: string, value: string) => items.set(key, value),
  } as unknown as Storage;
  setInitialFormStateFromJob(storage, 'key', job);
  return JSON.parse(items.get('key') ?? '{}').state.formData.dataSampling;
}

describe('PII detection data sampling form mapping', () => {
  const { UNSPECIFIED, PROFILES, VALUES } =
    JobTypeConfig_JobTypePiiDetect_DataSampling_ModelInput;

  it('reads the values choice from a job', () => {
    expect(storedDataSampling(piiDetectJob(VALUES))).toEqual({
      isEnabled: true,
      modelInput: 'values',
    });
  });

  it.each([UNSPECIFIED, PROFILES])(
    'reads statistics only from a job with model input %s',
    (modelInput) => {
      expect(storedDataSampling(piiDetectJob(modelInput))).toMatchObject({
        modelInput: 'profiles',
      });
    }
  );

  it('writes MODEL_INPUT_VALUES for the values choice', () => {
    const config = toPiiDetectJobTypeConfig({
      dataSampling: { isEnabled: true, modelInput: 'values' },
      incremental: { isEnabled: false },
      tableScanFilter: {
        mode: 'include_all',
        patterns: { schemas: [], tables: [] },
      },
      userPrompt: '',
    });
    expect(config.dataSampling?.modelInput).toBe(VALUES);
  });

  it('leaves the field unset for statistics only', () => {
    const config = toPiiDetectJobTypeConfig({
      dataSampling: { isEnabled: true, modelInput: 'profiles' },
      incremental: { isEnabled: false },
      tableScanFilter: {
        mode: 'include_all',
        patterns: { schemas: [], tables: [] },
      },
      userPrompt: '',
    });
    expect(config.dataSampling?.modelInput).toBe(UNSPECIFIED);
  });

  it('leaves the field unset when sampling is disabled, whatever the choice', () => {
    const config = toPiiDetectJobTypeConfig({
      dataSampling: { isEnabled: false, modelInput: 'values' },
      incremental: { isEnabled: false },
      tableScanFilter: {
        mode: 'include_all',
        patterns: { schemas: [], tables: [] },
      },
      userPrompt: '',
    });
    expect(config.dataSampling?.isEnabled).toBe(false);
    expect(config.dataSampling?.modelInput).toBe(UNSPECIFIED);
  });

  it('reads statistics only from a stored form that holds no choice', () => {
    const stored = {
      dataSampling: { isEnabled: true },
      incremental: { isEnabled: false },
      tableScanFilter: {
        mode: 'include_all',
        patterns: { schemas: [], tables: [] },
      },
      userPrompt: '',
    } as unknown as PiiDetectionSchemaFormValues;
    expect(withModelInputDefault(stored).dataSampling).toEqual({
      isEnabled: true,
      modelInput: 'profiles',
    });
  });

  it('keeps the values choice of a stored form', () => {
    const stored = {
      dataSampling: { isEnabled: true, modelInput: 'values' },
    } as unknown as PiiDetectionSchemaFormValues;
    expect(withModelInputDefault(stored).dataSampling.modelInput).toBe(
      'values'
    );
  });
});
