'use client';

import { ColumnDef, RowData, useTable } from '@tanstack/react-table';

import FastTable from '@/components/FastTable/FastTable';
import {
  AppTableFeatures,
  unpaginatedTableFeatures,
} from '@/components/table/features';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { LogLevel } from '@husonym/sdk';

interface DataTableProps<TData extends RowData> {
  columns: ColumnDef<AppTableFeatures, TData>[];
  data: TData[];

  selectedLogLevel: LogLevel;
  setSelectedLogLevel(newval: LogLevel): void;
  isLoading: boolean;
}

export function DataTable<TData extends RowData>({
  columns,
  data,
  selectedLogLevel,
  setSelectedLogLevel,
}: DataTableProps<TData>) {
  const table = useTable({
    features: unpaginatedTableFeatures,
    data,
    columns,
    enableRowSelection: false,
  });

  return (
    <div className="space-y-4">
      <div className="flex lg:w-1/2 gap-2 flex-col lg:flex-row">
        <div className="flex flex-row gap-2 items-center">
          <div>
            <p className="font-light text-xs">Log Level</p>
          </div>
          <div className="flex w-full">
            <Select
              onValueChange={(value) =>
                setSelectedLogLevel(parseInt(value, 10))
              }
              value={selectedLogLevel.toString()}
            >
              <SelectTrigger>
                <SelectValue className="w-[500px]" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={LogLevel.UNSPECIFIED.toString()}>
                  {'Any'}
                </SelectItem>
                <SelectItem value={LogLevel.INFO.toString()}>
                  {LogLevel[LogLevel.INFO]}
                </SelectItem>
                <SelectItem value={LogLevel.WARN.toString()}>
                  {LogLevel[LogLevel.WARN]}
                </SelectItem>
                <SelectItem value={LogLevel.ERROR.toString()}>
                  {LogLevel[LogLevel.ERROR]}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>
      </div>
      <FastTable
        table={table}
        estimateRowSize={() => 33}
        rowOverscan={100}
        bodyRow={{ disableTdWidth: true }}
        headerRow={{ disableThWidth: true }}
      />
    </div>
  );
}
