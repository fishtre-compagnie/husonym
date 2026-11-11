import { Badge } from '@/components/ui/badge';
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip';
import { cn } from '@/libs/utils';
import { ReactElement } from 'react';

interface Props {
  isPrimaryKey: boolean;
  foreignKey: [boolean, string[]];
  virtualForeignKey: [boolean, string[]];
  isUnique: boolean;
}

export default function ConstraintsCell(props: Props): ReactElement {
  const { isPrimaryKey, foreignKey, virtualForeignKey, isUnique } = props;
  const [isForeignKey, fkCols] = foreignKey;
  const [isVirtualForeignKey, vfkCols] = virtualForeignKey;
  return (
    <span className="max-w-[500px] truncate font-medium">
      <div className="flex flex-col lg:flex-row items-start gap-1">
        {isPrimaryKey && (
          <div>
            <HoverBadge
              badgeText="P"
              tooltipContent="Primary Key"
              badgeClassName="bg-brand-soft text-link"
            />
          </div>
        )}
        {isForeignKey && (
          <div>
            <HoverBadge
              badgeText="FK"
              tooltipContent={`FK: ${fkCols
                .map((col) => `Primary Key: ${col}`)
                .join('\n')}`}
              badgeClassName="bg-warn-soft text-warn-fg"
            />
          </div>
        )}
        {isVirtualForeignKey && (
          <div>
            <HoverBadge
              badgeText="VFK"
              tooltipContent={`VFK: ${vfkCols
                .map((col) => `Primary Key: ${col}`)
                .join('\n')}`}
              badgeClassName="border-dashed bg-warn-soft text-warn-fg"
            />
          </div>
        )}
        {isUnique && (
          <div>
            <HoverBadge
              badgeText="U"
              tooltipContent="Unique Key"
              badgeClassName="bg-quiet-soft text-quiet-fg"
            />
          </div>
        )}
      </div>
    </span>
  );
}

interface HoverBadgeProps {
  badgeText: string;
  tooltipContent: string;
  badgeClassName?: string;
}

function HoverBadge(props: HoverBadgeProps): ReactElement {
  const { badgeText, tooltipContent, badgeClassName } = props;
  return (
    <TooltipProvider>
      <Tooltip delayDuration={200}>
        <TooltipTrigger>
          <Badge
            variant="outline"
            className={cn(
              'cursor-default border-transparent font-mono text-[11px]',
              badgeClassName
            )}
          >
            {badgeText}
          </Badge>
        </TooltipTrigger>
        <TooltipContent>{tooltipContent}</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}
