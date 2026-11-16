'use client';

import { Pill, StatusDot, Tone } from '@/components/ds';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import {
  formatDate,
  LicenseState,
  licenseState,
  missingKeyMessage,
} from '@/libs/license/license';
import { licensedReportingLabel } from '@/libs/license/usage-report';
import { Timestamp, timestampDate } from '@bufbuild/protobuf/wkt';
import { SystemLicense, UsageReportingMode } from '@husonym/sdk';
import { useTranslations } from 'next-intl';
import { ReactElement, ReactNode } from 'react';

interface Props {
  license?: SystemLicense;
  isLoading: boolean;
  // What the key provides for the usage report, once read.
  licenseReportingMode?: UsageReportingMode;
}

// The tone each state of the license is told in.
const STATE_TONES: Record<LicenseState, Tone> = {
  none: 'muted',
  valid: 'ok',
  expiring: 'warn',
  grace: 'warn',
  frozen: 'danger',
};

// The origins of a key the page has words for, under settings.license.status.origins.
const ORIGINS = ['interface', 'environment', 'file', 'renewal'] as const;
type Origin = (typeof ORIGINS)[number];

function isKnownOrigin(origin: string): origin is Origin {
  return (ORIGINS as readonly string[]).includes(origin);
}

export default function LicenseStatusCard(props: Props): ReactElement {
  const { license, isLoading, licenseReportingMode } = props;
  const t = useTranslations('settings.license.status');

  if (isLoading) {
    return <Skeleton className="w-full h-32" />;
  }

  const state = licenseState(license);

  return (
    <Card>
      <CardHeader>
        <div className="flex flex-row items-center gap-3">
          <CardTitle>{t('title')}</CardTitle>
          <Pill tone={STATE_TONES[state]}>
            <StatusDot tone={STATE_TONES[state]} />
            {t(`states.${state}`)}
          </Pill>
        </div>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {license?.problem && (
          <Alert variant="destructive">
            <AlertTitle>{t('unreadable')}</AlertTitle>
            <AlertDescription>{license.problem}</AlertDescription>
          </Alert>
        )}
        {license && state !== 'none' ? (
          <LicenseDetails
            license={license}
            state={state}
            reportingMode={licenseReportingMode}
          />
        ) : (
          <p className="text-sm">{missingKeyMessage(license)}</p>
        )}
      </CardContent>
    </Card>
  );
}

function LicenseDetails({
  license,
  state,
  reportingMode,
}: {
  license: SystemLicense;
  state: LicenseState;
  reportingMode?: UsageReportingMode;
}): ReactElement {
  const t = useTranslations('settings.license.status');
  const expiresOn = formatTimestamp(license.expiresAt);
  const graceEndsOn = formatTimestamp(license.graceEndsAt);
  const installedOn = formatTimestamp(license.installedAt);
  const reporting =
    reportingMode === undefined
      ? undefined
      : licensedReportingLabel(reportingMode);
  const origin = isKnownOrigin(license.origin)
    ? t(`origins.${license.origin}`)
    : license.origin;

  return (
    <dl className="grid grid-cols-[max-content_1fr] gap-x-8 gap-y-2 text-sm">
      {license.issuedTo && (
        <Detail term={t('licensee')}>{license.issuedTo}</Detail>
      )}
      {license.plan && <Detail term={t('plan')}>{license.plan}</Detail>}
      {expiresOn && (
        <Detail
          term={
            state === 'grace' || state === 'frozen'
              ? t('expiredOn')
              : t('expiresOn')
          }
        >
          {expiresOn}
        </Detail>
      )}
      {state === 'grace' && graceEndsOn && (
        <Detail term={t('graceEndsOn')}>{graceEndsOn}</Detail>
      )}
      {(origin || installedOn) && (
        <Detail term={t('key')}>
          {[
            origin,
            installedOn ? t('installedOn', { date: installedOn }) : undefined,
          ]
            .filter(Boolean)
            .join(', ')}
        </Detail>
      )}
      {reporting && <Detail term={t('usageReport')}>{reporting}</Detail>}
    </dl>
  );
}

function Detail({
  term,
  children,
}: {
  term: string;
  children: ReactNode;
}): ReactElement {
  return (
    <>
      <dt className="text-muted-foreground">{term}</dt>
      <dd>{children}</dd>
    </>
  );
}

// A timestamp the API left unset comes as nothing or as the epoch: neither is a date
// worth showing.
function formatTimestamp(timestamp: Timestamp | undefined): string | undefined {
  if (!timestamp) {
    return undefined;
  }
  const date = timestampDate(timestamp);
  return date.getTime() === 0 ? undefined : formatDate(date);
}
