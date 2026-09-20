import FastTable from '@/components/FastTable/FastTable';
import {
  AppTableFeatures,
  unpaginatedTableFeatures,
} from '@/components/table/features';
import {
  ColumnDef,
  RowData,
  TableFeatures,
  useTable,
} from '@tanstack/react-table';
import { ReactElement } from 'react';
import { SubsetTableToolbar } from './SubsetTableToolbar';

declare module '@tanstack/react-table' {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  interface TableMeta<TFeatures extends TableFeatures, TData extends RowData> {
    subsetTable?: {
      onEdit(rowIndex: number, schema: string, table: string): void;
      onReset(rowIndex: number, schema: string, table: string): void;
      hasLocalChange(rowIndex: number, schema: string, table: string): boolean;
    };
  }
}

interface Props<TData extends RowData> {
  data: TData[];
  columns: ColumnDef<AppTableFeatures, TData>[];
  onEdit(rowIndex: number, schema: string, table: string): void;
  onReset(rowIndex: number, schema: string, table: string): void;
  hasLocalChange(rowIndex: number, schema: string, table: string): boolean;
  onBulkEdit(data: TData[], onClearSelection: () => void): void;
}

export default function SubsetTable<TData extends RowData>(
  props: Props<TData>
): ReactElement {
  const { data, columns, onEdit, onReset, hasLocalChange, onBulkEdit } = props;

  const table = useTable({
    features: unpaginatedTableFeatures,
    data,
    columns,
    enableRowSelection: true,
    initialState: {},
    meta: {
      subsetTable: {
        onEdit,
        onReset,
        hasLocalChange,
      },
    },
  });

  return (
    <div className="flex flex-col gap-4">
      <SubsetTableToolbar
        isFilterButtonDisabled={table.state.columnFilters.length === 0}
        onClearFilters={() => table.resetColumnFilters()}
        isBulkEditButtonDisabled={
          Object.keys(table.state.rowSelection).length <= 1
        }
        onBulkEditClick={() => {
          const selectedRows = table
            .getSelectedRowModel()
            .rows.map((row) => row.original);
          onBulkEdit(selectedRows, () => table.resetRowSelection());
        }}
      />
      <FastTable table={table} estimateRowSize={() => 33} rowOverscan={50} />
    </div>
  );
}
