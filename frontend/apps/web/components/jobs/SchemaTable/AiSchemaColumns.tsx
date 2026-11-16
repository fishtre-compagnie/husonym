import { AppTableFeatures } from '@/components/table/features';
import { Badge } from '@/components/ui/badge';
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip';
import { ColumnDef } from '@tanstack/react-table';
import type { useTranslations } from 'next-intl';
import { SchemaColumnHeader } from './SchemaColumnHeader';
import { SchemaConstraintHandler } from './schema-constraint-handler';
import { handleDataTypeBadge, toColKey } from './util';

interface Props {
  constraintHandler: SchemaConstraintHandler;
  t: ReturnType<typeof useTranslations<'mapping.schemaTable.aiSchema'>>;
}

interface Row {
  schema: string;
  table: string;
  column: string;
}

export function getSchemaColumns(
  props: Props
): ColumnDef<AppTableFeatures, Row>[] {
  const { constraintHandler, t } = props;

  return [
    {
      accessorKey: 'schema',
      enableSorting: false,
      enableHiding: false,
    },
    {
      accessorKey: 'table',
      enableSorting: false,
      enableHiding: false,
    },
    {
      accessorFn: (row) => `${row.schema}.${row.table}`,
      id: 'schemaTable',
      header: ({ column }) => (
        <SchemaColumnHeader column={column} title={t('table')} />
      ),
      cell: ({ getValue }) => {
        return (
          <div className="flex flex-row gap-2 items-center">
            <span className="max-w-[500px] truncate font-medium">
              {getValue<string>()}
            </span>
          </div>
        );
      },
      maxSize: 500,
      size: 300,
    },
    {
      accessorKey: 'column',
      header: ({ column }) => (
        <SchemaColumnHeader column={column} title={t('column')} />
      ),
      cell: ({ row }) => {
        return (
          <span className="max-w-[500px] truncate font-medium">
            {row.getValue('column')}
          </span>
        );
      },
      maxSize: 500,
      size: 200,
    },
    {
      id: 'constraints',
      accessorKey: 'constraints',
      header: ({ column }) => (
        <SchemaColumnHeader column={column} title={t('constraints')} />
      ),
      accessorFn: (row) => {
        const key = toColKey(row.schema, row.table, row.column);
        const isPrimaryKey = constraintHandler.getIsPrimaryKey(key);
        const [isForeignKey, fkCols] = constraintHandler.getIsForeignKey(key);

        const pieces: string[] = [];
        if (isPrimaryKey) {
          pieces.push(t('primaryKey'));
        }
        if (isForeignKey) {
          fkCols.forEach((col) =>
            pieces.push(t('foreignKeyOf', { column: col }))
          );
        }
        return pieces.join('\n');
      },
      cell: ({ row }) => {
        const key = {
          schema: row.getValue<string>('schema'),
          table: row.getValue<string>('table'),
          column: row.getValue<string>('column'),
        };
        const isPrimaryKey = constraintHandler.getIsPrimaryKey(key);
        const [isForeignKey, fkCols] = constraintHandler.getIsForeignKey(key);
        const isUniqueConstraint = constraintHandler.getIsUniqueConstraint(key);
        return (
          <span className="max-w-[500px] truncate font-medium">
            <div className="flex flex-col lg:flex-row items-start gap-1">
              <div>
                {isPrimaryKey && (
                  <Badge
                    variant="outline"
                    className="cursor-default border-transparent bg-brand-soft font-mono text-[11px] text-link"
                  >
                    {t('primaryKey')}
                  </Badge>
                )}
              </div>
              <div>
                {isForeignKey && (
                  <TooltipProvider>
                    <Tooltip delayDuration={200}>
                      <TooltipTrigger>
                        <Badge
                          variant="outline"
                          className="cursor-default border-transparent bg-warn-soft font-mono text-[11px] text-warn-fg"
                        >
                          {t('foreignKey')}
                        </Badge>
                      </TooltipTrigger>
                      <TooltipContent>
                        {fkCols
                          .map((col) => t('primaryKeyOf', { column: col }))
                          .join('\n')}
                      </TooltipContent>
                    </Tooltip>
                  </TooltipProvider>
                )}
              </div>
              <div>
                {isUniqueConstraint && (
                  <Badge
                    variant="outline"
                    className="cursor-default border-transparent bg-quiet-soft font-mono text-[11px] text-quiet-fg"
                  >
                    {t('unique')}
                  </Badge>
                )}
              </div>
            </div>
          </span>
        );
      },
    },
    {
      accessorKey: 'isNullable',
      accessorFn: (row) => {
        const key = toColKey(row.schema, row.table, row.column);
        return constraintHandler.getIsNullable(key) ? t('yes') : t('no');
      },
      header: ({ column }) => (
        <SchemaColumnHeader column={column} title={t('nullable')} />
      ),
      cell: ({ row }) => {
        const key = {
          schema: row.getValue<string>('schema'),
          table: row.getValue<string>('table'),
          column: row.getValue<string>('column'),
        };
        const isNullable = constraintHandler.getIsNullable(key);
        const text = isNullable ? t('yes') : t('no');
        return (
          <span className="max-w-[500px] truncate font-medium">
            <Badge variant="outline">{text}</Badge>
          </span>
        );
      },
    },
    {
      accessorKey: 'dataType',
      accessorFn: (row) => {
        const key = toColKey(row.schema, row.table, row.column);
        return handleDataTypeBadge(constraintHandler.getDataType(key));
      },
      header: ({ column }) => (
        <SchemaColumnHeader column={column} title={t('dataType')} />
      ),
      cell: ({ row }) => {
        const key = {
          schema: row.getValue<string>('schema'),
          table: row.getValue<string>('table'),
          column: row.getValue<string>('column'),
        };
        const datatype = constraintHandler.getDataType(key);
        return (
          <TooltipProvider>
            <Tooltip delayDuration={200}>
              <TooltipTrigger asChild>
                <div>
                  <Badge variant="outline" className="max-w-[100px]">
                    <span className="truncate block overflow-hidden">
                      {handleDataTypeBadge(datatype)}
                    </span>
                  </Badge>
                </div>
              </TooltipTrigger>
              <TooltipContent>
                <p>{datatype}</p>
              </TooltipContent>
            </Tooltip>
          </TooltipProvider>
        );
      },
    },
  ];
}
