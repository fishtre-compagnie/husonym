'use client';
import ButtonText from '@/components/ButtonText';
import Spinner from '@/components/Spinner';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Job } from '@husonym/sdk';
import { useTranslations } from 'next-intl';
import { ReactElement } from 'react';
import PreflightReportView from './PreflightReportView';
import { countReport, engineLabel, summarize } from './report';
import { useJobConnections } from './useJobConnections';
import { usePreflightCheck } from './usePreflightCheck';
import { useFormatDateTime } from '@/i18n/date-fns';

interface Props {
  job: Job;
}

// PreflightCard tells, when asked, what a run of the job would meet. It asks nothing by
// itself: each check logs in to the databases of the job.
export default function PreflightCard(props: Props): ReactElement {
  const { job } = props;
  const t = useTranslations('mapping.preflight');
  const formatDateTime = useFormatDateTime();
  const { check, isChecking, outcome } = usePreflightCheck(job.id);
  const connections = useJobConnections(job, job.accountId);
  const report = outcome?.report;

  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between gap-4 space-y-0">
        <div className="flex flex-col gap-1.5">
          <CardTitle>{t('title')}</CardTitle>
          <CardDescription>
            {report && outcome?.checkedAt
              ? t('card.checkedAt', {
                  engine: engineLabel(report.engine, t),
                  date: formatDateTime(outcome.checkedAt) ?? '',
                })
              : t('card.description')}
          </CardDescription>
        </div>
        <Button
          type="button"
          variant="outline"
          disabled={isChecking}
          onClick={() => void check()}
        >
          <ButtonText
            leftIcon={isChecking ? <Spinner /> : undefined}
            text={isChecking ? t('card.checking') : t('card.checkNow')}
          />
        </Button>
      </CardHeader>
      {isChecking || outcome ? (
        <CardContent className="flex flex-col gap-4">
          {isChecking ? (
            <p className="text-sm text-muted-foreground">
              {t('card.inProgress')}
            </p>
          ) : null}
          {!isChecking && outcome?.error ? (
            <Alert variant="destructive">
              <AlertTitle>{t('checkDidNotEnd')}</AlertTitle>
              <AlertDescription>{outcome.error}</AlertDescription>
            </Alert>
          ) : null}
          {!isChecking && report ? (
            <>
              <p className="text-sm">{summarize(countReport(report), t)}</p>
              <PreflightReportView
                report={report}
                connections={connections}
                copyableRemedies
              />
            </>
          ) : null}
        </CardContent>
      ) : null}
    </Card>
  );
}
