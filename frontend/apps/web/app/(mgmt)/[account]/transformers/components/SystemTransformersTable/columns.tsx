'use client';

import { AppTableFeatures } from '@/components/table/features';
import { Pill } from '@/components/ds';
import {
  getTransformerDataTypesString,
  getTransformerJobTypesString,
  getTransformerSourceString,
  JobTypeLabels,
} from '@/util/util';
import { SystemTransformer } from '@husonym/sdk';
import { ColumnDef } from '@tanstack/react-table';
import { useTranslations } from 'next-intl';
import NextLink from 'next/link';
import { DataTableColumnHeader } from './data-table-column-header';
import { DataTableRowActions } from './data-table-row-actions';

interface GetSystemTransformercolumnsProps {
  accountName: string;
  t: ReturnType<typeof useTranslations<'connections.transformers.table'>>;
  jobTypeLabels?: JobTypeLabels;
}

export function getSystemTransformerColumns(
  props: GetSystemTransformercolumnsProps
): ColumnDef<AppTableFeatures, SystemTransformer>[] {
  const { accountName, t, jobTypeLabels } = props;
  return [
    {
      id: 'name',
      accessorKey: 'name',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('name')} />
      ),
      cell: ({ row }) => {
        return (
          <div className="flex space-x-2">
            <span className="max-w-[500px] truncate font-medium">
              <div>
                <NextLink
                  className="font-semibold text-foreground hover:text-hi"
                  href={`/${accountName}/transformers/systemTransformers/${row.original.source}`}
                >
                  {row.original.name}
                </NextLink>
              </div>
            </span>
          </div>
        );
      },
    },
    {
      id: 'types',
      accessorKey: 'types',
      accessorFn: (row) => getTransformerDataTypesString(row.dataTypes),
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('dataTypes')} />
      ),
      cell: ({ getValue }) => {
        return (
          <div className="flex space-x-2">
            <Pill className="font-mono">{getValue<string>()}</Pill>
          </div>
        );
      },
    },
    {
      id: 'jobTypes',
      accessorKey: 'jobTypes',
      accessorFn: (row) =>
        getTransformerJobTypesString(row.supportedJobTypes, jobTypeLabels),
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('jobTypes')} />
      ),
      cell: ({ getValue }) => {
        return (
          <div className="flex flex-wrap gap-1.5">
            {getValue<string[]>().map((item) => (
              <Pill className="font-mono" key={item}>
                {item}
              </Pill>
            ))}
          </div>
        );
      },
    },
    {
      accessorKey: 'source',
      accessorFn: (row) => getTransformerSourceString(row.source),
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('source')} />
      ),
      cell: ({ getValue }) => {
        return (
          <div className="flex space-x-2">
            <span className="max-w-[500px] truncate text-muted-foreground">
              {getValue<string>()}
            </span>
          </div>
        );
      },
    },
    {
      id: 'description',
      accessorKey: 'description',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('description')} />
      ),
      cell: ({ row }) => {
        return (
          <div className="flex space-x-2">
            <span className="max-w-[500px] truncate text-muted-foreground">
              {row.original.description}
            </span>
          </div>
        );
      },
    },
    {
      id: 'actions',
      cell: ({ row }) => <DataTableRowActions row={row} />,
    },
  ];
}
