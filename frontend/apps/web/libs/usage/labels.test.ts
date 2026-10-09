import { RunErrorCategory, RunErrorStep } from '@husonym/sdk';
import { errorCategoryLabel, errorStepLabel } from './labels';

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

const STEPS = Object.values(RunErrorStep).filter(
  (value): value is RunErrorStep => typeof value === 'number'
);

describe('errorStepLabel', () => {
  it('names the six steps', () => {
    expect(
      STEPS.filter((step) => step !== RunErrorStep.UNSPECIFIED).map(
        errorStepLabel
      )
    ).toEqual([
      'Preflight',
      'Schema initialization',
      'Table sync',
      'Hooks',
      'Integrity check',
      'Other',
    ]);
  });

  it('has a label of its own for every member of the enum', () => {
    const named = STEPS.filter(
      (step) => step !== RunErrorStep.UNSPECIFIED && step !== RunErrorStep.OTHER
    );
    expect(named).toHaveLength(5);
    const labels = named.map(errorStepLabel);
    for (const label of labels) {
      expect(label).not.toBe('');
      expect(label).not.toBe('Other');
    }
    expect(new Set(labels).size).toBe(labels.length);
  });

  it('reads a step it does not know as Other', () => {
    expect(errorStepLabel(RunErrorStep.UNSPECIFIED)).toBe('Other');
    expect(errorStepLabel(99 as RunErrorStep)).toBe('Other');
  });
});
