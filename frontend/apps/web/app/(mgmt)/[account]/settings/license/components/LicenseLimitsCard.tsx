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
import { sourceUsage } from '@/libs/license/license';
import { getErrorMessage } from '@/util/util';
import {
  GetLicenseUsageResponse,
  LicenseSource,
  SystemLicense,
} from '@husonym/sdk';
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

  if (isLoading) {
    return <Skeleton className="w-full h-48" />;
  }

  const limits = license?.limits;

  return (
    <Card>
      <CardHeader>
        <CardTitle>Limits</CardTitle>
        <CardDescription>
          What the license of the instance caps, against what is used.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-6">
        {usageError ? (
          <div className="flex flex-col gap-2">
            <span className="text-sm font-medium">Sources</span>
            <Alert variant="destructive">
              <AlertTitle>Unable to count the sources</AlertTitle>
              <AlertDescription>{getErrorMessage(usageError)}</AlertDescription>
            </Alert>
            {limits?.maxSources !== undefined && (
              <p className="text-sm">The license allows {limits.maxSources}.</p>
            )}
          </div>
        ) : (
          <Sources license={license} usage={usage} />
        )}
        {limits?.maxJobs !== undefined && (
          <OtherLimit name="Jobs" value={`At most ${limits.maxJobs}`} />
        )}
        {limits?.maxConnections !== undefined && (
          <OtherLimit
            name="Connections"
            value={`At most ${limits.maxConnections}`}
          />
        )}
        {!!limits?.allowedConnectionTypes.length && (
          <OtherLimit
            name="Connection types"
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
  const { used, cap, over } = sourceUsage(license, usage);
  const sources = usage?.sourcesInAccount ?? [];

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-row items-baseline gap-2">
        <span className="text-sm font-medium">Sources</span>
        <span className="text-sm">
          {cap === undefined ? `${used}, no limit` : `${used} of ${cap}`}
        </span>
      </div>
      <p className="text-muted-foreground text-sm">
        The count covers the whole instance, every account included.
      </p>
      {over && (
        <Alert variant="warning">
          <AlertTitle>More sources than the license allows</AlertTitle>
          <AlertDescription>
            The instance counts {used} sources and its license allows {cap}.
          </AlertDescription>
        </Alert>
      )}
      <span className="pt-2 text-sm font-medium">Sources of this account</span>
      {sources.length === 0 ? (
        <p className="text-muted-foreground text-sm">
          This account has no source.
        </p>
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
