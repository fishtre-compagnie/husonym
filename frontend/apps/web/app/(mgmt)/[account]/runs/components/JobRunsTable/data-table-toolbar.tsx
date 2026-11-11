'use client';

import { Cross2Icon, ReloadIcon } from '@radix-ui/react-icons';
import { ReactTable, RowData } from '@tanstack/react-table';

import { Segmented } from '@/components/ds';
import { AppTableFeatures } from '@/components/table/features';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';

import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { cn } from '@/libs/utils';
import { JobRunStatus as JobRunStatusEnum } from '@husonym/sdk';
import { DataTableViewOptions } from './data-table-view-options';

export type RunStatusGroup = 'all' | 'live' | 'complete' | 'failed' | 'stopped';

export const STATUS_GROUPS: Record<
  Exclude<RunStatusGroup, 'all'>,
  JobRunStatusEnum[]
> = {
  live: [JobRunStatusEnum.RUNNING, JobRunStatusEnum.PENDING],
  complete: [JobRunStatusEnum.COMPLETE],
  failed: [
    JobRunStatusEnum.ERROR,
    JobRunStatusEnum.FAILED,
    JobRunStatusEnum.TIMED_OUT,
  ],
  stopped: [JobRunStatusEnum.CANCELED, JobRunStatusEnum.TERMINATED],
};

interface DataTableToolbarProps<
  TData extends RowData,
  TAutoRefreshInterval extends string,
> {
  table: ReactTable<AppTableFeatures, TData>;
  onRefreshClick(): void;
  refreshInterval: TAutoRefreshInterval;
  autoRefreshIntervalOptions: TAutoRefreshInterval[];
  onAutoRefreshIntervalChange(interval: TAutoRefreshInterval): void;
  isRefreshing: boolean;
  statusCounts: Record<RunStatusGroup, number>;
}

export function DataTableToolbar<
  TData extends RowData,
  TAutoRefreshInterval extends string,
>({
  table,
  onRefreshClick,
  refreshInterval,
  autoRefreshIntervalOptions,
  onAutoRefreshIntervalChange,
  isRefreshing,
  statusCounts,
}: DataTableToolbarProps<TData, TAutoRefreshInterval>) {
  const isFiltered = table.state.columnFilters.length > 0;
  const statusFilter = table.getColumn('status')?.getFilterValue() as
    JobRunStatusEnum[] | undefined;
  const group = toGroup(statusFilter);

  function onGroupChange(next: RunStatusGroup): void {
    table
      .getColumn('status')
      ?.setFilterValue(next === 'all' ? undefined : STATUS_GROUPS[next]);
  }

  const groups: { value: RunStatusGroup; label: string }[] = [
    { value: 'all', label: 'All' },
    { value: 'live', label: 'Running' },
    { value: 'complete', label: 'Complete' },
    { value: 'failed', label: 'Failed' },
    { value: 'stopped', label: 'Stopped' },
  ];

  return (
    <div className="flex flex-wrap items-center gap-2 border-b border-line-soft px-5 py-3">
      <Segmented
        ariaLabel="Filter runs by status"
        value={group}
        onChange={onGroupChange}
        className="max-w-full overflow-x-auto"
        options={groups.map((g) => ({
          value: g.value,
          label: (
            <span className="flex items-center gap-1.5 whitespace-nowrap">
              {g.label}
              <span className="text-xs tabular-nums text-muted-foreground">
                {statusCounts[g.value]}
              </span>
            </span>
          ),
        }))}
      />
      <Input
        placeholder="Filter by job name…"
        aria-label="Filter by job name"
        value={(table.getColumn('jobName')?.getFilterValue() as string) ?? ''}
        onChange={(event) =>
          table.getColumn('jobName')?.setFilterValue(event.target.value)
        }
        className="h-9 w-full sm:w-[240px]"
      />
      {isFiltered && (
        <Button
          variant="ghost"
          onClick={() => table.resetColumnFilters()}
          className="h-9 px-3"
        >
          Reset
          <Cross2Icon className="h-4 w-4" />
        </Button>
      )}
      <div className="ml-auto flex items-center gap-2">
        <Select
          onValueChange={onAutoRefreshIntervalChange}
          defaultValue={refreshInterval}
        >
          <SelectTrigger
            className="h-9 w-[132px]"
            aria-label="Auto-refresh interval"
          >
            <span className="text-muted-foreground">Refresh</span>
            <SelectValue placeholder="Select auto-refresh interval" />
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              <SelectLabel>Auto-refresh interval</SelectLabel>
              {autoRefreshIntervalOptions.map((interval, index) => {
                return (
                  <SelectItem key={`${interval}-${index}`} value={interval}>
                    {interval}
                  </SelectItem>
                );
              })}
            </SelectGroup>
          </SelectContent>
        </Select>
        <Button
          variant="outline"
          size="icon"
          className="h-9 w-9"
          aria-label="Refresh runs"
          onClick={() => onRefreshClick()}
        >
          <ReloadIcon
            className={cn('h-4 w-4', isRefreshing && 'animate-spin')}
          />
        </Button>
        <DataTableViewOptions table={table} />
      </div>
    </div>
  );
}

function toGroup(filter?: JobRunStatusEnum[]): RunStatusGroup {
  if (!filter) {
    return 'all';
  }
  for (const [key, statuses] of Object.entries(STATUS_GROUPS)) {
    if (statuses === filter) {
      return key as RunStatusGroup;
    }
  }
  return 'all';
}
