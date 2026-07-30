import { Column } from '@tanstack/react-table';

import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip';
import { cn } from '@/libs/utils';
import {
  ArrowDownIcon,
  ArrowUpIcon,
  CaretSortIcon,
} from '@radix-ui/react-icons';
import { ReactElement, useState } from 'react';
import { FaSearch } from 'react-icons/fa';
import { useDebounceCallback } from 'usehooks-ts';

interface DataTableColumnHeaderProps<
  TData,
  TValue,
> extends React.HTMLAttributes<HTMLDivElement> {
  column: Column<TData, TValue>;
  title: string;
}

export function SchemaColumnHeader<TData, TValue>({
  column,
  title,
  className,
}: DataTableColumnHeaderProps<TData, TValue>) {
  const [inputValue, setInputValue] = useState(
    (column.getFilterValue() ?? '') as string
  );
  const onInputChange = useDebounceCallback(
    (value) => column.setFilterValue(value),
    300
  );
  return (
    <div className="flex w-full min-w-0 flex-row items-center gap-1 justify-start">
      {column.getCanFilter() && (
        <div className="min-w-0 flex-1">
          <div className="relative">
            {/* Loupe à DROITE, et non à gauche : le libellé de la colonne est le
                placeholder de ce champ. Une icône à gauche le décalait d'environ
                25px, si bien que « Schema » ne tombait pas au-dessus de
                « public ». Bord gauche libéré, le texte s'aligne sur les valeurs
                de la colonne (cf. le padding des cellules dans MemoizedRow). */}
            <Input
              type="text"
              value={inputValue}
              onChange={(e) => {
                setInputValue(e.target.value);
                onInputChange(e.target.value);
              }}
              placeholder={title}
              title={title}
              className="border border-gray-300 dark:border-gray-700 bg-white dark:bg-transparent text-xs h-8 w-full pl-2 pr-6"
            />
            <FaSearch className="absolute right-1.5 top-1/2 h-3 w-3 -translate-y-1/2 transform text-gray-400" />
          </div>
        </div>
      )}
      {!column.getCanFilter() && (
        <span className={cn(className, 'text-xs')}>{title}</span>
      )}
      {column.getCanSort() && (
        <SortButtonWithTooltip
          column={column}
          tooltip={getColumnTooltip(title, column.getCanMultiSort())}
        />
      )}
    </div>
  );
}

function getColumnTooltip(title: string, canMultiSort: boolean): string {
  if (canMultiSort) {
    return `Sort by ${title}, hold shift to multi-sort`;
  }
  return `Sort by ${title}`;
}

interface SortButtonWithTooltipProps<TData, TValue> {
  column: Column<TData, TValue>;
  tooltip: string;
}

function SortButtonWithTooltip<TData, TValue>({
  column,
  tooltip,
}: SortButtonWithTooltipProps<TData, TValue>): ReactElement {
  return (
    <TooltipProvider>
      <Tooltip delayDuration={200}>
        <TooltipTrigger asChild>
          <Button
            type="button"
            onClick={(e) => {
              const sorted = column.getIsSorted();
              if (!sorted) {
                column.toggleSorting(false, e.shiftKey);
              } else if (sorted === 'asc') {
                column.toggleSorting(true, e.shiftKey);
              } else if (sorted === 'desc') {
                column.clearSorting();
              }
            }}
            variant="ghost"
            className="h-8 w-6 shrink-0 px-0"
          >
            {column.getIsSorted() === 'desc' ? (
              <ArrowDownIcon className="h-4 w-4" />
            ) : column.getIsSorted() === 'asc' ? (
              <ArrowUpIcon className="h-4 w-4" />
            ) : (
              <CaretSortIcon className="h-4 w-4" />
            )}
          </Button>
        </TooltipTrigger>
        <TooltipContent>{tooltip}</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}
