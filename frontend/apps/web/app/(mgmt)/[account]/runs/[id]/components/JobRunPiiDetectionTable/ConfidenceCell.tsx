import { Badge } from '@/components/ui/badge';
import { ReactElement } from 'react';

interface Props {
  confidence: number[];
}

export default function ConfidenceCell(props: Props): ReactElement {
  const { confidence } = props;

  const uniqueConfidences = dedupe(confidence);

  return (
    <span className="max-w-[500px] truncate font-medium">
      <div className="flex flex-col lg:flex-row items-start gap-1">
        {uniqueConfidences.map((confidence) => (
          <ConfidenceBadge key={confidence} confidence={confidence} />
        ))}
      </div>
    </span>
  );
}

function dedupe(arr: number[]): number[] {
  return [...Array.from(new Set(arr))];
}

interface ConfidenceBadgeProps {
  confidence: number;
}

function ConfidenceBadge(props: ConfidenceBadgeProps): ReactElement {
  const { confidence } = props;
  return (
    <Badge
      variant="outline"
      className="cursor-default border-transparent bg-brand-soft text-xs text-link"
    >
      {confidence}
    </Badge>
  );
}
