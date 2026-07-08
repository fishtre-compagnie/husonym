import { Badge } from '@/components/ui/badge';
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip';
import { ExclamationTriangleIcon } from '@radix-ui/react-icons';
import { ReactElement } from 'react';

interface Props {
  category: string;
}

// Small warning indicator shown on rows that were flagged as containing PII by the
// AI recommendation pipeline but are still mapped to the Passthrough transformer.
export default function PiiRecommendationBadge(props: Props): ReactElement {
  const { category } = props;
  return (
    <TooltipProvider>
      <Tooltip delayDuration={200}>
        <TooltipTrigger type="button">
          <Badge
            variant="outline"
            className="text-xs bg-amber-100 text-amber-900 border-amber-300 cursor-default dark:bg-amber-900/40 dark:text-amber-200 dark:border-amber-700"
          >
            <ExclamationTriangleIcon className="h-3 w-3" />
          </Badge>
        </TooltipTrigger>
        <TooltipContent>
          Detected as potential PII (&quot;{category}&quot;) but currently left
          as Passthrough.
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}
