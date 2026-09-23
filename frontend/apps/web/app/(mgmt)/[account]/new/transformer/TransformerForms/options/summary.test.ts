import { create } from '@bufbuild/protobuf';
import {
  GenerateEmailType,
  GenerateFirstNameSchema,
  TransformerConfigSchema,
} from '@husonym/sdk';
import { TRANSFORMER_OPTIONS } from './registry';
import { summarizeOptions } from './summary';

describe('summarizeOptions', () => {
  it('names a switch that is on, and leaves out one another makes moot', () => {
    const config = create(TransformerConfigSchema, {
      config: {
        case: 'transformPhoneNumberConfig',
        value: { preserveFormat: true, preserveLength: true },
      },
    });
    // Preserve Length is implied by Preserve Format: saying it too would be noise.
    expect(summarizeOptions(config)).toEqual(['Preserve Format']);
  });

  it('leaves out a switch that is off', () => {
    const config = create(TransformerConfigSchema, {
      config: {
        case: 'transformPhoneNumberConfig',
        value: { preserveFormat: false, preserveLength: false },
      },
    });
    expect(summarizeOptions(config)).toEqual([]);
  });

  it('gives numbers, the value chosen in a list, and a list of texts', () => {
    const config = create(TransformerConfigSchema, {
      config: {
        case: 'transformEmailConfig',
        value: {
          preserveDomain: true,
          excludedDomains: ['gmail.com', 'example.org'],
          emailType: GenerateEmailType.UUID_V4,
        },
      },
    });
    expect(summarizeOptions(config)).toEqual([
      'Preserve Domain',
      'Excluded Domains: gmail.com, example.org',
      'Email Type: uuid_v4',
    ]);

    const range = create(TransformerConfigSchema, {
      config: {
        case: 'generateStringPhoneNumberConfig',
        value: { min: BigInt(9), max: BigInt(12) },
      },
    });
    expect(summarizeOptions(range)).toEqual([
      'Minimum Length: 9',
      'Maximum Length: 12',
    ]);
  });

  it('says nothing about a transformer without options', () => {
    const config = create(TransformerConfigSchema, {
      config: {
        case: 'generateFirstNameConfig',
        value: create(GenerateFirstNameSchema),
      },
    });
    expect(summarizeOptions(config)).toEqual([]);
    expect(summarizeOptions(undefined)).toEqual([]);
  });

  it('every registered option names a field of its message', () => {
    // A field renamed in the proto would otherwise surface as a crash in the UI, not here.
    for (const registered of Object.values(TRANSFORMER_OPTIONS)) {
      for (const option of registered!.options) {
        expect(registered!.schema.field[option.field]).toBeDefined();
      }
    }
  });
});
