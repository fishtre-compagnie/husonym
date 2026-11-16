'use client';

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import {
  limitsInForce,
  SourceUsage,
  sourceUsage,
} from '@/libs/license/license';
import { getErrorMessage } from '@/util/util';
import {
  GetLicenseUsageResponse,
  LicenseSource,
  SystemLicense,
} from '@husonym/sdk';
import { useTranslations } from 'next-intl';
import { ReactElement } from 'react';

interface Props {
  license?: SystemLicense;
  usage?: GetLicenseUsageResponse;
  isLoading: boolean;
  // Why the usage could not be read. The caps of the key are still shown then.
  usageError: unknown;
}

export default function LicenseLimitsCard(props: Props): ReactElement {
  const { license, usage, isLoading, usageError } = props;
  const t = useTranslations('settings.license.limits');

  if (isLoading) {
    return <Skeleton className="w-full h-48" />;
  }

  const limits = limitsInForce(license);

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('title')}</CardTitle>
        <CardDescription>{t('description')}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-6">
        {usageError ? (
          <div className="flex flex-col gap-2">
            <span className="text-sm font-medium">{t('sources')}</span>
            <Alert variant="destructive">
              <AlertTitle>{t('countError')}</AlertTitle>
              <AlertDescription>{getErrorMessage(usageError)}</AlertDescription>
            </Alert>
            {limits?.maxSources !== undefined && (
              <p className="text-sm">
                {t('allows', { count: limits.maxSources })}
              </p>
            )}
          </div>
        ) : (
          <Sources license={license} usage={usage} />
        )}
        {/* Unlike the sources, counted for the whole instance, these two caps apply to
            each account. */}
        {limits?.maxJobs !== undefined && (
          <OtherLimit
            name={t('jobs')}
            value={t('perAccount', { count: limits.maxJobs })}
          />
        )}
        {limits?.maxConnections !== undefined && (
          <OtherLimit
            name={t('connections')}
            value={t('perAccount', { count: limits.maxConnections })}
          />
        )}
        {!!limits?.allowedConnectionTypes.length && (
          <OtherLimit
            name={t('connectionTypes')}
            value={limits.allowedConnectionTypes.join(', ')}
          />
        )}
      </CardContent>
    </Card>
  );
}

function Sources({
  license,
  usage,
}: Pick<Props, 'license' | 'usage'>): ReactElement {
  const { used, cap, over, limit } = sourceUsage(license, usage);
  const sources = usage?.sourcesInAccount ?? [];
  const t = useTranslations('settings.license.limits');
  const count: Record<SourceUsage['limit'], string> = {
    'none-in-force': `${used}`,
    capped: t('capped', { used, cap: cap ?? 0 }),
    uncapped: t('uncapped', { used }),
  };

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-row items-baseline gap-2">
        <span className="text-sm font-medium">{t('sources')}</span>
        <span className="text-sm">{count[limit]}</span>
      </div>
      <p className="text-muted-foreground text-sm">{t('countedForInstance')}</p>
      {over && (
        <Alert variant="warning">
          <AlertTitle>{t('overTitle')}</AlertTitle>
          <AlertDescription>
            {t('overDescription', { used, cap: cap ?? 0 })}
          </AlertDescription>
        </Alert>
      )}
      <span className="pt-2 text-sm font-medium">{t('accountSources')}</span>
      {sources.length === 0 ? (
        <p className="text-muted-foreground text-sm">{t('noSource')}</p>
      ) : (
        <ul className="flex flex-col gap-1 text-sm">
          {sources.map((source) => (
            <li key={`${source.connectionId}/${source.database}`}>
              <SourceName source={source} />
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function SourceName({ source }: { source: LicenseSource }): ReactElement {
  return (
    <>
      {/* The connection of a source may be gone: its id is all that is left to name it. */}
      {source.connectionName || source.connectionId}
      {source.database && (
        <span className="text-muted-foreground"> — {source.database}</span>
      )}
    </>
  );
}

function OtherLimit({
  name,
  value,
}: {
  name: string;
  value: string;
}): ReactElement {
  return (
    <div className="flex flex-row items-baseline gap-2">
      <span className="text-sm font-medium">{name}</span>
      <span className="text-sm">{value}</span>
    </div>
  );
}
