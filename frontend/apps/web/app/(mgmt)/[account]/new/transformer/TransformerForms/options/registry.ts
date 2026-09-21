import {
  getGenerateEmailTypeString,
  getGenerateIpAddressVersionString,
  getInvalidEmailActionString,
} from '@/util/util';
import {
  GenerateCardNumberSchema,
  GenerateCategoricalSchema,
  GenerateCountrySchema,
  GenerateE164PhoneNumberSchema,
  GenerateEmailSchema,
  GenerateFloat64Schema,
  GenerateGenderSchema,
  GenerateInt64Schema,
  GenerateIpAddressSchema,
  GenerateStateSchema,
  GenerateStringPhoneNumberSchema,
  GenerateStringSchema,
  GenerateUuidSchema,
  TransformE164PhoneNumberSchema,
  TransformEmailSchema,
  TransformFirstNameSchema,
  TransformFloat64Schema,
  TransformFullNameSchema,
  TransformInt64PhoneNumberSchema,
  TransformInt64Schema,
  TransformLastNameSchema,
  TransformPhoneNumberSchema,
  TransformStringSchema,
  TransformerConfig,
} from '@husonym/sdk';
import { RegisteredOptions, registerOptions } from './register';

// The options of every transformer whose form is a plain list of fields, keyed by the case of
// the TransformerConfig oneof. The one place they are declared: the options form renders them,
// and the review tab summarizes a config with them.
export const TRANSFORMER_OPTIONS: Partial<
  Record<NonNullable<TransformerConfig['config']['case']>, RegisteredOptions>
