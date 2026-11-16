import { TransformerHandler } from '@/components/jobs/SchemaTable/transformer-handler';
import { Transformer } from '@/shared/transformers';
import { JobMappingTransformerForm } from '@/yup-validations/jobs';
import { create } from '@bufbuild/protobuf';
import {
  AccountRole,
  AwsS3DestinationConnectionOptions_StorageClass,
  GenerateEmailType,
  GenerateIpAddressType,
  InvalidEmailAction,
  SupportedJobType,
  SystemTransformer,
  SystemTransformerSchema,
  TransformerDataType,
  TransformerSource,
  UserDefinedTransformer,
} from '@husonym/sdk';
import { format } from 'date-fns';
import type { Locale } from '@/i18n/config';

// A date and time, written the way the UI language writes them: in English
// 04/16/2024 02:30:00 PM (or 14:30:00 with is24Hour), in French 16/04/2024 14:30:00,
// the French clock being a 24-hour one. Components get it bound to the UI language
// from useFormatDateTime (i18n/date-fns.ts).
export function formatDateTime(
  dateStr?: string | Date | number,
  is24Hour = false,
  locale: Locale = 'en'
): string | undefined {
  if (!dateStr) {
    return undefined;
  }

  // checks to make sure that the string can be transformed into a valid date
  const date = new Date(dateStr);

  if (isNaN(date.getTime())) {
    return undefined;
  }

  if (locale === 'fr') {
    return format(date, 'dd/MM/yyyy HH:mm:ss');
  }
  const hourFormat = is24Hour ? 'HH' : 'hh';
  const amPm = is24Hour ? '' : ' a';
  return format(date, `MM/dd/yyyy ${hourFormat}:mm:ss${amPm}`);
}

export function getErrorMessage(error: unknown): string {
  return isErrorWithMessage(error) ? error.message : 'unknown error message';
}

function isErrorWithMessage(error: unknown): error is { message: string } {
  return (
    typeof error === 'object' &&
    error != null &&
    'message' in error &&
    typeof error.message === 'string'
  );
}

export const toTitleCase = (str: string) => {
  return str.toLowerCase().replace(/\b\w/g, (s) => s.toUpperCase());
};

const NANOS_PER_SECOND = BigInt(1000000000);
const SECONDS_PER_MIN = BigInt(60);

// if the duration is too large to convert to minutes, it will return the max safe integer
export function convertNanosecondsToMinutes(duration: bigint): number {
  // Convert nanoseconds to minutes
  const minutesBigInt = duration / NANOS_PER_SECOND / SECONDS_PER_MIN;

  // Check if the result is within the safe range for JavaScript numbers
  if (minutesBigInt <= BigInt(Number.MAX_SAFE_INTEGER)) {
    return Number(minutesBigInt);
  } else {
    // Handle the case where the number of minutes is too large
    console.warn(
      'The number of minutes is too large for a safe JavaScript number. Returning as BigInt.'
    );
    return Number.MAX_SAFE_INTEGER;
  }
}

// Convert minutes to BigInt to ensure precision in multiplication
export function convertMinutesToNanoseconds(minutes: number): bigint {
  const minutesBigInt = BigInt(minutes);
  return minutesBigInt * SECONDS_PER_MIN * NANOS_PER_SECOND;
}

function getTransformerDataTypeString(dt: TransformerDataType): string {
  const value = TransformerDataType[dt];
  return value ? value.toLowerCase() : 'unspecified';
}

export function getTransformerDataTypesString(
  dts: TransformerDataType[]
): string {
  return dts.map((dt) => getTransformerDataTypeString(dt)).join(' | ');
}

// What a job type reads as, by the words of common.jobTypes; without them, its name in
// title case, in English.
export type JobTypeLabels = Partial<Record<'sync' | 'generate', string>>;

function getTransformerJobTypeString(
  dt: SupportedJobType,
  labels: JobTypeLabels = {}
): string {
  switch (dt) {
    case SupportedJobType.SYNC:
      return labels.sync ?? 'Sync';
    case SupportedJobType.GENERATE:
      return labels.generate ?? 'Generate';
  }
  const value = SupportedJobType[dt];
  return value ? toTitleCase(value) : 'unspecified';
}

export function getTransformerJobTypesString(
  dts: SupportedJobType[],
  labels?: JobTypeLabels
): string[] {
  return dts.map((dt) => getTransformerJobTypeString(dt, labels));
}

export function getTransformerSourceString(ds: TransformerSource): string {
  const value = TransformerSource[ds];
  return value ? value.toLowerCase() : 'unspecified';
}

export function getGenerateEmailTypeString(
  emailType: GenerateEmailType
): string {
  const value = GenerateEmailType[emailType];
  return value ? value.toLowerCase() : 'unknown';
}

export function getInvalidEmailActionString(
  invalidEmailAction: InvalidEmailAction
): string {
  const value = InvalidEmailAction[invalidEmailAction];
  return value ? value.toLowerCase() : 'unknown';
}

export function getAccountRoleString(role: AccountRole): string {
  const value = AccountRole[role];
  return value ? value.toLowerCase() : 'unknown';
}

export function getStorageClassString(
  storageclass: AwsS3DestinationConnectionOptions_StorageClass
): string {
  const value = AwsS3DestinationConnectionOptions_StorageClass[storageclass];
  return value ? value.toLowerCase() : 'unknown';
}

// Given the currently selected transformer mapping config, return the relevant Transformer
export function getTransformerFromField(
  handler: TransformerHandler,
  value: JobMappingTransformerForm
): Transformer {
  if (value.config.case === 'userDefinedTransformerConfig') {
    return (
      handler.getUserDefinedTransformerById(value.config.value.id) ??
      create(SystemTransformerSchema)
    );
  }
  return (
    handler.getSystemTransformerByConfigCase(value.config.case) ??
    create(SystemTransformerSchema)
  );
}

// Checks to see if the config is unspecified
export function isInvalidTransformer(transformer: Transformer): boolean {
  return transformer.config == null;
}

export function getTransformerSelectButtonText(
  transformer: Transformer,
  defaultText: string = 'Transformer'
): string {
  return isInvalidTransformer(transformer) ? defaultText : transformer.name;
}

export function getFilterdTransformersByType(
  transformerHandler: TransformerHandler,
  datatype: TransformerDataType
): {
  system: SystemTransformer[];
  userDefined: UserDefinedTransformer[];
} {
  return transformerHandler.getFilteredTransformers({
    isForeignKey: false,
    isVirtualForeignKey: false,
    hasDefault: false,
    isNullable: true,
    isGenerated: false,
    dataType: datatype,
    jobType: SupportedJobType.SYNC,
  });
}

export function getGenerateIpAddressVersionString(
  type: GenerateIpAddressType
): string {
  const value = GenerateIpAddressType[type];
  return value ? value.toLowerCase() : 'unknown';
}
