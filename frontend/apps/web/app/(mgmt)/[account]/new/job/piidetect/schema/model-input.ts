import { PiiDetectionSchemaFormValues } from '../../job-form-validations';

// A form kept in the session may hold no choice of what the model receives: it
// then reads as the statistics choice.
export function withModelInputDefault(
  formData: PiiDetectionSchemaFormValues
): PiiDetectionSchemaFormValues {
  return {
    ...formData,
    dataSampling: {
      ...formData.dataSampling,
      modelInput:
        formData.dataSampling?.modelInput === 'values' ? 'values' : 'profiles',
    },
  };
}