> = {
  generateCardNumberConfig: registerOptions(GenerateCardNumberSchema, [
    {
      field: 'validLuhn',
      label: 'Valid Luhn',
      description: 'Generate a 16 digit card number that passes a luhn check.',
    },
  ]),
  generateCategoricalConfig: registerOptions(GenerateCategoricalSchema, [
    {
      field: 'categories',
      label: 'Categories',
      description:
        'Provide a list of comma-separated string values that you want to randomly select from.',
      stacked: true,
    },
  ]),
  generateE164PhoneNumberConfig: registerOptions(
    GenerateE164PhoneNumberSchema,
    [
      {
        field: 'min',
        label: 'Minimum Length',
        description:
          'Set the minimum length range of the output phone number. It cannot be less than 9.',
      },
      {
        field: 'max',
        label: 'Maximum Length',
        description:
          'Set the maximum length range of the output phone number. It cannot be greater than 15.',
      },
    ]
  ),
  generateFloat64Config: registerOptions(GenerateFloat64Schema, [
    {
      field: 'randomizeSign',
      label: 'Randomize Sign',
      description:
        'Will randomly assign the sign. This may cause the generated value to be out of the defined min/max range. If the min/max is 20-40, the value may be in the following ranges: 20 <= x <= 40 and -40 <= x <= -20',
    },
    {
      field: 'min',
      label: 'Minimum Value',
      description: 'Sets a minimum range for generated float64 value.',
    },
    {
      field: 'max',
      label: 'Maximum Value',
      description: 'Sets a maximum range for generated float64 value.',
    },
    {
      field: 'precision',
      label: 'Precision',
      description:
        'Sets the precision for the entire float64 value, not just the decimals. For example. a precision of 4 would update a float64 value of 23.567 to 23.56.',
    },
  ]),
  generateGenderConfig: registerOptions(GenerateGenderSchema, [
    {
      field: 'abbreviate',
      label: 'Abbreviate',
      description:
        'Abbreviate the gender to a single character. For example, female would be returned as f.',
    },
  ]),
  generateInt64Config: registerOptions(GenerateInt64Schema, [
    {
      field: 'randomizeSign',
      label: 'Randomize Sign',
      description:
        'Will randomly assign the sign. This may cause the generated value to be out of the defined min/max range. If the min/max is 20-40, the value may be in the following ranges: 20 <= x <= 40 and -40 <= x <= -20',
    },
    {
      field: 'min',
      label: 'Minimum Value',
      description: 'Sets a minimum range for generated int64 value.',
    },
    {
      field: 'max',
      label: 'Maximum Value',
      description: 'Sets a maximum range for generated int64 value.',
    },
  ]),
  generateStringConfig: registerOptions(GenerateStringSchema, [
    {
      field: 'min',
      label: 'Minimum Length',
      description: 'Set the minimum length range of the output string.',
    },
    {
      field: 'max',
      label: 'Maximum Length',
      description: 'Set the maximum length range of the output string.',
    },
  ]),
  generateStringPhoneNumberConfig: registerOptions(
    GenerateStringPhoneNumberSchema,
    [
      {
        field: 'min',
        label: 'Minimum Length',
        description: 'Set the minimum length range of the output phone number.',
      },
      {
        field: 'max',
        label: 'Maximum Length',
        description: 'Set the maximum length range of the output phone number.',
      },
    ]
  ),
  generateStateConfig: registerOptions(GenerateStateSchema, [
    {
      field: 'generateFullName',
      label: 'Generate Full Name',
      description:
        'Enable to return the full state name with a capitalized first letter. Returns the 2-letter state code by default.',
    },
  ]),
  generateUuidConfig: registerOptions(GenerateUuidSchema, [
    {
      field: 'includeHyphens',
      label: 'Include hyphens',
      description:
        'Set to true to include hyphens in the generated UUID. Note: some databases such as Postgres automatically convert UUIDs with no hyphens to have hyphens when they store the data.',
    },
  ]),
  transformE164PhoneNumberConfig: registerOptions(
    TransformE164PhoneNumberSchema,
    [
      {
        field: 'preserveLength',
        label: 'Preserve Length',
        description:
          'Set the length of the output e164 phone number to be the same as the input e164 phone number.',
      },
    ]
  ),
  transformEmailConfig: registerOptions(TransformEmailSchema, [
    {
      field: 'preserveLength',
      label: 'Preserve Length',
      description:
        'Set the length of the output email to be the same as the input',
    },
    {
      field: 'preserveDomain',
      label: 'Preserve Domain',
      description:
        'Preserve the input domain including top level domain to the output value. For ex. if the input is john@gmail.com, the output will be ij23o@gmail.com',
    },
    {
      field: 'excludedDomains',
      label: 'Excluded Domains',
      description:
        'Provide a list of comma-separated domains that you want to be excluded from the transformer. Do not provide an @ with the domains.',
    },
    {
      field: 'emailType',
      label: 'Email Type',
      description:
        'Configure the email type that will be used during transformation.',
      formatEnumValue: getGenerateEmailTypeString,
    },
    {
      field: 'invalidEmailAction',
      label: 'Invalid Email Action',
      description:
        'Configure the invalid email action that will be run in the event the system encounters an email that does not conform to RFC 5322.',
      formatEnumValue: getInvalidEmailActionString,
    },
  ]),
  generateEmailConfig: registerOptions(GenerateEmailSchema, [
    {
      field: 'emailType',
      label: 'Email Type',
      description:
        'Select the type of email you want to generate. Uuid_v4 emails guarantee uniqueness.',
      formatEnumValue: getGenerateEmailTypeString,
    },
  ]),
  transformFirstNameConfig: registerOptions(TransformFirstNameSchema, [
    {
      field: 'preserveLength',
      label: 'Preserve Length',
      description:
        'Set the length of the output first name to be the same as the input',
    },
  ]),
  transformFloat64Config: registerOptions(TransformFloat64Schema, [
    {
      field: 'randomizationRangeMin',
      label: 'Relative Minimum Range Value',
      description:
        'Sets a relative minimum lower range value. This will create a lowerbound around the source input value. For example, if the input value is 10, and you set this value to 5, then the minimum range will be 5 (10 - 5 = 5).',
    },
    {
      field: 'randomizationRangeMax',
      label: 'Relative Maximum Range Value',
      description:
        'Sets a relative maximum upper range value. This will create an upperbound around the source input value. For example, if the input value is 10, and you set this value to 5, then the maximum range will be 15 (10 + 5 = 15).',
    },
  ]),
  transformFullNameConfig: registerOptions(TransformFullNameSchema, [
    {
      field: 'preserveLength',
      label: 'Preserve Length',
      description:
        'Generates a full name which has the same first name and last name length as the input first and last names',
    },
  ]),
  transformInt64Config: registerOptions(TransformInt64Schema, [
    {
      field: 'randomizationRangeMin',
      label: 'Relative Minimum Range Value',
      description:
        'Sets a relative minimum lower range value. This will create a lowerbound around the source input value. For example, if the input value is 10, and you set this value to 5, then the minimum range will be 5 (10 - 5 = 5).',
    },
    {
      field: 'randomizationRangeMax',
      label: 'Relative Maximum Range Value',
      description:
        'Sets a relative maximum upper range value. This will create an upperbound around the source input value. For example, if the input value is 10, and you set this value to 5, then the maximum range will be 15 (10 + 5 = 15).',
    },
  ]),
  transformInt64PhoneNumberConfig: registerOptions(
    TransformInt64PhoneNumberSchema,
    [
      {
        field: 'preserveLength',
        label: 'Preserve Length',
        description:
          'Set the length of the output phone number to be the same as the input',
      },
    ]
  ),
  transformLastNameConfig: registerOptions(TransformLastNameSchema, [
    {
      field: 'preserveLength',
      label: 'Preserve Length',
      description:
        'Set the length of the output last name to be the same as the input',
    },
  ]),
  transformPhoneNumberConfig: registerOptions(TransformPhoneNumberSchema, [
    {
      field: 'preserveFormat',
      label: 'Preserve Format',
      description:
        'Keep the prefix (06, +33 6), separators and length of the input, and replace the other digits. Two distinct numbers never give the same output, and the same number always gives the same one.',
    },
    {
      field: 'preserveLength',
      label: 'Preserve Length',
      description:
        'Set the length of the output phone number to be the same as the input. Implied by Preserve Format.',
      disabledWhen: (value) => value.preserveFormat === true,
    },
  ]),
  transformStringConfig: registerOptions(TransformStringSchema, [
    {
      field: 'preserveLength',
      label: 'Preserve Length',
      description:
        'Set the length of the output string to be the same as the input',
    },
  ]),
  generateCountryConfig: registerOptions(GenerateCountrySchema, [
    {
      field: 'generateFullName',
      label: 'Generate Full Name',
      description:
        'Enable to return the full country name otherwise it returns the 2-letter country code by default.',
    },
  ]),
  generateIpAddressConfig: registerOptions(GenerateIpAddressSchema, [
    {
      field: 'ipType',
      label: 'IP Version',
      description: 'Select if you want to generate an IPv4 or IPv6 address.',
      formatEnumValue: getGenerateIpAddressVersionString,
    },
  ]),
};
