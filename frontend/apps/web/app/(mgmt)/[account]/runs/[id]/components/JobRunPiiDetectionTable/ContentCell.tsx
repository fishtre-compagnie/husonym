import { Badge } from '@/components/ui/badge';
import { ReactElement } from 'react';

export interface ContentFinding {
  category: string;
  matchCount: number;
  sampledCount: number;
}

interface Props {
  finding?: ContentFinding;
}

export default function ContentCell(props: Props): ReactElement | null {
  const { finding } = props;

  if (!finding) {
    return null;
  }

  return (
    <span className="max-w-[500px] truncate font-medium">
      <div className="flex flex-col lg:flex-row items-start gap-1">
        <Badge
          variant="outline"
          className="text-xs bg-blue-100 text-gray-800 cursor-default dark:bg-blue-200 dark:text-gray-900"
        >
          {finding.category}
        </Badge>
        <Badge
          variant="outline"
          className="text-xs bg-blue-100 text-gray-800 cursor-default dark:bg-blue-200 dark:text-gray-900"
        >
          {formatContentCounts(finding)}
        </Badge>
      </div>
    </span>
  );
}

function formatContentCounts(finding: ContentFinding): string {
  return `${finding.matchCount}/${finding.sampledCount}`;
}

// The finding as plain text, for sorting and for the CSV export.
export function formatContentFinding(finding?: ContentFinding): string {
  return finding ? `${finding.category} ${formatContentCounts(finding)}` : '';
}
