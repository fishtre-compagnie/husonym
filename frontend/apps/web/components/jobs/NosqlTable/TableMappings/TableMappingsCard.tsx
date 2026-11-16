import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { EditDestinationOptionsFormValues } from '@/yup-validations/jobs';
import { TableIcon } from '@radix-ui/react-icons';
import { useTranslations } from 'next-intl';
import { ReactElement, useMemo } from 'react';
import {
  DestinationDetails,
  getTableMappingsColumns,
  OnTableMappingUpdateRequest,
  TableMappingRow,
} from './Columns';
import TableMappingsTable from './TableMappingsTable';

export interface Props {
  mappings: EditDestinationOptionsFormValues[];
  onUpdate(req: OnTableMappingUpdateRequest): void;
  destinationDetailsRecord: Record<string, DestinationDetails>;
}

export default function TableMappingsCard(props: Props): ReactElement {
  const { mappings, onUpdate, destinationDetailsRecord } = props;
  const t = useTranslations('mapping.nosql.tableMappings');
  const columns = useMemo(
    () => getTableMappingsColumns({ destinationDetailsRecord, onUpdate, t }),
    [destinationDetailsRecord, onUpdate, t]
  );
  return (
    <Card className="w-full">
      <CardHeader className="flex flex-col gap-2">
        <div className="flex flex-row items-center gap-2">
          <div className="flex">
            <TableIcon className="h-4 w-4" />
          </div>
          <CardTitle>{t('cardTitle')}</CardTitle>
        </div>
        <CardDescription className="max-w-2xl">
          {t('cardDescription')}
        </CardDescription>
      </CardHeader>
      <CardContent>
        <TableMappingsTable
          data={toTableMappingRows(mappings)}
          columns={columns}
        />
      </CardContent>
    </Card>
  );
}

function toTableMappingRows(
  mappings: EditDestinationOptionsFormValues[]
): TableMappingRow[] {
  return mappings.flatMap((mapping) => {
    return (
      mapping.dynamodb?.tableMappings.map((tm): TableMappingRow => {
        return {
          destinationId: mapping.destinationId,
          sourceTable: tm.sourceTable,
          destinationTable: tm.destinationTable,
        };
      }) ?? []
    );
  });
}
