import { TransformerDataType } from '@husonym/sdk';
import { dbDataTypeToTransformerDataType } from './schema-constraint-handler';

// The same cases as TransformerDataTypeOf in internal/job/datatype_test.go: what the job
// builder offers and what the API accepts are the same set.
describe('dbDataTypeToTransformerDataType', () => {
  it.each([
    ['character varying(40)', TransformerDataType.STRING],
    ['character(2)', TransformerDataType.STRING],
    ['bpchar', TransformerDataType.STRING],
    ['tinytext', TransformerDataType.STRING],
    ['timestamp without time zone', TransformerDataType.TIME],
    ['integer[]', TransformerDataType.ANY],
    ['tinyint', TransformerDataType.INT64],
    ['inet', TransformerDataType.UNSPECIFIED],
  ])('reads %s', (dataType, want) => {
    expect(dbDataTypeToTransformerDataType(dataType)).toBe(want);
  });
});
