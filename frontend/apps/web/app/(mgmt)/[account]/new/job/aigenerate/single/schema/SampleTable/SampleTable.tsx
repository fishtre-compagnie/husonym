import {
  AppTableFeatures,
  unpaginatedTableFeatures,
} from '@/components/table/features';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { ColumnDef, useTable } from '@tanstack/react-table';
import { ReactElement } from 'react';
import { SampleRecord } from '../types';

interface Props {
  columns: ColumnDef<AppTableFeatures, SampleRecord>[];
  records: SampleRecord[];
}
export default function SampleTable(props: Props): ReactElement {
  const { records, columns } = props;

  const table = useTable({
    features: unpaginatedTableFeatures,
    data: records,
    columns: columns,
    enableRowSelection: false,
  });

  return (
    <section className="flex flex-col gap-3">
      <div className="flex flex-col gap-0.5">
        <h2 className="m-0 text-[15px] font-semibold">Synthetic data sample</h2>
        <p className="m-0 text-[13px] text-muted-foreground">
          A sample of synthetic data given the inputs above: the configured row
          count, up to 10 records.
        </p>
      </div>
      <div className="overflow-auto rounded-[14px] border border-line-soft bg-card">
        <Table>
          <TableHeader className="bg-surface-2">
            {table.getHeaderGroups().map((headerGroup) => (
              <TableRow key={headerGroup.id}>
                {headerGroup.headers.map((header) => {
                  return (
                    <TableHead key={header.id} className="pl-2">
                      {header.isPlaceholder ? null : (
                        <table.FlexRender header={header} />
                      )}
                    </TableHead>
                  );
                })}
              </TableRow>
            ))}
          </TableHeader>
          <TableBody>
            {table.getRowModel().rows?.length ? (
              table.getRowModel().rows.map((row) => (
                <TableRow
                  key={row.id}
                  data-state={row.getIsSelected() && 'selected'}
                >
                  {row.getVisibleCells().map((cell) => (
                    <TableCell key={cell.id}>
                      <table.FlexRender cell={cell} />
                    </TableCell>
                  ))}
                </TableRow>
              ))
            ) : (
              <TableRow>
                <TableCell
                  colSpan={columns.length}
                  className="h-24 text-center text-muted-foreground"
                >
                  Click Sample to generate a preview of synthetic data.
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>
    </section>
  );
}
