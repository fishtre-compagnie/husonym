import { addsWhereClause } from './utils';

describe('addsWhereClause', () => {
  it('is true when a table has a WHERE clause', () => {
    expect(
      addsWhereClause([{ whereClause: undefined }, { whereClause: 'id > 10' }])
    ).toBe(true);
  });

  it('is false for subsets that clear every clause, blank ones included', () => {
    expect(addsWhereClause([])).toBe(false);
    expect(
      addsWhereClause([{ whereClause: undefined }, { whereClause: '  \n ' }])
    ).toBe(false);
  });
});
