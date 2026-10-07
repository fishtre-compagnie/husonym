'use client';

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Badge, BadgeProps } from '@/components/ui/badge';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { formatDate, LicenseState, licenseState } from '@/libs/license/license';
import { Timestamp, timestampDate } from '@bufbuild/protobuf/wkt';
import { SystemLicense } from '@husonym/sdk';
import { ReactElement, ReactNode } from 'react';

interface Props {
  license?: SystemLicense;
  isLoading: boolean;
}

const STATE_LABELS: Record<LicenseState, string> = {
  none: 'No license',
  valid: 'Valid',
  expiring: 'Expiring soon',
  grace: 'Expired — grace period',
  frozen: 'Expired',
};

// The badge has no warning tone of its own: the two states that call for one borrow
// the border of the warning alert.
const STATE_BADGES: Record<
  LicenseState,
  Pick<BadgeProps, 'variant' | 'className'>
> = {
  none: { variant: 'outline' },
  valid: { variant: 'default' },
  expiring: {
    variant: 'outline',
    className: 'border-yellow-300 dark:border-orange-500',
  },
  grace: {
    variant: 'outline',
    className: 'border-yellow-300 dark:border-orange-500',
  },
  frozen: { variant: 'destructive' },
};

const ORIGIN_LABELS: Record<string, string> = {
  interface: 'Installed through the interface',
  environment: 'Read from the environment',
  file: 'Read from a file',
  renewal: 'Received as a renewal',
};

const TELEMETRY_LABELS: Record<string, string> = {
  online: 'Usage is reported online',
  offline_report: 'Usage is reported offline, through a report',
  none: 'No usage is reported',
};

export default function LicenseStatusCard(props: Props): ReactElement {
  const { license, isLoading } = props;

  if (isLoading) {
    return <Skeleton className="w-full h-32" />;
  }

  const state = licenseState(license);

  return (
    <Card>
      <CardHeader>
        <div className="flex flex-row items-center gap-3">
          <CardTitle>Status</CardTitle>
          <Badge {...STATE_BADGES[state]}>{STATE_LABELS[state]}</Badge>
        </div>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {license?.problem && (
          <Alert variant="destructive">
            <AlertTitle>The license key could not be read</AlertTitle>
            <AlertDescription>{license.problem}</AlertDescription>
          </Alert>
        )}
        {license && state !== 'none' ? (
          <LicenseDetails license={license} state={state} />
        ) : (
          <p className="text-sm">
            No license key is installed. You can paste one below.
          </p>
        )}
      </CardContent>
    </Card>
  );
}

function LicenseDetails({
  license,
  state,
}: {
  license: SystemLicense;
  state: LicenseState;
}): ReactElement {
  const expiresOn = formatTimestamp(license.expiresAt);
  const graceEndsOn = formatTimestamp(license.graceEndsAt);
  const installedOn = formatTimestamp(license.installedAt);
  const origin = ORIGIN_LABELS[license.origin] ?? license.origin;
  const telemetry = TELEMETRY_LABELS[license.telemetry] ?? license.telemetry;

  return (
    <dl className="grid grid-cols-[max-content_1fr] gap-x-8 gap-y-2 text-sm">
      {license.issuedTo && <Detail term="Licensee">{license.issuedTo}</Detail>}
      {license.plan && <Detail term="Plan">{license.plan}</Detail>}
      {expiresOn && (
        <Detail
          term={
            state === 'grace' || state === 'frozen'
              ? 'Expired on'
              : 'Expires on'
          }
        >
          {expiresOn}
        </Detail>
      )}
      {state === 'grace' && graceEndsOn && (
        <Detail term="Grace period ends on">{graceEndsOn}</Detail>
      )}
      {(origin || installedOn) && (
        <Detail term="Key">
          {[origin, installedOn ? `on ${installedOn}` : undefined]
            .filter(Boolean)
            .join(', ')}
        </Detail>
      )}
      {telemetry && <Detail term="Usage reporting">{telemetry}</Detail>}
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
