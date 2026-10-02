import { PiiConfidence } from '@husonym/sdk';

// What the GDPR cell of a column says, and how much the column asks of the person.
//
//   * 'none'           — nothing known to be personal: nothing to do.
//   * 'compliant'      — personal and anonymized: nothing to do.
//   * 'not_analyzed'   — the content scan could not analyze the column, and its name says
//                        nothing: whether it holds personal data is not known.
//   * 'review'         — a detection that is not proven, to confirm or dismiss.
//   * 'not_anonymized' — personal and left as it is: it would leave in clear.
export type RgpdState =
  'none' | 'compliant' | 'not_analyzed' | 'review' | 'not_anonymized';

interface RgpdInput {
  isSensitive: boolean;
  confidence?: PiiConfidence;
  // False for Passthrough: the value is then copied as it is.
  isAnonymized?: boolean;
  // True when the content scan could not analyze the column.
  contentNotAnalyzed?: boolean;
}

export function rgpdState({
  isSensitive,
  confidence,
  isAnonymized,
  contentNotAnalyzed,
}: RgpdInput): RgpdState {
  // A doubt on the detection comes first: no use alarming on a missing transformer
  // if the column may not be personal.
  if (confidence === PiiConfidence.NEEDS_REVIEW) {
    return 'review';
  }
  if (isSensitive) {
    return isAnonymized === false ? 'not_anonymized' : 'compliant';
  }
  // The name of the column said nothing, and its content could not be read: an empty
  // cell would pass for a column without personal data.
  return contentNotAnalyzed ? 'not_analyzed' : 'none';
}

// Higher asks more of the person: sorted downwards, the columns that need a decision
// come first.
export function rgpdRank(state: RgpdState): number {
  switch (state) {
    case 'not_anonymized':
      return 3;
    case 'review':
    case 'not_analyzed':
      return 2;
    case 'compliant':
      return 1;
    case 'none':
      return 0;
  }
}
