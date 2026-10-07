import { formatDate, licenseState } from '@/libs/license/license';
import { timestampDate } from '@bufbuild/protobuf/wkt';
import { useQuery } from '@connectrpc/connect-query';
import { UserAccountService } from '@husonym/sdk';
import { ReactElement } from 'react';
import { IoAlertCircleOutline } from 'react-icons/io5';
import { Alert, AlertDescription, AlertTitle } from './ui/alert';

interface Props {
  title?: string;
  description?: string;
}

function daysUntil(date: Date): number {
  return Math.max(
    0,
    Math.ceil((date.getTime() - Date.now()) / (24 * 60 * 60 * 1000))
  );
}

// Displays a licensing alert whose urgency follows the license lifecycle. Silent while
// the license is comfortably valid.
export default function SystemLicenseAlert(props: Props): ReactElement | null {
  const { data: systemInfo } = useQuery(
    UserAccountService.method.getSystemInformation
  );

  const license = systemInfo?.license;
  const expiresAt = license?.expiresAt
    ? timestampDate(license.expiresAt)
    : undefined;
  const state = licenseState(license);

  if (state === 'valid') {
    return null;
  }

  // Props keep overriding the copy, so existing call sites that gate an EE feature
  // behind this banner read exactly as they did before.
  const { title, description } = ((): {
    title: string;
    description: string;
  } => {
    switch (state) {
      case 'expiring':
        return {
          title: `License expires in ${daysUntil(expiresAt!)} days`,
          description: `Your license expires on ${formatDate(expiresAt!)}. Renew it to avoid losing the ability to create and run jobs.`,
        };
      case 'grace':
        return {
          title: 'License expired — grace period',
          description: `Your license expired on ${formatDate(expiresAt!)}. Everything still works for now, but creating and running jobs will stop when the grace period ends. Contact us to renew.`,
        };
      case 'frozen':
        return {
          title: 'License expired',
          description:
            'Creating, configuring and running jobs is disabled. Your configuration and run history remain available, and you can still pause and delete jobs. Renew your license to resume.',
        };
      default:
        return {
          title: props.title ?? 'License Required',
          description:
            props.description ??
            'This feature is only available to customers with a valid license.',
        };
    }
  })();

  return (
    <Alert variant={state === 'expiring' ? 'warning' : 'destructive'}>
      <div className="flex flex-row items-center gap-2">
        <IoAlertCircleOutline className="h-6 w-6" />
        <AlertTitle className="font-semibold">
          {props.title ?? title}
        </AlertTitle>
      </div>
      <AlertDescription className="pl-8">
        {props.description ?? description}
      </AlertDescription>
    </Alert>
  );
}
