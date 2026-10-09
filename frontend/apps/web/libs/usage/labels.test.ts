import { RunErrorCategory } from '@husonym/sdk';
import { errorCategoryLabel } from './labels';

// The members of the generated enum: a numeric enum also maps its numbers back to
// their names, which are left out.
const CATEGORIES = Object.values(RunErrorCategory).filter(
  (value): value is RunErrorCategory => typeof value === 'number'
);

describe('errorCategoryLabel', () => {
  it('names the eleven categories', () => {
    expect(
      CATEGORIES.filter(
        (category) => category !== RunErrorCategory.UNSPECIFIED
      ).map(errorCategoryLabel)
    ).toEqual([
      'Connection refused',
      'Authentication refused',
      'Timeout',
      'Constraint violated',
      'Insufficient privileges',
      'Object missing',
      'Type mismatch',
      'Resources exhausted',
      'Canceled',
      'License',
      'Other',
    ]);
  });

  it('has a label of its own for every member of the enum', () => {
    const named = CATEGORIES.filter(
      (category) =>
        category !== RunErrorCategory.UNSPECIFIED &&
        category !== RunErrorCategory.OTHER
    );
    expect(named).toHaveLength(10);
    const labels = named.map(errorCategoryLabel);
    for (const label of labels) {
      expect(label).not.toBe('');
      expect(label).not.toBe('Other');
    }
    expect(new Set(labels).size).toBe(labels.length);
  });

  it('reads a category it does not know as Other', () => {
    expect(errorCategoryLabel(RunErrorCategory.UNSPECIFIED)).toBe('Other');
    expect(errorCategoryLabel(99 as RunErrorCategory)).toBe('Other');
  });
});
