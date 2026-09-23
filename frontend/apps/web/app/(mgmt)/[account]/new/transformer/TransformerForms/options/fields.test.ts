import { create } from '@bufbuild/protobuf';
import {
  GenerateCategoricalSchema,
  GenerateEmailSchema,
  GenerateEmailType,
  GenerateFloat64Schema,
  GenerateIpAddressSchema,
  GenerateIpAddressType,
  GenerateStringPhoneNumberSchema,
  TransformEmailSchema,
  TransformPhoneNumberSchema,
} from '@husonym/sdk';
import {
  formatNumber,
  formatStringList,
  getEnumChoices,
  getOptionField,
  getOptionKind,
  parseStringList,
  readOptionValue,
  toFieldInteger,
  withOptionValue,
} from './fields';

describe('getOptionKind', () => {
  it.each([
    [getOptionField(TransformPhoneNumberSchema, 'preserveLength'), 'bool'],
    [getOptionField(GenerateStringPhoneNumberSchema, 'min'), 'integer'],
    [getOptionField(GenerateFloat64Schema, 'min'), 'float'],
    [getOptionField(GenerateCategoricalSchema, 'categories'), 'string'],
    [getOptionField(GenerateEmailSchema, 'emailType'), 'enum'],
    [getOptionField(TransformEmailSchema, 'excludedDomains'), 'stringList'],
  ])('reads the control from the descriptor of %s', (field, kind) => {
    expect(getOptionKind(field)).toBe(kind);
  });
});

describe('getOptionField', () => {
  it('fails loudly on a field the message does not have', () => {
    expect(() =>
      // @ts-expect-error the name is checked at compile time too
      getOptionField(TransformPhoneNumberSchema, 'preserveEverything')
    ).toThrow('has no field "preserveEverything"');
  });
});

describe('toFieldInteger', () => {
  it('writes a bigint into an int64 field', () => {
    const field = getOptionField(GenerateStringPhoneNumberSchema, 'min');
    expect(toFieldInteger(field, 12)).toBe(BigInt(12));
  });

  it('drops the decimals a number input may hold', () => {
    const field = getOptionField(GenerateStringPhoneNumberSchema, 'max');
    expect(toFieldInteger(field, 12.9)).toBe(BigInt(12));
  });
});

describe('formatNumber', () => {
  it.each([
    [BigInt(9), '9'],
    [0, '0'],
    [1.5, '1.5'],
    [undefined, ''],
  ])('shows %p as %p', (value, shown) => {
    expect(formatNumber(value)).toBe(shown);
  });
});

describe('string lists', () => {
  it('ignores blanks and spaces around items', () => {
    expect(parseStringList(' gmail.com, ,yahoo.fr ,')).toEqual([
      'gmail.com',
      'yahoo.fr',
    ]);
  });

  it('round-trips through its text form', () => {
    const list = ['gmail.com', 'yahoo.fr'];
    expect(parseStringList(formatStringList(list))).toEqual(list);
  });
});

describe('getEnumChoices', () => {
  const field = getOptionField(GenerateEmailSchema, 'emailType');

  it('leaves UNSPECIFIED out', () => {
    expect(getEnumChoices(field).map((c) => c.number)).not.toContain(
      GenerateEmailType.UNSPECIFIED
    );
    expect(getEnumChoices(field).map((c) => c.number)).toContain(
      GenerateEmailType.UUID_V4
    );
  });

  it('labels a value by its name unless told otherwise', () => {
    expect(getEnumChoices(field)).toContainEqual({
      number: GenerateEmailType.UUID_V4,
      label: 'Uuid v4',
    });
    expect(getEnumChoices(field, (n) => `#${n}`)).toContainEqual({
      number: GenerateEmailType.UUID_V4,
      label: `#${GenerateEmailType.UUID_V4}`,
    });
  });

  it('humanizes a screaming-case name', () => {
    expect(
      getEnumChoices(getOptionField(GenerateIpAddressSchema, 'ipType'))
    ).toContainEqual({
      number: GenerateIpAddressType.V4_PRIVATE_A,
      label: 'V4 private a',
    });
  });
});

describe('withOptionValue', () => {
  it('replaces one field and leaves the input message untouched', () => {
    const before = create(GenerateFloat64Schema, {
      min: 1,
      max: 10,
      randomizeSign: false,
    });
    const after = withOptionValue(
      GenerateFloat64Schema,
      before,
      'randomizeSign',
      true
    );

    expect(after.$typeName).toBe(GenerateFloat64Schema.typeName);
    expect(readOptionValue(after, 'randomizeSign')).toBe(true);
    expect(after.max).toBe(10);
    expect(before.randomizeSign).toBe(false);
  });
});
