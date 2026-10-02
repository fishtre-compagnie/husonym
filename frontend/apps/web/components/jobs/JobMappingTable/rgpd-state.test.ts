import { PiiConfidence } from '@husonym/sdk';
import { rgpdRank, rgpdState } from './rgpd-state';

describe('rgpdState', () => {
  it('says nothing of a column nothing is known of', () => {
    expect(rgpdState({ isSensitive: false })).toBe('none');
  });

  it('tells a column whose content could not be analyzed, when its name says nothing', () => {
    expect(rgpdState({ isSensitive: false, contentNotAnalyzed: true })).toBe(
      'not_analyzed'
    );
  });

  it('lets the name of a column speak when its content could not be analyzed', () => {
    expect(
      rgpdState({
        isSensitive: true,
        confidence: PiiConfidence.CONFIRMED,
        isAnonymized: true,
        contentNotAnalyzed: true,
      })
    ).toBe('compliant');
    expect(
      rgpdState({
        isSensitive: true,
        confidence: PiiConfidence.CONFIRMED,
        isAnonymized: false,
        contentNotAnalyzed: true,
      })
    ).toBe('not_anonymized');
  });

  it('puts a doubt on the detection before the rest', () => {
    expect(
      rgpdState({
        isSensitive: true,
        confidence: PiiConfidence.NEEDS_REVIEW,
        isAnonymized: false,
      })
    ).toBe('review');
    expect(
      rgpdState({ isSensitive: false, confidence: PiiConfidence.NEEDS_REVIEW })
    ).toBe('review');
  });

  it('tells a personal column left as it is', () => {
    expect(
      rgpdState({
        isSensitive: true,
        confidence: PiiConfidence.CONFIRMED,
        isAnonymized: false,
      })
    ).toBe('not_anonymized');
  });
});

describe('rgpdRank', () => {
  it('puts the columns that need a decision first', () => {
    expect(rgpdRank('not_anonymized')).toBeGreaterThan(rgpdRank('review'));
    expect(rgpdRank('not_analyzed')).toBe(rgpdRank('review'));
    expect(rgpdRank('review')).toBeGreaterThan(rgpdRank('compliant'));
    expect(rgpdRank('compliant')).toBeGreaterThan(rgpdRank('none'));
  });
});
