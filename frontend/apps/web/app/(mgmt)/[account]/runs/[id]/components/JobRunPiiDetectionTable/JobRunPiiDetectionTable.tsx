import { Panel, PanelHeader } from '@/components/ds';
import FastTable from '@/components/FastTable/FastTable';
import { useAccount } from '@/components/providers/account-provider';
import { unpaginatedTableFeatures } from '@/components/table/features';
import { Button } from '@/components/ui/button';
import { cn, refreshWhenJobRunning } from '@/libs/utils';
import { useQuery } from '@connectrpc/connect-query';
import { JobService, PiiDetectionReport_TableReport } from '@husonym/sdk';
import { DownloadIcon, ReloadIcon } from '@radix-ui/react-icons';
import { useTable } from '@tanstack/react-table';
import Papa from 'papaparse';
import { ReactElement, useCallback, useMemo } from 'react';
import { getPiiDetectionColumns, PiiDetectionRow } from './columns';
import { useTranslations } from 'next-intl';

interface Props {
  jobRunId: string;
  isRunning: boolean;
}

export default function JobRunPiiDetectionTable(props: Props): ReactElement {
  const { jobRunId, isRunning } = props;
  const { account } = useAccount();
  const t = useTranslations('runs.pii');
  const tColumns = useTranslations('runs.pii.columns');
  const columns = useMemo(() => getPiiDetectionColumns(tColumns), [tColumns]);

  const {
    data: reportResp,
    isLoading: isLoadingReport,
    isPending: isPendingReport,
    isFetching: isFetchingReport,
    refetch: reportMutate,
  } = useQuery(
    JobService.method.getPiiDetectionReport,
    {
      jobRunId: jobRunId,
      accountId: account?.id,
    },
    {
      enabled: !!account && !!jobRunId,
      refetchInterval(query) {
        return query.state.data ? refreshWhenJobRunning(isRunning) : 0;
      },
    }
  );

  const data: PiiDetectionRow[] = useMemo(() => {
    if (!reportResp?.report || isLoadingReport || isPendingReport) {
      return [];
    }
    const report = reportResp.report;
    if (!report) {
      return [];
    }
    return getPiiDetectionRowsFromTables(report.tables);
  }, [reportResp?.report, isLoadingReport, isPendingReport, isFetchingReport]);

  const table = useTable({
    features: unpaginatedTableFeatures,
    data: data,
    columns,
  });

  function onRefreshClick(): void {
    if (!isFetchingReport) {
      reportMutate();
    }
  }

  const onDownloadClick = useOnDownloadClick(jobRunId, data);

  return (
    <Panel aria-label={t('title')}>
      <PanelHeader
        title={t('title')}
        actions={
          <>
            <span className="text-muted-foreground">
              {t('columnsCount', { count: data.length })}
            </span>
            <Button
              variant="ghost"
              size="icon"
              className="h-8 w-8"
              aria-label={t('refresh')}
              onClick={() => onRefreshClick()}
            >
              <ReloadIcon
                className={cn('h-4 w-4', isFetchingReport && 'animate-spin')}
              />
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => onDownloadClick()}
              disabled={!data.length}
            >
              <DownloadIcon className="h-3.5 w-3.5" />
              {t('csv')}
            </Button>
          </>
        }
      />
      <FastTable table={table} estimateRowSize={() => 53} rowOverscan={50} />
    </Panel>
  );
}

function useOnDownloadClick(
  jobRunId: string,
  data: PiiDetectionRow[]
): () => void {
  return useCallback(() => {
    if (!data.length) {
      return;
    }

    // Create CSV content
    const csvRows = data.map((row) => {
      return {
        schema: row.schema,
        table: row.table,
        column: row.column,
        categories: row.reporterCategory,
        confidence: row.reporterConfidence,
        reporters: row.reporterType,
      };
    });
    const csvContent = Papa.unparse(csvRows, {
      escapeFormulae: true,
      header: true,
    });
    downloadCsvData(csvContent, `pii-detection-report-${jobRunId}.csv`);
  }, [jobRunId, data]);
}

// Create and download the CSV file
function downloadCsvData(content: string, filename: string): void {
  const blob = new Blob([content], { type: 'text/csv;charset=utf-8;' });
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.setAttribute('href', url);
  link.setAttribute('download', filename);
  link.style.visibility = 'hidden';
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
}

function getPiiDetectionRowsFromTables(
  tables: PiiDetectionReport_TableReport[]
): PiiDetectionRow[] {
  return tables.flatMap(tableReportToPiiDetectionRow);
}

function tableReportToPiiDetectionRow(
  report: PiiDetectionReport_TableReport
): PiiDetectionRow[] {
  const output: PiiDetectionRow[] = [];

  for (const column of report.columns) {
    const categories: string[] = [];
    const confidence: number[] = [];
    const reporters: string[] = [];
    if (column.regexReport) {
      categories.push(column.regexReport.category);
      reporters.push('regex');
    }
    if (column.llmReport) {
      categories.push(column.llmReport.category);
      reporters.push('llm');
      confidence.push(column.llmReport.confidence);
    }
    output.push({
      schema: report.schema,
      table: report.table,
      column: column.column,
      reporterCategory: categories,
      reporterConfidence: confidence,
      reporterType: reporters,
    });
  }

  return output;
}
