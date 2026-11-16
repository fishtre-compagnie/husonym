import { SchemaColumnHeader } from '@/components/jobs/SchemaTable/SchemaColumnHeader';
import { AppTableFeatures } from '@/components/table/features';
import TruncatedText from '@/components/TruncatedText';
import { createColumnHelper } from '@tanstack/react-table';
import CategoryCell from './CategoryCell';
import ConfidenceCell from './ConfidenceCell';
import ReporterTypeCell from './ReporterTypeCell';
import { useTranslations } from 'next-intl';

export interface PiiDetectionRow {
  schema: string;
  table: string;
  column: string;
  reporterType: string[];
  reporterConfidence: number[];
  reporterCategory: string[];
}

export function getPiiDetectionColumns(
  t: ReturnType<typeof useTranslations<'runs.pii.columns'>>
) {
  const columnHelper = createColumnHelper<AppTableFeatures, PiiDetectionRow>();

  const schemaColumn = columnHelper.accessor('schema', {
    header({ column }) {
      return <SchemaColumnHeader column={column} title={t('schema')} />;
    },
    cell({ getValue }) {
      return <TruncatedText text={getValue()} />;
    },
  });

  const tableColumn = columnHelper.accessor('table', {
    header({ column }) {
      return <SchemaColumnHeader column={column} title={t('table')} />;
    },
    cell({ getValue }) {
      return <TruncatedText text={getValue()} />;
    },
  });

  const columnColumn = columnHelper.accessor('column', {
    header({ column }) {
      return <SchemaColumnHeader column={column} title={t('column')} />;
    },
    cell({ getValue }) {
      return <TruncatedText text={getValue()} />;
    },
  });

  const reporterTypeColumn = columnHelper.accessor(
    (row) => {
      return row.reporterType.join(', ');
    },
    {
      id: 'reporterType',
      header({ column }) {
        return <SchemaColumnHeader column={column} title={t('reporter')} />;
      },
      cell({ row }) {
        return <ReporterTypeCell reporterTypes={row.original.reporterType} />;
      },
    }
  );

  const reporterConfidenceColumn = columnHelper.accessor(
    (row) => {
      return row.reporterConfidence.join(', ');
    },
    {
      id: 'reporterConfidence',
      header({ column }) {
        return <SchemaColumnHeader column={column} title={t('confidence')} />;
      },
      cell({ row }) {
        return <ConfidenceCell confidence={row.original.reporterConfidence} />;
      },
      sortFn: (a, b) => {
        return (
          a.original.reporterConfidence.reduce((acc, curr) => {
            return acc + curr;
          }, 0) -
          b.original.reporterConfidence.reduce((acc, curr) => {
            return acc + curr;
          }, 0)
        );
      },
    }
  );

  const reporterCategoryColumn = columnHelper.accessor(
    (row) => {
      return row.reporterCategory.join(', ');
    },
    {
      id: 'reporterCategory',
      header({ column }) {
        return <SchemaColumnHeader column={column} title={t('category')} />;
      },
      cell({ row }) {
        return <CategoryCell categories={row.original.reporterCategory} />;
      },
    }
  );

  return columnHelper.columns([
    schemaColumn,
    tableColumn,
    columnColumn,
    reporterTypeColumn,
    reporterConfidenceColumn,
    reporterCategoryColumn,
  ]);
}
