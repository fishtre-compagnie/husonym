import { AppTableFeatures } from '@/components/table/features';
import { Column, RowData } from '@tanstack/react-table';
import {
  ArrowDownIcon,
  ArrowUpIcon,
  CaretSortIcon,
} from '@radix-ui/react-icons';
import { ReactElement } from 'react';

interface Props<TData extends RowData, TValue> {
  column: Column<AppTableFeatures, TData, TValue>;
  title: string;
}

// The head of the personal-data column. It has no search box: its value is a key
// for the memoized cells, not words, and the list is filtered by state from the
// bar above it. Sorting it downwards brings up the columns that need a decision.
export default function RgpdColumnHeader<TData extends RowData, TValue>({
  column,
  title,
}: Props<TData, TValue>): ReactElement {
  const sorted = column.getIsSorted();
  return (
    <div className="flex w-full min-w-0 items-center gap-1">
      <span className="truncate text-xs">{title}</span>
      {column.getCanSort() && (
        <button
          type="button"
          className="grid size-7 shrink-0 cursor-pointer place-items-center rounded-md text-muted-foreground hover:bg-hover hover:text-foreground"
          aria-label={
            sorted === 'desc'
              ? `Sorted by ${title}, the columns to decide first. Clear the sort`
              : sorted === 'asc'
                ? `Sorted by ${title}, ascending. Sort the columns to decide first`
                : `Sort by ${title}`
          }
          title={`Sort by ${title}`}
          onClick={(e) => {
            if (!sorted) {
              column.toggleSorting(false, e.shiftKey);
            } else if (sorted === 'asc') {
              column.toggleSorting(true, e.shiftKey);
            } else {
              column.clearSorting();
            }
          }}
        >
          {sorted === 'desc' ? (
            <ArrowDownIcon className="size-4" />
          ) : sorted === 'asc' ? (
            <ArrowUpIcon className="size-4" />
          ) : (
            <CaretSortIcon className="size-4" />
          )}
        </button>
      )}
    </div>
  );
}
