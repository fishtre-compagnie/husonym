import {
  create,
  DescField,
  DescMessage,
  MessageInitShape,
  MessageShape,
  ScalarType,
} from '@bufbuild/protobuf';

// The control an option gets, read from the protobuf descriptor of its field.
export type OptionKind =
  'bool' | 'integer' | 'float' | 'string' | 'enum' | 'stringList';

// Keys of a generated message that carry configuration, not protobuf bookkeeping.
export type OptionKey<T> = Exclude<keyof T, '$typeName' | '$unknown'> & string;

export type OptionFieldName<S extends DescMessage> = OptionKey<MessageShape<S>>;

export interface EnumChoice {
  number: number;
  label: string;
}

const INT64_SCALARS: ReadonlySet<ScalarType> = new Set([
  ScalarType.INT64,
  ScalarType.UINT64,
  ScalarType.SINT64,
  ScalarType.FIXED64,
  ScalarType.SFIXED64,
]);

const INT32_SCALARS: ReadonlySet<ScalarType> = new Set([
  ScalarType.INT32,
  ScalarType.UINT32,
  ScalarType.SINT32,
  ScalarType.FIXED32,
  ScalarType.SFIXED32,
]);

export function getOptionField<S extends DescMessage>(
  schema: S,
  name: OptionFieldName<S>
): DescField {
  const field = schema.field[name];
  if (!field) {
    throw new Error(`${schema.typeName} has no field "${name}"`);
  }
  return field;
}

export function getOptionKind(field: DescField): OptionKind {
  switch (field.fieldKind) {
    case 'scalar':
      if (field.scalar === ScalarType.BOOL) return 'bool';
      if (field.scalar === ScalarType.STRING) return 'string';
      if (
        field.scalar === ScalarType.DOUBLE ||
        field.scalar === ScalarType.FLOAT
      )
        return 'float';
      if (INT64_SCALARS.has(field.scalar) || INT32_SCALARS.has(field.scalar))
        return 'integer';
      break;
    case 'enum':
      return 'enum';
    case 'list':
      if (field.listKind === 'scalar' && field.scalar === ScalarType.STRING)
        return 'stringList';
      break;
  }
  throw new Error(
    `${field.parent.typeName}.${field.name} has no option control for its type`
  );
}

// Converts what a number input holds into the representation protobuf-es uses for
// the field: bigint for 64-bit integers (string under jstype = JS_STRING), number
// otherwise.
export function toFieldInteger(
  field: DescField,
  n: number
): bigint | string | number {
  const whole = Math.trunc(n);
  if (field.fieldKind === 'scalar' && INT64_SCALARS.has(field.scalar)) {
    return field.longAsString ? whole.toString() : BigInt(whole);
  }
  return whole;
}

export function formatNumber(value: unknown): string {
  if (typeof value === 'number' || typeof value === 'bigint') {
    return value.toString();
  }
  if (typeof value === 'string') {
    return value;
  }
  return '';
}

export function parseStringList(text: string): string[] {
  return text
    .split(',')
    .map((item) => item.trim())
    .filter((item) => item !== '');
}

export function formatStringList(list: readonly string[]): string {
  return list.join(', ');
}

// The values offered for an enum field. The zero value is left out: by protobuf
// convention it is UNSPECIFIED, which is not a choice.
export function getEnumChoices(
  field: DescField,
  formatValue?: (value: number) => string
): EnumChoice[] {
  if (field.fieldKind !== 'enum') {
    throw new Error(`${field.parent.typeName}.${field.name} is not an enum`);
  }
  return field.enum.values
    .filter((value) => value.number !== 0)
    .map((value) => ({
      number: value.number,
      label: formatValue
        ? formatValue(value.number)
        : humanizeEnumName(value.localName),
    }));
}

// UUID_V4 → "Uuid v4"
export function humanizeEnumName(localName: string): string {
  const words = localName.toLowerCase().split('_').filter(Boolean);
  const sentence = words.join(' ');
  return sentence.charAt(0).toUpperCase() + sentence.slice(1);
}

export function readOptionValue<T extends object>(
  value: T,
  name: OptionKey<T>
): unknown {
  return (value as Record<string, unknown>)[name];
}

// Returns a new message with one field replaced; the input message is left untouched.
// S is inferred from the schema alone: letting the message take part widens it to
// DescMessage, whose field names are `never`.
export function withOptionValue<S extends DescMessage>(
  schema: S,
  value: NoInfer<MessageShape<S>>,
  name: OptionFieldName<S>,
  fieldValue: unknown
): MessageShape<S> {
  return create(schema, {
    ...value,
    [name]: fieldValue,
  } as MessageInitShape<S>);
}
