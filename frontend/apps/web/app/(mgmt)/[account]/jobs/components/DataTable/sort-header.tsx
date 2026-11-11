import { AppTableFeatures } from '@/components/table/features';
import { cn } from '@/libs/utils';
import {
  ArrowDownIcon,
  ArrowUpIcon,
  CaretSortIcon,
} from '@radix-ui/react-icons';
import { Column, RowData } from '@tanstack/react-table';
import { ReactElement } from 'react';

interface Props<TData extends RowData, TValue> {
  column: Column<AppTableFeatures, TData, TValue>;
  title: string;
  className?: string;
}

// SortHeader is the head of a column of the jobs list: a button that sorts by the column,
// ascending, descending, then back to the list's order.
export function SortHeader<TData extends RowData, TValue>({
  column,
  title,
  className,
}: Props<TData, TValue>): ReactElement {
  if (!column.getCanSort()) {
    return <span className={className}>{title}</span>;
  }
  const sorted = column.getIsSorted();
  return (
    <button
      type="button"
      onClick={column.getToggleSortingHandler()}
      className={cn(
        '-mx-1 inline-flex cursor-pointer items-center gap-1 rounded px-1 uppercase tracking-[0.06em] hover:text-foreground',
        sorted && 'text-foreground',
        className
      )}
    >
      {title}
      {sorted === 'desc' ? (
        <ArrowDownIcon className="size-3.5" aria-hidden="true" />
      ) : sorted === 'asc' ? (
        <ArrowUpIcon className="size-3.5" aria-hidden="true" />
      ) : (
        <CaretSortIcon className="size-3.5 opacity-60" aria-hidden="true" />
      )}
    </button>
  );
}
