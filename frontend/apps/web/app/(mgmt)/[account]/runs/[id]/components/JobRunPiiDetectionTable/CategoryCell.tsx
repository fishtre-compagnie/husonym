import { Badge } from '@/components/ui/badge';
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip';
import { ReactElement, useMemo } from 'react';

interface Props {
  categories: string[];
}

export default function CategoryCell(props: Props): ReactElement {
  const { categories } = props;

  const uniqueCategories = dedupe(categories);

  return (
    <span className="max-w-[500px] truncate font-medium">
      <div className="flex flex-col lg:flex-row items-start gap-1">
        {uniqueCategories.map((category) => (
          <CategoryBadge key={category} category={category} />
        ))}
      </div>
    </span>
  );
}

function dedupe(arr: string[]): string[] {
  return [...Array.from(new Set(arr))];
}

interface CategoryBadgeProps {
  category: string;
}

function CategoryBadge(props: CategoryBadgeProps): ReactElement {
  const { category } = props;
  const tooltip = useCategoryTooltip(category);
  return (
    <TooltipProvider>
      <Tooltip delayDuration={200}>
        <TooltipTrigger type="button">
          <Badge
            variant="outline"
            className="cursor-default border-transparent bg-brand-soft text-xs text-link"
          >
            {category}
          </Badge>
        </TooltipTrigger>
        <TooltipContent>{tooltip}</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}

function useCategoryTooltip(category: string): string {
  return useMemo(() => {
    switch (category) {
      case 'personal':
        return 'Personal information';
      case 'financial':
        return 'Financial information';
      default:
        return category;
    }
  }, [category]);
}
