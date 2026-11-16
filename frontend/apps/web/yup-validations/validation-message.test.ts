import * as Yup from 'yup';
import { getBigIntValidator } from './bigint';
import { matchValidationMessage } from './validation-message';

// The messages a failed validation gives.
async function errorsOf(validation: Promise<unknown>): Promise<string[]> {
  try {
    await validation;
  } catch (err) {
    if (err instanceof Yup.ValidationError) {
      return err.errors;
    }
  }
  return [];
}

describe('matchValidationMessage', () => {
  it('finds a fixed message, whatever its final period', () => {
    expect(matchValidationMessage('The database name is required')).toEqual({
      key: 'connection.databaseNameRequired',
      values: {},
    });
    expect(matchValidationMessage('The database name is required.')?.key).toBe(
      'connection.databaseNameRequired'
    );
  });

  it('reads back the parts that vary', () => {
    expect(matchValidationMessage('Must be less than or equal to 200')).toEqual(
      { key: 'common.mustBeLte', values: { max: '200' } }
    );
    expect(
      matchValidationMessage(
        'A default transformer config must be provided for the byte data type'
      )
    ).toEqual({
      key: 'job.defaultTransformerRequired',
      values: { type: 'byte' },
    });
  });

  it('prefers a schema message to the yup default it looks like', () => {
    expect(matchValidationMessage('Name is a required field')?.key).toBe(
      'common.nameRequiredField'
    );
    expect(
      matchValidationMessage('Value must be greater than or equal to 10')?.key
    ).toBe('common.valueGte');
  });

  it("finds yup's own messages", async () => {
    const schema = Yup.object({
      email: Yup.string().email(),
      port: Yup.number().integer().positive().required(),
    });
    const errors = await errorsOf(
      schema.validate({ email: 'nope', port: 1.5 }, { abortEarly: false })
    );
    const keys = errors.map((m) => matchValidationMessage(m)?.key);
    expect(keys).toEqual(['defaults.email', 'defaults.integer']);
    const [typeError] = await errorsOf(Yup.number().validate('abc'));
    expect(matchValidationMessage(typeError)?.key).toBe('defaults.notType');
  });

  it('finds the messages built from a value', async () => {
    const [message] = await errorsOf(
      getBigIntValidator({ range: [10, 100] }).validate(BigInt(5))
    );
    expect(matchValidationMessage(message)).toEqual({
      key: 'common.valueGte',
      values: { min: '10' },
    });
  });

  it('leaves other messages alone', () => {
    expect(matchValidationMessage('Something the server said')).toBeUndefined();
  });
});
