import { AppTableFeatures } from '@/components/table/features';
import { ColumnDef } from '@tanstack/react-table';
import type { useTranslations } from 'next-intl';
import { StringSelect } from '../../SchemaTable/StringSelect';
import { ColumnHeader } from './ColumnHeader';

interface Props {
  destinationDetailsRecord: Record<string, DestinationDetails>;
  onUpdate(req: OnTableMappingUpdateRequest): void;
  t: ReturnType<typeof useTranslations<'mapping.nosql.tableMappings'>>;
}

export interface OnTableMappingUpdateRequest {
  destinationId: string;
  souceName: string;
  tableName: string;
}

export interface DestinationDetails {
  destinationId: string;
  friendlyName: string;

  availableTableNames: string[];
}

export interface TableMappingRow {
  destinationId: string;
  sourceTable: string;
  destinationTable: string;
}

export function getTableMappingsColumns(
  props: Props
): ColumnDef<AppTableFeatures, TableMappingRow>[] {
  const { destinationDetailsRecord, onUpdate, t } = props;

  return [
    {
      accessorKey: 'destinationId',
      header: ({ column }) => (
        <ColumnHeader column={column} title={t('destination')} />
      ),
      cell: ({ getValue }) => {
        const destId = getValue<string>();
        const details: DestinationDetails = destinationDetailsRecord[
          destId
        ] ?? {
          destinationId: destId,
          friendlyName: t('unknownName'),
          availableTableNames: [],
        };
        return <span>{details.friendlyName}</span>;
      },
    },
    {
      accessorKey: 'sourceTable',
      header: ({ column }) => (
        <ColumnHeader column={column} title={t('sourceTable')} />
      ),
    },
    {
      accessorKey: 'destinationTable',
      header: ({ column }) => (
        <ColumnHeader column={column} title={t('destinationTable')} />
      ),
      cell: ({ row, getValue }) => {
        const destTableName = getValue<string>();
        const destId = row.getValue<string>('destinationId');
        const details: DestinationDetails = destinationDetailsRecord[
          destId
        ] ?? {
          destinationId: destId,
          friendlyName: t('unknownName'),
          availableTableNames: [destTableName],
        };

        return (
          <div className="flex flex-row gap-2 items-center">
            <StringSelect
              value={destTableName}
              values={details.availableTableNames}
              setValue={(newDestTableName) =>
                onUpdate({
                  destinationId: destId,
                  souceName: row.getValue<string>('sourceTable'),
                  tableName: newDestTableName,
                })
              }
              placeholder={t('selectDestinationTable')}
              searchPlaceholder={t('searchDestinationTable')}
              emptyText={t('noDestinationTable')}
            />
          </div>
        );
      },
    },
  ];
}
